package filetransfer

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestListingPreservesUnusualNamesAndDoesNotExecutePaths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "folder ' ; $(touch INJECTED)")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"a file.txt", "quote'file", "line\nfile", "tab\tfile", ".hidden"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("subdir", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", ListingScript(dir))
	cmd.Dir = root
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	resolved, entries, err := ParseListing(data)
	if err != nil || resolved != dir {
		t.Fatalf("listing: %q %v", resolved, err)
	}
	if len(entries) != len(names)+2 || entries[0].Name != "subdir" || !entries[0].IsDir() {
		t.Fatalf("entries: %#v", entries)
	}
	for _, name := range names {
		if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Name == name && e.Size == 5 && e.CanCopy() }) {
			t.Fatalf("missing %q", name)
		}
	}
	if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Name == "link" && !e.CanCopy() }) {
		t.Fatal("link treated as a copyable entry")
	}
	if _, err := os.Stat(filepath.Join(root, "INJECTED")); !os.IsNotExist(err) {
		t.Fatal("shell injection")
	}
	_, local, err := ListLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(local, func(e Entry) bool { return e.Name == "link" && e.Kind == "l" }) {
		t.Fatal("local link not detected")
	}
}

func TestParseListingRejectsMalformedOutput(t *testing.T) {
	for _, data := range []string{"", "banner\n/tmp\x00", "/tmp\x00f\x00NaN\x00file\x00", "/tmp\x00f\x001\x00../escape\x00", "/tmp\x00f\x001\x00file", "/tmp\x00f\x001\x00..\x00"} {
		if _, _, err := ParseListing([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	if _, entries, err := ParseListing([]byte("/tmp\x00")); err != nil || len(entries) != 0 {
		t.Fatal("empty directory rejected")
	}
}

func TestCommandsReuseSSHOptionsAndQuoteRemotePaths(t *testing.T) {
	ssh := exec.Command("ssh", "-i", "/keys/id", "-p", "2222", "-o", "ProxyJump=bastion", "user@2001:db8::1")
	client := New(ssh)
	remote := "/tmp/a ' ; $(touch BAD)"
	cmd, err := client.CopyCommand("/tmp/local", remote, true, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scp", "-O", "-o", "ConnectTimeout=10", "-r", "-i", "/keys/id", "-P", "2222", "-o", "ProxyJump=bastion", "--", "/tmp/local", "user@[2001:db8::1]:" + SCPPath(remote)}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %#v", cmd.Args)
	}
	down, err := client.CopyCommand("/tmp/local", remote, false, false)
	if err != nil || down.Args[len(down.Args)-1] != "/tmp/local" {
		t.Fatalf("download: %v %v", down, err)
	}
	alias := New(exec.Command("ssh", "production"))
	if alias.Target != "production" || len(alias.Options) != 0 {
		t.Fatal("alias lost")
	}
	listing := client.ListCommand(remote)
	if !strings.Contains(listing.Args[len(listing.Args)-1], Quote(remote)) || !slices.Equal(client.Options, ssh.Args[1:len(ssh.Args)-1]) {
		t.Fatal("listing changed connection options")
	}
	for _, paths := range [][2]string{{"relative", "/remote"}, {"/local", "relative"}, {"/local", "/remote\nfile"}} {
		if _, err := client.CopyCommand(paths[0], paths[1], true, false); err == nil {
			t.Fatalf("unsafe paths accepted: %v", paths)
		}
	}
}
