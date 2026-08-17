package sshcfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"zenssh/internal/config"
)

func TestParseResolvedConfig(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_work")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := []byte("hostname 10.0.0.8\nuser deploy\nport 2202\nidentityfile /missing\nidentityfile " + key + "\n")
	host, err := parseResolvedConfig("prod", output)
	if err != nil {
		t.Fatal(err)
	}
	if host.HostName != "10.0.0.8" || host.User != "deploy" || host.Port != 2202 || len(host.IdentityFiles) != 2 || host.IdentityFiles[1] != key {
		t.Fatalf("unexpected host: %#v", host)
	}
}

func TestParseResolvedConfigPreservesEffectiveOptions(t *testing.T) {
	output := []byte("hostname bastion.internal\nuser deploy\nport 22\nproxyjump jump.example\nidentitiesonly no\nforwardagent yes\ncertificatefile /tmp/id-cert.pub\n")
	host, err := parseResolvedConfig("prod", output)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ProxyJump=jump.example", "IdentitiesOnly=no", "ForwardAgent=yes", "CertificateFile=/tmp/id-cert.pub"}
	if !slices.Equal(host.SSHOptions, want) {
		t.Fatalf("options = %#v, want %#v", host.SSHOptions, want)
	}
}

func TestReconcileCandidatesDetectsChangeAndConflict(t *testing.T) {
	old := config.Host{Alias: "prod", HostName: "old", User: "deploy", Port: 22, Source: "ssh-config", Management: config.ManagementManaged}
	old.SourceFingerprint = config.HostFingerprint(old)
	old.ImportedFingerprint = old.SourceFingerprint
	changed := old
	changed.HostName = "new"
	changed.SourceFingerprint = config.HostFingerprint(changed)
	candidates := ReconcileCandidates([]Candidate{{Host: changed}}, []config.Host{old})
	if candidates[0].Status != "alterado" || !candidates[0].Selected {
		t.Fatalf("unexpected candidate: %#v", candidates[0])
	}
	old.User = "local-edit"
	candidates = ReconcileCandidates([]Candidate{{Host: changed}}, []config.Host{old})
	if candidates[0].Status != "conflito" || candidates[0].Selected {
		t.Fatalf("unexpected conflict: %#v", candidates[0])
	}
}

func TestCollectAliasesFollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "work.conf")
	if err := os.WriteFile(included, []byte("Host staging *.internal\n  User deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(main, []byte("Host prod\n  HostName 10.0.0.1\nInclude work.conf\nHost *\n  ServerAliveInterval 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	aliases, sources, err := collectAliases(main, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 2 || aliases[0] != "prod" || aliases[1] != "staging" {
		t.Fatalf("unexpected aliases: %#v", aliases)
	}
	if sources["staging"] != included {
		t.Fatalf("unexpected source: %q", sources["staging"])
	}
}

func TestCollectAliasesSupportsQuotedIncludeAndInlineComment(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "with space")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	included := filepath.Join(sub, "hosts.conf")
	if err := os.WriteFile(included, []byte("Host quoted # comentario\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(main, []byte("Include \"with space/*.conf\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	aliases, _, err := collectAliases(main, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0] != "quoted" {
		t.Fatalf("aliases = %#v", aliases)
	}
}

func TestParseEtcHostsFiltersLocalhost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	data := "127.0.0.1 localhost\n10.0.0.2 db db.internal # database\n::1 localhost\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts, err := parseEtcHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Alias != "db" || hosts[1].Alias != "db.internal" {
		t.Fatalf("unexpected hosts: %#v", hosts)
	}
}
