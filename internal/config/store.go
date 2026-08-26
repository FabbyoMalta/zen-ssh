package config

import (
	"crypto/sha256"
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
	Alias               string    `json:"alias"`
	HostName            string    `json:"hostname"`
	Port                int       `json:"port"`
	User                string    `json:"user"`
	Group               string    `json:"group,omitempty"`
	IdentityFile        string    `json:"identity_file,omitempty"` // campo legado; removido na proxima gravacao
	IdentityFiles       []string  `json:"identity_files,omitempty"`
	SSHOptions          []string  `json:"ssh_options,omitempty"`
	TermType            string    `json:"term_type,omitempty"`
	Source              string    `json:"source,omitempty"`
	SourcePath          string    `json:"source_path,omitempty"`
	Management          string    `json:"management,omitempty"`
	SourceFingerprint   string    `json:"source_fingerprint,omitempty"`
	ImportedFingerprint string    `json:"imported_fingerprint,omitempty"`
	KeySent             bool      `json:"key_sent"`
	KeyAuthStatus       string    `json:"key_auth_status,omitempty"`
	KeyAuthCheckedAt    time.Time `json:"key_auth_checked_at,omitempty"`
	KeyAuthError        string    `json:"key_auth_error,omitempty"`
	LastConnectedAt     time.Time `json:"last_connected_at,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (h Host) Address() string {
	return fmt.Sprintf("%s@%s", h.User, h.HostName)
}

const (
	ManagementManual   = "manual"
	ManagementManaged  = "managed"
	ManagementReadOnly = "readonly"
)

const (
	KeyAuthUnknown   = "unknown"
	KeyAuthValidated = "validated"
	KeyAuthFailed    = "failed"
)

const (
	TermSystem = "system"
	TermXterm  = "xterm"
)

func (h Host) PrimaryIdentity() string {
	if len(h.IdentityFiles) > 0 {
		return h.IdentityFiles[0]
	}
	return h.IdentityFile
}

func (h *Host) Normalize() {
	if len(h.IdentityFiles) == 0 && h.IdentityFile != "" {
		h.IdentityFiles = []string{h.IdentityFile}
	}
	h.IdentityFile = ""
	if h.Management == "" {
		if h.Source == "" || h.Source == "manual" {
			h.Management = ManagementManual
		} else {
			h.Management = ManagementManaged
		}
	}
	if h.KeyAuthStatus == "" {
		h.KeyAuthStatus = KeyAuthUnknown
	}
	if h.TermType == "" {
		h.TermType = TermSystem
	}
}

func HostFingerprint(h Host) string {
	payload := struct {
		Alias, HostName, User string
		Port                  int
		IdentityFiles         []string
		SSHOptions            []string
	}{h.Alias, h.HostName, h.User, h.Port, h.IdentityFiles, h.SSHOptions}
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type Store struct {
	baseDir    string
	hostsFile  string
	configFile string
	stateFile  string
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
		stateFile:  filepath.Join(baseDir, "state.json"),
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

func (s *Store) IsFirstRun() bool {
	if _, err := os.Stat(s.stateFile); err == nil {
		return false
	}
	if info, err := os.Stat(s.hostsFile); err == nil && info.Size() > 0 {
		return false
	}
	return true
}

func (s *Store) MarkOnboardingComplete() error {
	return atomicWriteFile(s.stateFile, []byte("{\"onboarding_complete\":true}\n"), 0o600)
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
	for i := range hosts {
		hosts[i].Normalize()
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
	for _, host := range hosts {
		if err := ValidateHost(host); err != nil {
			return fmt.Errorf("host %q: %w", host.Alias, err)
		}
	}
	for i := range hosts {
		hosts[i].Normalize()
	}

	sort.Slice(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Alias) < strings.ToLower(hosts[j].Alias)
	})

	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(s.hostsFile, data, 0o600)
}

func ValidateHost(h Host) error {
	if h.Alias == "" || h.HostName == "" || h.User == "" {
		return errors.New("alias, host e usuario sao obrigatorios")
	}
	if h.Port < 1 || h.Port > 65535 {
		return errors.New("porta deve estar entre 1 e 65535")
	}
	if invalidToken(h.Alias) || strings.ContainsAny(h.Alias, "*?!") {
		return errors.New("alias contem espacos, curingas ou caracteres de controle")
	}
	if invalidToken(h.HostName) {
		return errors.New("host contem espacos ou caracteres de controle")
	}
	if invalidToken(h.User) || strings.Contains(h.User, "@") {
		return errors.New("usuario invalido")
	}
	if strings.ContainsAny(h.Group+h.IdentityFile, "\r\n") {
		return errors.New("grupo ou caminho da chave contem quebra de linha")
	}
	for _, identity := range h.IdentityFiles {
		if strings.ContainsAny(identity, "\r\n") {
			return errors.New("caminho da chave contem quebra de linha")
		}
	}
	if h.Management != "" && h.Management != ManagementManual && h.Management != ManagementManaged && h.Management != ManagementReadOnly {
		return errors.New("modo de gerenciamento invalido")
	}
	if h.KeyAuthStatus != "" && h.KeyAuthStatus != KeyAuthUnknown && h.KeyAuthStatus != KeyAuthValidated && h.KeyAuthStatus != KeyAuthFailed {
		return errors.New("estado de autenticacao por chave invalido")
	}
	if h.TermType != "" && h.TermType != TermSystem && h.TermType != TermXterm {
		return errors.New("tipo de terminal invalido")
	}
	for _, option := range h.SSHOptions {
		if strings.ContainsAny(option, "\r\n") {
			return errors.New("opcao SSH contem quebra de linha")
		}
	}
	return nil
}

func invalidToken(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".zenssh-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func DefaultIdentityFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.ssh/id_ed25519"
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}
