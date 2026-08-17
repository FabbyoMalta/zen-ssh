package sshcfg

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"zenssh/internal/config"
)

type ImportResult struct {
	Imported int
	Updated  int
	Removed  int
	Skipped  int
}

func ImportCandidates(store *config.Store, candidates []Candidate) (ImportResult, error) {
	existing, err := store.LoadHosts()
	if err != nil {
		return ImportResult{}, err
	}
	byAlias := make(map[string]int, len(existing))
	for i, host := range existing {
		byAlias[strings.ToLower(host.Alias)] = i
	}
	now := time.Now()
	result := ImportResult{}
	for _, candidate := range candidates {
		if !candidate.Selected {
			continue
		}
		key := strings.ToLower(candidate.Host.Alias)
		index, exists := byAlias[key]
		if candidate.Remove {
			if exists {
				existing = append(existing[:index], existing[index+1:]...)
				result.Removed++
				byAlias = make(map[string]int, len(existing))
				for i, host := range existing {
					byAlias[strings.ToLower(host.Alias)] = i
				}
			}
			continue
		}
		host := candidate.Host
		if err := config.ValidateHost(host); err != nil {
			return ImportResult{}, fmt.Errorf("candidato %q: %w", host.Alias, err)
		}
		if exists {
			if candidate.Status == "sem mudancas" || candidate.Status == "editado localmente" {
				result.Skipped++
				continue
			}
			host.CreatedAt = existing[index].CreatedAt
			host.UpdatedAt = now
			host.KeySent = existing[index].KeySent
			if config.HostFingerprint(existing[index]) == config.HostFingerprint(host) {
				host.KeyAuthStatus = existing[index].KeyAuthStatus
				host.KeyAuthCheckedAt = existing[index].KeyAuthCheckedAt
				host.KeyAuthError = existing[index].KeyAuthError
			} else {
				host.KeyAuthStatus = config.KeyAuthUnknown
			}
			host.Group = existing[index].Group
			host.ImportedFingerprint = config.HostFingerprint(host)
			existing[index] = host
			result.Updated++
			continue
		}
		host.CreatedAt, host.UpdatedAt = now, now
		host.ImportedFingerprint = config.HostFingerprint(host)
		existing = append(existing, host)
		byAlias[key] = len(existing) - 1
		result.Imported++
	}
	if err := SaveAll(store, existing); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

type importBlock struct {
	aliases      []string
	hostName     string
	user         string
	group        string
	identityFile string
	port         int
	sshOptions   []string
}

func ImportHosts(store *config.Store) (ImportResult, error) {
	discovery, err := Discover(store.ManagedSSHConfigPath())
	if err != nil {
		return ImportResult{}, err
	}
	existing, err := store.LoadHosts()
	if err != nil {
		return ImportResult{}, err
	}
	discovery.Candidates = ReconcileCandidates(discovery.Candidates, existing)
	return ImportCandidates(store, discovery.Candidates)
}

func ParseOpenSSHConfigs(mainConfig string, managedPath string) ([]config.Host, error) {
	visited := map[string]bool{}
	hostsByAlias := map[string]config.Host{}
	if err := parseConfigFile(mainConfig, managedPath, visited, hostsByAlias); err != nil {
		if os.IsNotExist(err) {
			return []config.Host{}, nil
		}
		return nil, err
	}

	hosts := make([]config.Host, 0, len(hostsByAlias))
	for _, host := range hostsByAlias {
		hosts = append(hosts, host)
	}
	slices.SortFunc(hosts, func(a, b config.Host) int {
		return strings.Compare(strings.ToLower(a.Alias), strings.ToLower(b.Alias))
	})
	return hosts, nil
}

func parseConfigFile(path string, managedPath string, visited map[string]bool, hostsByAlias map[string]config.Host) error {
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if visited[resolvedPath] {
		return nil
	}
	visited[resolvedPath] = true

	if managedPath != "" {
		resolvedManagedPath, err := filepath.Abs(managedPath)
		if err == nil && resolvedManagedPath == resolvedPath {
			return nil
		}
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	current := importBlock{}
	flush := func() {
		for _, host := range current.toHosts() {
			if _, exists := hostsByAlias[host.Alias]; exists {
				continue
			}
			hostsByAlias[host.Alias] = host
		}
		current = importBlock{}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := splitDirective(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			flush()
			current.aliases = parseAliases(value)
		case "include":
			flush()
			for _, includePath := range expandIncludePaths(filepath.Dir(resolvedPath), value) {
				if err := parseConfigFile(includePath, managedPath, visited, hostsByAlias); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		case "match":
			flush()
		case "hostname":
			current.hostName = value
		case "user":
			current.user = value
		case "port":
			port, err := strconv.Atoi(value)
			if err == nil && port > 0 {
				current.port = port
			}
		case "identityfile":
			current.identityFile = expandPath(value)
		case "tag":
			current.group = value
		default:
			current.sshOptions = append(current.sshOptions, fmt.Sprintf("%s %s", key, value))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	flush()
	return nil
}

func (b importBlock) toHosts() []config.Host {
	if len(b.aliases) == 0 {
		return nil
	}

	hosts := make([]config.Host, 0, len(b.aliases))
	for _, alias := range b.aliases {
		if !isImportableAlias(alias) {
			continue
		}
		hostName := b.hostName
		if hostName == "" {
			hostName = alias
		}
		user := b.user
		if user == "" {
			user = os.Getenv("USER")
		}
		if user == "" {
			continue
		}
		port := b.port
		if port == 0 {
			port = 22
		}
		identity := b.identityFile
		if identity == "" {
			identity = config.DefaultIdentityFile()
		}
		hosts = append(hosts, config.Host{
			Alias:         alias,
			HostName:      hostName,
			Port:          port,
			User:          user,
			Group:         b.group,
			IdentityFiles: []string{identity},
			SSHOptions:    append([]string{}, b.sshOptions...),
			Management:    config.ManagementManaged,
		})
	}
	return hosts
}

func splitDirective(line string) (string, string, bool) {
	if strings.Contains(line, "=") {
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			return "", "", false
		}
		return key, value, true
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], " "), true
}

func parseAliases(value string) []string {
	fields := tokenizeSSHValue(value)
	aliases := make([]string, 0, len(fields))
	for _, field := range fields {
		aliases = append(aliases, strings.TrimSpace(field))
	}
	return aliases
}

func isImportableAlias(alias string) bool {
	if alias == "" || alias == "*" {
		return false
	}
	return !strings.ContainsAny(alias, "*?!")
}

func expandIncludePaths(baseDir string, value string) []string {
	fields := tokenizeSSHValue(value)
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		field = expandPath(field)
		if !filepath.IsAbs(field) {
			field = filepath.Join(baseDir, field)
		}
		matches, err := filepath.Glob(field)
		if err != nil || len(matches) == 0 {
			paths = append(paths, field)
			continue
		}
		paths = append(paths, matches...)
	}
	return paths
}

func tokenizeSSHValue(value string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	for _, r := range value {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '#' {
			break
		}
		if r == ' ' || r == '\t' {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
	return fields
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
