package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveHostsIsAtomicAndValidates(t *testing.T) {
	dir := t.TempDir()
	store := &Store{baseDir: dir, hostsFile: filepath.Join(dir, "hosts.json"), configFile: filepath.Join(dir, "ssh_config"), stateFile: filepath.Join(dir, "state.json")}
	host := Host{Alias: "prod", HostName: "10.0.0.10", User: "ubuntu", Port: 22}
	if err := store.SaveHosts([]Host{host}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.hostsFile); err != nil {
		t.Fatal(err)
	}
	host.Alias = "prod\nProxyCommand bad"
	if err := store.SaveHosts([]Host{host}); err == nil {
		t.Fatal("expected invalid alias to be rejected")
	}
}

func TestFirstRunState(t *testing.T) {
	dir := t.TempDir()
	store := &Store{baseDir: dir, hostsFile: filepath.Join(dir, "hosts.json"), stateFile: filepath.Join(dir, "state.json")}
	if !store.IsFirstRun() {
		t.Fatal("new store should be first run")
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkOnboardingComplete(); err != nil {
		t.Fatal(err)
	}
	if store.IsFirstRun() {
		t.Fatal("completed onboarding should not be first run")
	}
}

func TestLoadMigratesLegacyIdentityFile(t *testing.T) {
	dir := t.TempDir()
	store := &Store{baseDir: dir, hostsFile: filepath.Join(dir, "hosts.json")}
	legacy := `[{"alias":"prod","hostname":"server","port":22,"user":"deploy","identity_file":"/tmp/legacy"}]`
	if err := os.WriteFile(store.hostsFile, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts, err := store.LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts[0].IdentityFiles) != 1 || hosts[0].IdentityFiles[0] != "/tmp/legacy" || hosts[0].IdentityFile != "" {
		t.Fatalf("migration failed: %#v", hosts[0])
	}
	if err := store.SaveHosts(hosts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.hostsFile)
	if strings.Contains(string(data), "identity_file\"") {
		t.Fatalf("legacy field persisted: %s", data)
	}
}
