// Package filetransfer browses Linux directories over SSH and copies with SCP.
package filetransfer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Entry struct {
	Name string
	Kind string
	Size int64
}

func (e Entry) IsDir() bool   { return e.Kind == "d" }
func (e Entry) CanCopy() bool { return e.Kind == "d" || e.Kind == "f" }

type Client struct {
	SSHPath    string
	Options    []string
	Target     string
	controlDir string
}

// Multiplex keeps authentication alive while background listings use the same
// connection. A private short path also stays within Unix socket length limits.
func (c Client) Multiplex() (Client, error) {
	dir, err := os.MkdirTemp("", "zenssh-ssh-")
	if err != nil {
		return c, err
	}
	c.controlDir = dir
	c.Options = append([]string{"-o", "ControlMaster=auto", "-o", "ControlPersist=60", "-o", "ControlPath=" + filepath.Join(dir, "socket")}, c.Options...)
	return c, nil
}

func (c Client) Close() {
	if c.controlDir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := append([]string{"-o", "BatchMode=yes"}, c.Options...)
	args = append(args, "-O", "exit", "--", c.Target)
	_ = exec.CommandContext(ctx, c.SSHPath, args...).Run()
	_ = os.RemoveAll(c.controlDir)
}

const listingStart = "\x00ZENSSH-LIST-V1\x00"
const listingEnd = "\x00ZENSSH-LIST-END\x00"

// New reuses the fully prepared connection, including imported SSH aliases.
func New(ssh *exec.Cmd) Client {
	args := ssh.Args[1:]
	return Client{SSHPath: ssh.Path, Options: append([]string(nil), args[:len(args)-1]...), Target: args[len(args)-1]}
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// SCP's download filename check uses fnmatch on the requested basename.
// Backslash escaping works for both the remote shell and that check; enclosing
// quotes would become literal characters in the expected filename pattern.
func SCPPath(s string) string {
	var escaped strings.Builder
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-", r) || r > 127) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}

func ListingScript(dir string) string {
	// NUL framing preserves spaces, tabs and newlines in filenames. GNU find
	// lists links as links; the browser does not follow them implicitly.
	return "cd -- " + Quote(dir) + " && printf '\\0ZENSSH-LIST-V1\\0%s\\0' \"$(pwd -P)\" && find . -mindepth 1 -maxdepth 1 -printf '%y\\0%s\\0%f\\0' && printf '\\0ZENSSH-LIST-END\\0'"
}

func (c Client) BackgroundListCommand(ctx context.Context, dir string) *exec.Cmd {
	cmd := c.ListCommand(dir)
	args := append([]string{"-o", "BatchMode=yes"}, cmd.Args[1:]...)
	return exec.CommandContext(ctx, c.SSHPath, args...)
}

func (c Client) ListCommand(dir string) *exec.Cmd {
	args := append([]string(nil), c.Options...)
	args = append(args, "-T", "-o", "RequestTTY=no", "-o", "ConnectTimeout=10", "--", c.Target, ListingScript(dir))
	return exec.Command(c.SSHPath, args...)
}

func ParseListing(data []byte) (string, []Entry, error) {
	if start := strings.Index(string(data), listingStart); start >= 0 {
		payload := string(data)[start+len(listingStart):]
		end := strings.Index(payload, listingEnd)
		if end < 0 {
			return "", nil, fmt.Errorf("listagem remota incompleta")
		}
		data = []byte(payload[:end])
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) < 2 || parts[len(parts)-1] != "" || !strings.HasPrefix(parts[0], "/") || (len(parts)-2)%3 != 0 {
		return "", nil, fmt.Errorf("listagem remota invalida; requer shell Linux e GNU find, sem mensagens extras em stdout")
	}
	entries := make([]Entry, 0, (len(parts)-2)/3)
	for i := 1; i < len(parts)-1; i += 3 {
		size, err := strconv.ParseInt(parts[i+1], 10, 64)
		name := parts[i+2]
		if err != nil || size < 0 || len(parts[i]) != 1 || name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return "", nil, fmt.Errorf("entrada remota invalida")
		}
		entries = append(entries, Entry{Name: name, Kind: parts[i], Size: size})
	}
	sortEntries(entries)
	return parts[0], entries, nil
}

func ListLocal(dir string) (string, []Entry, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		return "", nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			return "", nil, err
		}
		kind := "?"
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			kind = "l"
		case info.IsDir():
			kind = "d"
		case info.Mode().IsRegular():
			kind = "f"
		}
		entries = append(entries, Entry{Name: item.Name(), Kind: kind, Size: info.Size()})
	}
	sortEntries(entries)
	return abs, entries, nil
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name < entries[j].Name
	})
}

// CopyCommand uses the legacy SCP protocol explicitly. Both remote operands
// and shell metacharacters are quoted; local paths must be absolute.
func (c Client) CopyCommand(local, remote string, upload, recursive bool) (*exec.Cmd, error) {
	if !filepath.IsAbs(local) || !strings.HasPrefix(remote, "/") || strings.ContainsAny(local+remote, "\x00\r\n") {
		return nil, fmt.Errorf("SCP requer caminhos absolutos sem quebras de linha")
	}
	args := []string{"-O", "-o", "ConnectTimeout=10"}
	if recursive {
		args = append(args, "-r")
	}
	for i := 0; i < len(c.Options); i++ {
		arg := c.Options[i]
		if arg == "-p" {
			arg = "-P"
		}
		args = append(args, arg)
		if i+1 < len(c.Options) {
			i++
			args = append(args, c.Options[i])
		}
	}
	target := c.Target
	// SCP requires brackets around IPv6 addresses, unlike SSH destinations.
	if at := strings.LastIndex(target, "@"); at >= 0 {
		host := target[at+1:]
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			target = target[:at+1] + "[" + host + "]"
		}
	}
	operand := target + ":" + SCPPath(remote)
	args = append(args, "--")
	if upload {
		args = append(args, local, operand)
	} else {
		args = append(args, operand, local)
	}
	return exec.Command("scp", args...), nil
}
