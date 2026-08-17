package sshcfg

import (
	"os"
	"path/filepath"
	"testing"

	"zenssh/internal/config"
)

func TestSaveAllRollsBackWhenSSHValidationFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	original := config.Host{Alias: "prod", HostName: "server.example", User: "deploy", Port: 22, Management: config.ManagementManual}
	if err := SaveAll(store, []config.Host{original}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.HostsFile())
	if err != nil {
		t.Fatal(err)
	}
	invalid := original
	invalid.SSHOptions = []string{"DefinitelyNotAnSSHOption=yes"}
	if err := SaveAll(store, []config.Host{invalid}); err == nil {
		t.Fatal("expected OpenSSH validation failure")
	}
	after, err := os.ReadFile(store.HostsFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("hosts file was not rolled back\nbefore=%s\nafter=%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); err != nil {
		t.Fatal(err)
	}
}

func TestImportCandidatesAddsUpdatesAndRemoves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	host := config.Host{Alias: "prod", HostName: "old", User: "deploy", Port: 22, Source: "ssh-config", SourcePath: "/tmp/config", Management: config.ManagementReadOnly}
	host.SourceFingerprint = config.HostFingerprint(host)
	host.ImportedFingerprint = host.SourceFingerprint
	result, err := ImportCandidates(store, []Candidate{{Host: host, Selected: true, Status: "novo"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 {
		t.Fatalf("unexpected add result: %#v", result)
	}
	host.HostName = "new"
	host.SourceFingerprint = config.HostFingerprint(host)
	result, err = ImportCandidates(store, []Candidate{{Host: host, Selected: true, Status: "alterado"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("unexpected update result: %#v", result)
	}
	result, err = ImportCandidates(store, []Candidate{{Host: host, Selected: true, Status: "removido na origem", Remove: true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 {
		t.Fatalf("unexpected remove result: %#v", result)
	}
	hosts, err := store.LoadHosts()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("hosts after remove: %#v, %v", hosts, err)
	}
}
