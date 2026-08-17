package sshcfg

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"zenssh/internal/config"
)

type Candidate struct {
	Host     config.Host
	Selected bool
	Optional bool
	Status   string
	Remove   bool
}

type Discovery struct {
	Candidates []Candidate
	Keys       []string
}

func Discover(managedPath string) (Discovery, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Discovery{}, err
	}
	mainConfig := filepath.Join(home, ".ssh", "config")
	aliases, sources, err := collectAliases(mainConfig, managedPath)
	if err != nil && !os.IsNotExist(err) {
		return Discovery{}, err
	}

	result := Discovery{}
	seen := map[string]bool{}
	for _, alias := range aliases {
		seen[strings.ToLower(alias)] = true
		host, err := resolveAlias(alias)
		if err != nil {
			result.Candidates = append(result.Candidates, Candidate{Host: config.Host{Alias: alias, Source: "ssh-config", SourcePath: sources[strings.ToLower(alias)]}, Status: "falha ao resolver"})
			continue
		}
		host.Source = "ssh-config"
		host.SourcePath = sources[strings.ToLower(alias)]
		host.Management = config.ManagementReadOnly
		host.SourceFingerprint = config.HostFingerprint(host)
		host.ImportedFingerprint = host.SourceFingerprint
		result.Candidates = append(result.Candidates, Candidate{Host: host, Selected: true, Status: "novo"})
	}

	hostCandidates, _ := parseEtcHosts("/etc/hosts")
	for _, host := range hostCandidates {
		if seen[strings.ToLower(host.Alias)] {
			continue
		}
		host.SourceFingerprint = config.HostFingerprint(host)
		host.ImportedFingerprint = host.SourceFingerprint
		result.Candidates = append(result.Candidates, Candidate{Host: host, Optional: true, Status: "novo"})
		seen[strings.ToLower(host.Alias)] = true
	}
	result.Keys = discoverKeys(filepath.Join(home, ".ssh"))
	slices.SortFunc(result.Candidates, func(a, b Candidate) int {
		if a.Optional != b.Optional {
			if a.Optional {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.Host.Alias), strings.ToLower(b.Host.Alias))
	})
	return result, nil
}

func ReconcileCandidates(discovered []Candidate, existing []config.Host) []Candidate {
	byAlias := make(map[string]config.Host, len(existing))
	seen := map[string]bool{}
	for _, host := range existing {
		byAlias[strings.ToLower(host.Alias)] = host
	}
	for i := range discovered {
		key := strings.ToLower(discovered[i].Host.Alias)
		seen[key] = true
		current, ok := byAlias[key]
		if !ok {
			continue
		}
		discovered[i].Host.Management = current.Management
		if discovered[i].Status == "falha ao resolver" {
			discovered[i].Host = current
			discovered[i].Selected = false
			continue
		}
		discovered[i].Selected = false
		if current.SourceFingerprint == "" || current.ImportedFingerprint == "" {
			discovered[i].Status = "atualizar metadados"
			discovered[i].Selected = true
			continue
		}
		localChanged := current.ImportedFingerprint != "" && config.HostFingerprint(current) != current.ImportedFingerprint
		sourceChanged := current.SourceFingerprint != "" && discovered[i].Host.SourceFingerprint != current.SourceFingerprint
		switch {
		case localChanged && sourceChanged:
			discovered[i].Status = "conflito"
		case sourceChanged:
			discovered[i].Status = "alterado"
			discovered[i].Selected = true
		case localChanged:
			discovered[i].Status = "editado localmente"
		default:
			discovered[i].Status = "sem mudancas"
		}
	}
	for _, host := range existing {
		if (host.Source != "ssh-config" && host.Source != "etc-hosts") || seen[strings.ToLower(host.Alias)] {
			continue
		}
		discovered = append(discovered, Candidate{Host: host, Status: "removido na origem", Remove: true})
	}
	return discovered
}

func resolveAlias(alias string) (config.Host, error) {
	cmd := exec.Command("ssh", "-G", alias)
	output, err := cmd.Output()
	if err != nil {
		return config.Host{}, fmt.Errorf("ssh -G %s: %w", alias, err)
	}
	return parseResolvedConfig(alias, output)
}

func parseResolvedConfig(alias string, output []byte) (config.Host, error) {
	h := config.Host{Alias: alias, Port: 22}
	var identities []string
	var options []string
	s := bufio.NewScanner(bytes.NewReader(output))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		value := strings.Join(fields[1:], " ")
		switch strings.ToLower(fields[0]) {
		case "hostname":
			h.HostName = value
		case "user":
			h.User = value
		case "port":
			if p, err := strconv.Atoi(value); err == nil {
				h.Port = p
			}
		case "identityfile":
			identities = append(identities, expandPath(value))
		default:
			if option, ok := resolvedOption(fields[0], value); ok {
				options = append(options, option)
			}
		}
	}
	if err := s.Err(); err != nil {
		return config.Host{}, err
	}
	if h.HostName == "" {
		h.HostName = alias
	}
	h.IdentityFiles = identities
	h.SSHOptions = options
	h.Management = config.ManagementManaged
	return h, nil
}

