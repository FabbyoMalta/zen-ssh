package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const appDirName = "zenssh"

type Host struct {
	Alias        string    `json:"alias"`
	HostName     string    `json:"hostname"`
	Port         int       `json:"port"`
	User         string    `json:"user"`
	Group        string    `json:"group,omitempty"`
	IdentityFile string    `json:"identity_file,omitempty"`
	SSHOptions   []string  `json:"ssh_options,omitempty"`
	KeySent      bool      `json:"key_sent"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (h Host) Address() string {
	return fmt.Sprintf("%s@%s", h.User, h.HostName)
}

type Store struct {
	baseDir    string
	hostsFile  string
	configFile string
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Join(home, ".config", appDirName)
	return &Store{
		baseDir:    baseDir,
		hostsFile:  filepath.Join(baseDir, "hosts.json"),
		configFile: filepath.Join(baseDir, "ssh_config"),
	}, nil
}

func (s *Store) BaseDir() string {
	return s.baseDir
}

func (s *Store) HostsFile() string {
	return s.hostsFile
}

func (s *Store) ManagedSSHConfigPath() string {
	return s.configFile
}

func (s *Store) Ensure() error {
	return os.MkdirAll(s.baseDir, 0o700)
}

func (s *Store) LoadHosts() ([]Host, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.hostsFile)
	if errors.Is(err, os.ErrNotExist) {
		return []Host{}, nil
	}
	if err != nil {
		return nil, err
	}

	var hosts []Host
	if len(data) == 0 {
		return []Host{}, nil
	}
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, err
	}

	sort.Slice(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Alias) < strings.ToLower(hosts[j].Alias)
	})
	return hosts, nil
}

func (s *Store) SaveHosts(hosts []Host) error {
	if err := s.Ensure(); err != nil {
		return err
	}

	sort.Slice(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Alias) < strings.ToLower(hosts[j].Alias)
	})

	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.hostsFile, data, 0o600)
}

func DefaultIdentityFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.ssh/id_ed25519"
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}
