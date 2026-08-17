package ui

import (
	"errors"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"zenssh/internal/config"
	"zenssh/internal/sshcfg"
)

func TestVisibleHostsFiltersAllMetadata(t *testing.T) {
	m := Model{hosts: []config.Host{{Alias: "prod", HostName: "10.0.0.1", User: "deploy", Group: "production", Source: "ssh-config"}, {Alias: "dev", HostName: "dev.local", User: "me"}}}
	for _, query := range []string{"prod", "10.0.0.1", "production", "ssh-config"} {
		m.query = query
		if hosts := m.visibleHosts(); len(hosts) != 1 || hosts[0].Alias != "prod" {
			t.Fatalf("query %q returned %#v", query, hosts)
		}
	}
}

func TestImportCanAssociateDiscoveredKeyAndCertificate(t *testing.T) {
	dir := t.TempDir()
	key := dir + "/id_test"
	if err := os.WriteFile(key+"-cert.pub", []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Model{mode: modeImport, importer: importState{keys: []string{key}, candidates: []sshcfg.Candidate{{Host: config.Host{Alias: "prod"}}}}}
	updated, _ := m.updateImport(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := updated.(Model).importer.candidates[0].Host
	if got.PrimaryIdentity() != key || len(got.SSHOptions) != 1 || got.SSHOptions[0] != "CertificateFile="+key+"-cert.pub" {
		t.Fatalf("unexpected association: %#v", got)
	}
}

func TestFirstRunStartsInReviewWithoutWritingSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	model, err := NewModel(store)
	if err != nil {
		t.Fatal(err)
	}
	if model.mode != modeImport {
		t.Fatalf("mode = %v", model.mode)
	}
	if _, err := os.Stat(home + "/.ssh/config"); !os.IsNotExist(err) {
		t.Fatalf("SSH config was touched: %v", err)
	}
}

func TestKeyStatusLabels(t *testing.T) {
	host := config.Host{Management: config.ManagementManual}
	if got := keyStatusLabel(host); got != "sem-chave" {
		t.Fatalf("got %q", got)
	}
	host.IdentityFiles = []string{"/tmp/key"}
	if got := keyStatusLabel(host); got != "configurada" {
		t.Fatalf("got %q", got)
	}
	host.KeySent = true
	if got := keyStatusLabel(host); got != "envio-registrado" {
		t.Fatalf("got %q", got)
	}
	host.KeyAuthStatus = config.KeyAuthValidated
	if got := keyStatusLabel(host); got != "validada" {
		t.Fatalf("got %q", got)
	}
}

func TestPersistAuthResultStoresFailureAndSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	host := config.Host{Alias: "prod", HostName: "server", User: "deploy", Port: 22, Source: "ssh-config", Management: config.ManagementReadOnly}
	if err := sshcfg.SaveAll(store, []config.Host{host}); err != nil {
		t.Fatal(err)
	}
	checked := time.Now()
	if err := persistAuthResult(store, authResultMsg{alias: "prod", checkedAt: checked, err: errors.New("permission denied")}); err != nil {
		t.Fatal(err)
	}
	hosts, _ := store.LoadHosts()
	if hosts[0].KeyAuthStatus != config.KeyAuthFailed || hosts[0].KeyAuthError == "" {
		t.Fatalf("failure not stored: %#v", hosts[0])
	}
	if err := persistAuthResult(store, authResultMsg{alias: "prod", checkedAt: checked}); err != nil {
		t.Fatal(err)
	}
	hosts, _ = store.LoadHosts()
	if hosts[0].KeyAuthStatus != config.KeyAuthValidated || hosts[0].KeyAuthError != "" {
		t.Fatalf("success not stored: %#v", hosts[0])
	}
}
