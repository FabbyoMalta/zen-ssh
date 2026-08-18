package ui

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"zenssh/internal/config"
	"zenssh/internal/sshcfg"
	"zenssh/internal/style"
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

func TestVisibleHostsFiltersByGroupAndSearch(t *testing.T) {
	m := Model{hosts: []config.Host{
		{Alias: "prod-api", Group: "producao"},
		{Alias: "prod-db", Group: "producao"},
		{Alias: "dev-api", Group: "desenvolvimento"},
		{Alias: "lab"},
	}}
	m.groupFilter = "producao"
	m.query = "api"
	got := m.visibleHosts()
	if len(got) != 1 || got[0].Alias != "prod-api" {
		t.Fatalf("unexpected filtered hosts: %#v", got)
	}
	m.groupFilter = ungroupedFilter
	m.query = ""
	got = m.visibleHosts()
	if len(got) != 1 || got[0].Alias != "lab" {
		t.Fatalf("unexpected ungrouped hosts: %#v", got)
	}
}

func TestGroupTabsAreUniqueAndSorted(t *testing.T) {
	m := Model{hosts: []config.Host{{Group: "producao"}, {Group: "Dev"}, {Group: "PRODUCAO"}, {}}}
	got := m.groupTabs()
	want := []string{"", "Dev", "producao", ungroupedFilter}
	if !slices.Equal(got, want) {
		t.Fatalf("tabs = %#v, want %#v", got, want)
	}
}

func TestBulkGroupUpdatesOnlySelectedHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := config.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	hosts := []config.Host{
		{Alias: "one", HostName: "one.local", User: "root", Port: 22, Management: config.ManagementManual},
		{Alias: "two", HostName: "two.local", User: "root", Port: 22, Management: config.ManagementManual},
	}
	if err := sshcfg.SaveAll(store, hosts); err != nil {
		t.Fatal(err)
	}
	input := textinput.New()
	input.SetValue("infra")
	m := Model{store: store, hosts: hosts, selected: map[string]bool{"one": true}, groupInput: input, theme: style.New()}
	updated, _ := m.updateBulkGroup(tea.KeyMsg{Type: tea.KeyEnter})
	result := updated.(Model)
	if result.hosts[0].Group != "infra" || result.hosts[1].Group != "" {
		t.Fatalf("unexpected groups: %#v", result.hosts)
	}
	if len(result.selected) != 0 || result.groupFilter != "infra" {
		t.Fatalf("selection/filter not updated: %#v / %q", result.selected, result.groupFilter)
	}
}

func TestSelectionMarkersOnlyAppearInSelectionMode(t *testing.T) {
	layout := calculateLayout(80, 30)
	m := Model{
		theme: style.New(), layout: layout, viewport: viewport.New(layout.listWidth, layout.listHeight),
		hosts:    []config.Host{{Alias: "prod", HostName: "prod.local", User: "root", Port: 22}},
		selected: map[string]bool{}, knownHosts: map[string]bool{},
	}
	if view := m.renderDashboard(); strings.Contains(view, "[ ]") {
		t.Fatalf("selection marker visible outside selection mode: %q", view)
	}
	updated, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(Model)
	if !m.selectionMode || !strings.Contains(m.renderDashboard(), "[ ]") {
		t.Fatal("selection mode did not reveal markers")
	}
	updated, _ = m.updateList(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if !m.selected["prod"] || !strings.Contains(m.renderDashboard(), "[x]") {
		t.Fatal("space did not select the current host")
	}
}

func TestCalculateLayoutVariants(t *testing.T) {
	tests := []struct {
		width int
		want  layoutVariant
	}{{50, layoutCompact}, {80, layoutStacked}, {120, layoutSplit}}
	for _, test := range tests {
		if got := calculateLayout(test.width, 30).variant; got != test.want {
			t.Errorf("width %d: got %v, want %v", test.width, got, test.want)
		}
	}
}

func TestDashboardRendersAtResponsiveWidths(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		layout := calculateLayout(width, 30)
		h := help.New()
		m := Model{
			theme: style.New(), keys: newKeyMap(), help: h, layout: layout,
			viewport:   viewport.New(layout.listWidth, layout.listHeight),
			hosts:      []config.Host{{Alias: "production-api", HostName: "api.example.com", User: "deploy", Port: 22, IdentityFiles: []string{"/tmp/id_ed25519"}}},
			knownHosts: map[string]bool{"production-api": true},
		}
		view := m.renderDashboard()
		if !strings.Contains(view, "production-api") || !strings.Contains(view, "deploy@api") {
			t.Fatalf("width %d omitted host data: %q", width, view)
		}
	}
}

func TestFormSupportsMultipleIdentityInputs(t *testing.T) {
	form := newForm(config.Host{Alias: "prod", HostName: "prod.example.com", User: "deploy", Port: 22, IdentityFiles: []string{"/tmp/key-a", "/tmp/key-b"}})
	if len(form.identities) != 2 {
		t.Fatalf("identities = %d", len(form.identities))
	}
	host, err := form.host()
	if err != nil {
		t.Fatal(err)
	}
	if len(host.IdentityFiles) != 2 || host.IdentityFiles[1] != "/tmp/key-b" {
		t.Fatalf("unexpected identities: %#v", host.IdentityFiles)
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
