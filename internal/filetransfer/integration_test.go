package filetransfer

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in because this starts a local SSH server and needs openssh-server.
func TestSCPIntegration(t *testing.T) {
	if os.Getenv("ZENSSH_SSH_INTEGRATION") != "1" {
		t.Skip("set ZENSSH_SSH_INTEGRATION=1 to test against a local sshd")
	}
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	key := filepath.Join(root, "id")
	if data, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("keygen: %v %s", err, data)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := filepath.Join(root, "sshd_config")
	settings := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s.pub\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nPermitRootLogin yes\n", port, key, filepath.Join(root, "pid"), key)
	if err := os.WriteFile(config, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(sshd, "-D", "-e", "-f", config)
	logFile, err := os.Create(filepath.Join(root, "sshd.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	server.Stderr = logFile
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Process.Kill(); _ = server.Wait() }()
	ready := false
	for attempt := 0; attempt < 50; attempt++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(logFile.Name())
		t.Fatalf("sshd did not start: %s", data)
	}
	client := New(exec.Command("ssh", "-i", key, "-p", fmt.Sprint(port), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", account.Username+"@127.0.0.1"))
	local, remote, downloads := filepath.Join(root, "local"), filepath.Join(root, "remote ' files"), filepath.Join(root, "downloads")
	for _, dir := range []string{local, remote, downloads} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"space file.txt", "quote'file.txt", "literal$(touch BAD);[x]*?.txt"}
	for _, name := range names {
		source := filepath.Join(local, name)
		if err := os.WriteFile(source, []byte("original payload"), 0600); err != nil {
			t.Fatal(err)
		}
		up, err := client.CopyCommand(source, remote, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if data, err := up.CombinedOutput(); err != nil {
			t.Fatalf("upload %q: %v %s", name, err, data)
		}
		down, err := client.CopyCommand(downloads, filepath.Join(remote, name), false, false)
		if err != nil {
			t.Fatal(err)
		}
		if data, err := down.CombinedOutput(); err != nil {
			t.Fatalf("download %q: %v %s", name, err, data)
		}
		data, err := os.ReadFile(filepath.Join(downloads, name))
		if err != nil || !bytes.Equal(data, []byte("original payload")) {
			t.Fatalf("payload %q: %s %v", name, data, err)
		}
	}
	folder := filepath.Join(local, "nested ' folder")
	if err := os.MkdirAll(filepath.Join(folder, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "child", ".hidden"), []byte("nested"), 0600); err != nil {
		t.Fatal(err)
	}
	up, _ := client.CopyCommand(folder, remote, true, true)
	if data, err := up.CombinedOutput(); err != nil {
		t.Fatalf("recursive upload: %v %s", err, data)
	}
	down, _ := client.CopyCommand(downloads, filepath.Join(remote, filepath.Base(folder)), false, true)
	if data, err := down.CombinedOutput(); err != nil {
		t.Fatalf("recursive download: %v %s", err, data)
	}
	if data, err := os.ReadFile(filepath.Join(downloads, filepath.Base(folder), "child", ".hidden")); err != nil || string(data) != "nested" {
		t.Fatalf("recursive payload: %s %v", data, err)
	}
	listing := client.ListCommand(remote)
	data, err := listing.Output()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	resolved, entries, err := ParseListing(data)
	if err != nil || resolved != remote || len(entries) != len(names)+1 {
		t.Fatalf("listing %q: %#v %v", resolved, entries, err)
	}
	missing := client.ListCommand(filepath.Join(root, "does not exist"))
	if err := missing.Run(); err == nil {
		t.Fatal("missing directory accepted")
	}
	if data, _ := os.ReadFile(logFile.Name()); strings.Contains(string(data), "fatal") {
		t.Fatalf("sshd error: %s", data)
	}
}