func resolvedOption(key, value string) (string, bool) {
	canonical := map[string]string{
		"certificatefile": "CertificateFile", "proxyjump": "ProxyJump", "proxycommand": "ProxyCommand",
		"forwardagent": "ForwardAgent", "identitiesonly": "IdentitiesOnly", "preferredauthentications": "PreferredAuthentications",
		"pubkeyauthentication": "PubkeyAuthentication", "passwordauthentication": "PasswordAuthentication",
		"kbdinteractiveauthentication": "KbdInteractiveAuthentication", "addkeystoagent": "AddKeysToAgent",
		"identityagent": "IdentityAgent", "requesttty": "RequestTTY", "remotecommand": "RemoteCommand",
		"localforward": "LocalForward", "remoteforward": "RemoteForward", "dynamicforward": "DynamicForward",
		"hostkeyalias": "HostKeyAlias", "knownhostscommand": "KnownHostsCommand", "userknownhostsfile": "UserKnownHostsFile",
		"globalknownhostsfile": "GlobalKnownHostsFile", "stricthostkeychecking": "StrictHostKeyChecking",
		"controlmaster": "ControlMaster", "controlpath": "ControlPath", "controlpersist": "ControlPersist",
		"tcpkeepalive": "TCPKeepAlive", "serveraliveinterval": "ServerAliveInterval", "serveralivecountmax": "ServerAliveCountMax",
	}
	name, ok := canonical[strings.ToLower(key)]
	if !ok {
		return "", false
	}
	if (name == "ProxyJump" || name == "ProxyCommand" || name == "IdentityAgent" || name == "CertificateFile" || name == "RemoteCommand") && value == "none" {
		return "", false
	}
	return name + "=" + value, true
}

func collectAliases(mainConfig, managedPath string) ([]string, map[string]string, error) {
	visited, sources := map[string]bool{}, map[string]string{}
	rootDir := filepath.Dir(mainConfig)
	var aliases []string
	var walk func(string) error
	walk = func(path string) error {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if visited[absolute] {
			return nil
		}
		visited[absolute] = true
		if managedPath != "" {
			if managed, _ := filepath.Abs(managedPath); managed == absolute {
				return nil
			}
		}
		file, err := os.Open(absolute)
		if err != nil {
			return err
		}
		defer file.Close()
		s := bufio.NewScanner(file)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := splitDirective(line)
			if !ok {
				continue
			}
			switch strings.ToLower(key) {
			case "host":
				for _, alias := range parseAliases(value) {
					if !isImportableAlias(alias) {
						continue
					}
					lower := strings.ToLower(alias)
					if _, exists := sources[lower]; !exists {
						aliases = append(aliases, alias)
						sources[lower] = absolute
					}
				}
			case "include":
				for _, include := range expandIncludePaths(rootDir, value) {
					if err := walk(include); err != nil && !os.IsNotExist(err) {
						return err
					}
				}
			}
		}
		return s.Err()
	}
	err := walk(mainConfig)
	return aliases, sources, err
}

func parseEtcHosts(path string) ([]config.Host, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	user := os.Getenv("USER")
	var hosts []config.Host
	s := bufio.NewScanner(file)
	for s.Scan() {
		line := strings.TrimSpace(strings.SplitN(s.Text(), "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			continue
		}
		if fields[0] == "127.0.0.1" || fields[0] == "::1" || fields[0] == "255.255.255.255" {
			continue
		}
		for _, alias := range fields[1:] {
			if alias == "localhost" || strings.ContainsAny(alias, "*?!") {
				continue
			}
			hosts = append(hosts, config.Host{Alias: alias, HostName: fields[0], Port: 22, User: user, Source: "etc-hosts", SourcePath: path, Management: config.ManagementReadOnly})
		}
	}
	return hosts, s.Err()
}

func discoverKeys(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".pub") || strings.HasSuffix(entry.Name(), "known_hosts") || strings.HasPrefix(entry.Name(), "config") || strings.HasSuffix(entry.Name(), ".pem.pub") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		prefix := make([]byte, 512)
		n, readErr := file.Read(prefix)
		file.Close()
		if readErr != nil && n == 0 {
			continue
		}
		if bytes.Contains(prefix[:n], []byte("PRIVATE KEY-----")) {
			keys = append(keys, path)
		}
	}
	slices.Sort(keys)
	return keys
}
