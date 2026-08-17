package sshcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zenssh/internal/config"
)

func TestEnsureMainConfigIncludesCreatesBackupAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(sshDir, "config")
	original := "Host old\n  HostName 10.0.0.1\n"
	if err := os.WriteFile(main, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(home, ".config", "zenssh", "ssh_config")
	if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMainConfigIncludes(managed); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(main)
	if err := EnsureMainConfigIncludes(managed); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(main)
	if string(first) != string(second) {
		t.Fatal("second call changed config")
	}
	if !strings.HasPrefix(string(first), "# Managed by ZenSSH\nInclude "+managed) {
		t.Fatalf("unexpected config: %s", first)
	}
	backup, err := os.ReadFile(main + ".zenssh.bak")
	if err != nil || string(backup) != original {
		t.Fatalf("unexpected backup: %q, %v", backup, err)
	}
	if err := os.WriteFile(main, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreMainConfigBackup(); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(main)
	if string(restored) != original {
		t.Fatalf("restored = %q", restored)
	}
}

func TestRenderedConfigPreservesEffectiveIdentityAndProxySettings(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original")
	key1, key2 := filepath.Join(dir, "id_one"), filepath.Join(dir, "id_two")
	content := "Host prod\n  HostName 10.0.0.9\n  User deploy\n  Port 2202\n  IdentityFile " + key1 + "\n  IdentityFile " + key2 + "\n  IdentitiesOnly no\n  ForwardAgent yes\n"
	if err := os.WriteFile(original, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ssh", "-F", original, "-G", "prod").Output()
	if err != nil {
		t.Fatal(err)
	}
	host, err := parseResolvedConfig("prod", out)
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "generated")
	if err := os.WriteFile(generated, []byte(RenderManagedConfig([]config.Host{host})), 0o600); err != nil {
		t.Fatal(err)
	}
	out2, err := exec.Command("ssh", "-F", generated, "-G", "prod").Output()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := parseResolvedConfig("prod", out2)
	if err != nil {
		t.Fatal(err)
	}
	if host.HostName != resolved.HostName || host.User != resolved.User || host.Port != resolved.Port {
		t.Fatalf("endpoint changed: %#v -> %#v", host, resolved)
	}
	if !slices.Equal(host.IdentityFiles, resolved.IdentityFiles) {
		t.Fatalf("identities changed: %#v -> %#v", host.IdentityFiles, resolved.IdentityFiles)
	}
	for _, option := range []string{"IdentitiesOnly=no", "ForwardAgent=yes"} {
		if !slices.Contains(resolved.SSHOptions, option) {
			t.Fatalf("missing option %s in %#v", option, resolved.SSHOptions)
		}
	}
}

func TestReadOnlyHostIsNotRendered(t *testing.T) {
	host := config.Host{Alias: "external", HostName: "server", User: "deploy", Port: 22, Management: config.ManagementReadOnly}
	if strings.Contains(RenderManagedConfig([]config.Host{host}), "Host external") {
		t.Fatal("readonly host was rendered")
	}
}

func TestAtomicWritePreservesConfigSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-config")
	link := filepath.Join(dir, "config")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "new" {
		t.Fatalf("target = %q", data)
	}
}

func TestIsHostKnownUsesKnownHostsWithoutNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(home, "host-key")
	if output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, output)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(string(pub))
	known := "[server.example]:2222 " + parts[0] + " " + parts[1] + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(known), 0o600); err != nil {
		t.Fatal(err)
	}
	host := config.Host{HostName: "server.example", Port: 2222}
	if !IsHostKnown(host) {
		t.Fatal("expected host to be known")
	}
	host.HostName = "other.example"
	if IsHostKnown(host) {
		t.Fatal("unexpected known host")
	}
}
