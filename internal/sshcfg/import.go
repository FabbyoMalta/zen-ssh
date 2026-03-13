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
	Skipped  int
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
	home, err := os.UserHomeDir()
	if err != nil {
		return ImportResult{}, err
	}

	mainConfig := filepath.Join(home, ".ssh", "config")
	imported, err := ParseOpenSSHConfigs(mainConfig, store.ManagedSSHConfigPath())
	if err != nil {
		return ImportResult{}, err
	}

	existing, err := store.LoadHosts()
	if err != nil {
		return ImportResult{}, err
	}

	now := time.Now()
	byAlias := make(map[string]config.Host, len(existing))
	for _, host := range existing {
		byAlias[host.Alias] = host
	}

	result := ImportResult{}
	for _, host := range imported {
		_, exists := byAlias[host.Alias]
		if exists {
			result.Skipped++
			continue
		}
		host.CreatedAt = now
		host.UpdatedAt = now
		byAlias[host.Alias] = host
		existing = append(existing, host)
		result.Imported++
	}

	if result.Imported == 0 {
		return result, nil
	}
	if err := store.SaveHosts(existing); err != nil {
		return ImportResult{}, err
	}
	if err := WriteManagedConfig(store.ManagedSSHConfigPath(), existing); err != nil {
		return ImportResult{}, err
	}
	return result, nil
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
			Alias:        alias,
			HostName:     hostName,
			Port:         port,
			User:         user,
			Group:        b.group,
			IdentityFile: identity,
			SSHOptions:   append([]string{}, b.sshOptions...),
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
	fields := strings.Fields(value)
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
	fields := strings.Fields(value)
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

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
