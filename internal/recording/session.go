package recording

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Prepare wraps SSH in a terminal recorder, keeping passwords out of input logs.
func Prepare(ssh *exec.Cmd, alias string) (*exec.Cmd, string, error) {
	if runtime.GOOS != "linux" {
		return nil, "", fmt.Errorf("gravacao de sessao requer Linux e script (util-linux)")
	}
	script, err := exec.LookPath("script")
	if err != nil {
		return nil, "", fmt.Errorf("gravacao requer o comando script (util-linux): %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(home, ".local", "state", "zenssh", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, "", err
	}
	safeAlias := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, alias)
	if len(safeAlias) > 80 {
		safeAlias = safeAlias[:80]
	}
	f, err := os.CreateTemp(dir, safeAlias+"-"+time.Now().Format("20060102-150405")+"-*.log")
	if err != nil {
		return nil, "", err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return nil, "", err
	}
	args := append([]string{ssh.Path}, ssh.Args[1:]...)
	for i, arg := range args {
		args[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	cmd := exec.Command(script, "-q", "-e", "-f", "-c", "exec "+strings.Join(args, " "), path)
	cmd.Dir = ssh.Dir
	env := ssh.Env
	if env == nil {
		env = os.Environ()
	}
	for _, value := range env {
		if !strings.HasPrefix(value, "SHELL=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "SHELL=/bin/sh")
	return cmd, path, nil
}
