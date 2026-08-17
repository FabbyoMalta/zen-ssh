package sshcfg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"zenssh/internal/config"
)

const legacyIncludeMarker = "Include ~/.config/zenssh/ssh_config"

type ProbeResult struct {
	Host    config.Host
	Message string
}

func ValidateKeyAuthentication(host config.Host) error {
	if host.PrimaryIdentity() == "" && host.Management != config.ManagementReadOnly {
		return fmt.Errorf("nenhuma chave associada ao host")
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "PreferredAuthentications=publickey",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "ConnectTimeout=5",
	}
	if host.Management == config.ManagementReadOnly {
		args = append(args, host.Alias, "true")
	} else {
		args = append(args, sshArgs(host, host.SSHOptions)...)
		args = append(args, "true")
	}
	cmd := exec.Command("ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return err
		}
		return classifyCompatError(err, message)
	}
	return nil
}

func IsHostKnown(host config.Host) bool {
	query := host.HostName
	for _, option := range host.SSHOptions {
		parts := strings.SplitN(option, "=", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "HostKeyAlias") {
			query = parts[1]
			break
		}
	}
	if host.Port != 0 && host.Port != 22 {
		query = fmt.Sprintf("[%s]:%d", query, host.Port)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	files := []string{filepath.Join(home, ".ssh", "known_hosts"), filepath.Join(home, ".ssh", "known_hosts2")}
	for _, option := range host.SSHOptions {
		parts := strings.SplitN(option, "=", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "UserKnownHostsFile") {
			continue
		}
		files = nil
		for _, path := range strings.Fields(parts[1]) {
			files = append(files, expandPath(path))
		}
	}
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			continue
		}
		if exec.Command("ssh-keygen", "-F", query, "-f", file).Run() == nil {
			return true
		}
	}
	return false
}

func EnsureMainConfigIncludes(managedPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	sshDir := filepath.Join(home, ".ssh")
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}

	data, err := os.ReadFile(mainConfig)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := string(data)
	includeMarker := "Include " + managedPath
	if strings.Contains(content, includeMarker) {
		return ensureIncludePlacedFirst(mainConfig, content, includeMarker)
	}
	if strings.Contains(content, legacyIncludeMarker) {
		content = strings.ReplaceAll(content, legacyIncludeMarker, includeMarker)
		return ensureIncludePlacedFirst(mainConfig, content, includeMarker)
	}
	if strings.Contains(content, managedPath) {
		return ensureIncludePlacedFirst(mainConfig, content, includeMarker)
	}

	return ensureIncludePlacedFirst(mainConfig, content, includeMarker)
}

func ensureIncludePlacedFirst(mainConfig, content, includeMarker string) error {
	lines := strings.Split(content, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == includeMarker || trimmed == legacyIncludeMarker || trimmed == "# Managed by ZenSSH" {
			continue
		}
		filtered = append(filtered, line)
	}

	for len(filtered) > 0 && strings.TrimSpace(filtered[0]) == "" {
		filtered = filtered[1:]
	}

	result := "# Managed by ZenSSH\n" + includeMarker + "\n"
	if len(filtered) > 0 {
		result += "\n" + strings.Join(filtered, "\n")
	}
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	if result == content {
		return nil
	}
	if content != "" {
		if err := backupOnce(mainConfig); err != nil {
			return err
		}
	}
	return atomicWriteFile(mainConfig, []byte(result), 0o600)
}

func RenderManagedConfig(hosts []config.Host) string {
	var b strings.Builder
	b.WriteString("# Managed by ZenSSH. Do not edit manually.\n\n")

	for _, host := range hosts {
		if host.Management == config.ManagementReadOnly {
			continue
		}
		fmt.Fprintf(&b, "Host %s\n", host.Alias)
		fmt.Fprintf(&b, "  HostName %s\n", host.HostName)
		if host.User != "" {
			fmt.Fprintf(&b, "  User %s\n", host.User)
		}
		if host.Port > 0 {
			fmt.Fprintf(&b, "  Port %d\n", host.Port)
		}
		for _, identity := range host.IdentityFiles {
			fmt.Fprintf(&b, "  IdentityFile %s\n", identity)
		}
		if host.Management == config.ManagementManual {
			b.WriteString("  ServerAliveInterval 30\n")
			b.WriteString("  ServerAliveCountMax 3\n")
		}
		for _, option := range host.SSHOptions {
			fmt.Fprintf(&b, "  %s\n", option)
		}
		if host.Group != "" {
			fmt.Fprintf(&b, "  # Group: %s\n", host.Group)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func WriteManagedConfig(path string, hosts []config.Host) error {
	content := []byte(RenderManagedConfig(hosts))
	if err := validateManagedConfig(filepath.Dir(path), content, hosts); err != nil {
		return err
	}
	if err := atomicWriteFile(path, content, 0o600); err != nil {
		return err
	}
	return EnsureMainConfigIncludes(path)
}

func validateManagedConfig(dir string, content []byte, hosts []config.Host) error {
	tmp, err := os.CreateTemp(dir, ".zenssh-validate-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	for _, host := range hosts {
		if host.Management == config.ManagementReadOnly {
			continue
		}
		cmd := exec.Command("ssh", "-F", name, "-G", host.Alias)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("configuracao SSH invalida para %s: %s", host.Alias, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func backupOnce(path string) error {
	backup := path + ".zenssh.bak"
	if _, err := os.Stat(backup); err == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return atomicWriteFile(backup, data, 0o600)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	target := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		target = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".zenssh-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
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
	return os.Rename(name, target)
}

func GenerateKey(identityFile string) error {
	if _, err := os.Stat(identityFile); err == nil {
		return nil
	}

	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", identityFile, "-N", "")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func PushKey(host config.Host) error {
	_, cmd, err := PreparePushKey(host)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func Connect(host config.Host) (config.Host, error) {
	host, cmd, err := PrepareConnect(host)
	if err != nil {
		return host, err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return host, err
	}
	return host, nil
}

func PrepareConnect(host config.Host) (config.Host, *exec.Cmd, error) {
	if host.Management == config.ManagementReadOnly {
		return host, exec.Command("ssh", host.Alias), nil
	}
	options, err := resolveCompatOptions(host)
	if err != nil {
		return host, nil, err
	}

	host.SSHOptions = options
	args := sshArgs(host, options)
	cmd := exec.Command("ssh", args...)
	return host, cmd, nil
}

func PreparePushKey(host config.Host) (config.Host, *exec.Cmd, error) {
	if host.Management == config.ManagementReadOnly {
		identity := host.PrimaryIdentity()
		if identity == "" {
			return host, nil, fmt.Errorf("host sem chave associada")
		}
		return host, exec.Command("ssh-copy-id", "-i", identity+".pub", host.Alias), nil
	}
	options, err := resolveCompatOptions(host)
	if err != nil {
		return host, nil, err
	}

	host.SSHOptions = options
	identity := host.PrimaryIdentity()
	if identity == "" {
		return host, nil, fmt.Errorf("host sem chave associada")
	}
	pub := identity + ".pub"
	args := []string{"-i", pub}
	if host.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", host.Port))
	}
	for _, option := range options {
		args = append(args, "-o", option)
	}
	args = append(args, fmt.Sprintf("%s@%s", host.User, host.HostName))

	cmd := exec.Command("ssh-copy-id", args...)
	return host, cmd, nil
}

func Probe(host config.Host) (ProbeResult, error) {
	options, err := resolveCompatOptions(host)
	if err != nil {
		return ProbeResult{}, err
	}

	host.SSHOptions = options
	outcome, runErr := runSSH(host, options, true)
	if runErr == nil {
		return ProbeResult{Host: host, Message: "Conexao testada com sucesso."}, nil
	}

	switch {
	case isAuthReadyError(outcome.Stderr):
		return ProbeResult{Host: host, Message: "Host alcancavel e algoritmos validados. Falta apenas autenticacao."}, nil
	case strings.Contains(strings.ToLower(outcome.Stderr), "host key verification failed"):
		return ProbeResult{}, fmt.Errorf("host alcancavel, mas a verificacao da chave do servidor falhou")
	case strings.Contains(strings.ToLower(outcome.Stderr), "connection timed out"):
		return ProbeResult{}, fmt.Errorf("tempo limite excedido ao conectar")
	case strings.Contains(strings.ToLower(outcome.Stderr), "connection refused"):
		return ProbeResult{}, fmt.Errorf("conexao recusada na porta SSH")
	case strings.Contains(strings.ToLower(outcome.Stderr), "no route to host"):
		return ProbeResult{}, fmt.Errorf("sem rota ate o host informado")
	case strings.Contains(strings.ToLower(outcome.Stderr), "name or service not known"):
		return ProbeResult{}, fmt.Errorf("hostname invalido ou nao resolvido")
	case strings.Contains(strings.ToLower(outcome.Stderr), "could not resolve hostname"):
		return ProbeResult{}, fmt.Errorf("hostname invalido ou nao resolvido")
	default:
		return ProbeResult{}, runErr
	}
}

func sshArgs(host config.Host, options []string) []string {
	args := []string{}
	if host.Management == config.ManagementManual {
		args = append(args, "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3")
	}
	for _, identity := range host.IdentityFiles {
		args = append(args, "-i", identity)
	}
	if host.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", host.Port))
	}
	for _, option := range options {
		args = append(args, "-o", option)
	}
	args = append(args, fmt.Sprintf("%s@%s", host.User, host.HostName))
	return args
}

type sshRunOutcome struct {
	Stderr string
}

func runSSH(host config.Host, options []string, batchMode bool) (sshRunOutcome, error) {
	args := sshArgs(host, options)
	if batchMode {
		args = append([]string{
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=5",
			"-T",
		}, args...)
		args = append(args, "exit")
	}

	cmd := exec.Command("ssh", args...)
	if !batchMode {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
	}

	var stderr bytes.Buffer
	if batchMode {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	}

	err := cmd.Run()
	return sshRunOutcome{Stderr: stderr.String()}, err
}

func resolveCompatOptions(host config.Host) ([]string, error) {
	options := append([]string{}, host.SSHOptions...)

	for {
		outcome, err := runSSH(host, options, true)
		if err == nil || isAuthReadyError(outcome.Stderr) {
			return options, nil
		}

		retryOptions := fallbackOptions(outcome.Stderr, options)
		if len(retryOptions) == len(options) {
			return options, classifyCompatError(err, outcome.Stderr)
		}
		options = retryOptions
	}
}

func fallbackOptions(stderr string, current []string) []string {
	next := append([]string{}, current...)
	trimmed := strings.ToLower(strings.TrimSpace(stderr))

	switch {
	case strings.Contains(trimmed, "no matching host key type found") && strings.Contains(trimmed, "ssh-rsa"):
		next = appendOption(next, "HostKeyAlgorithms=+ssh-rsa")
	case strings.Contains(trimmed, "no matching key exchange method found") && strings.Contains(trimmed, "diffie-hellman-group14-sha1"):
		next = appendOption(next, "KexAlgorithms=+diffie-hellman-group14-sha1")
	case strings.Contains(trimmed, "no matching key exchange method found") && strings.Contains(trimmed, "diffie-hellman-group1-sha1"):
		next = appendOption(next, "KexAlgorithms=+diffie-hellman-group1-sha1")
	case strings.Contains(trimmed, "no matching signature algorithm") && strings.Contains(trimmed, "ssh-rsa"):
		next = appendOption(next, "PubkeyAcceptedAlgorithms=+ssh-rsa")
	case strings.Contains(trimmed, "no mutual signature supported") && strings.Contains(trimmed, "ssh-rsa"):
		next = appendOption(next, "PubkeyAcceptedAlgorithms=+ssh-rsa")
	case strings.Contains(trimmed, "send_pubkey_test: no mutual signature algorithm"):
		next = appendOption(next, "PubkeyAcceptedAlgorithms=+ssh-rsa")
	}

	return next
}

func classifyCompatError(err error, stderr string) error {
	text := strings.ToLower(strings.TrimSpace(stderr))
	switch {
	case strings.Contains(text, "connection timed out"):
		return fmt.Errorf("tempo limite excedido ao conectar")
	case strings.Contains(text, "connection refused"):
		return fmt.Errorf("conexao recusada na porta SSH")
	case strings.Contains(text, "no route to host"):
		return fmt.Errorf("sem rota ate o host informado")
	case strings.Contains(text, "could not resolve hostname"),
		strings.Contains(text, "name or service not known"),
		strings.Contains(text, "temporary failure in name resolution"):
		return fmt.Errorf("hostname invalido ou nao resolvido")
	case strings.Contains(text, "host key verification failed"):
		return fmt.Errorf("a chave do servidor nao confere com a conhecida localmente")
	case strings.Contains(text, "permission denied"):
		return fmt.Errorf("o host respondeu, mas a autenticacao falhou")
	case strings.Contains(text, "connection closed by remote host"):
		return fmt.Errorf("conexao encerrada pelo servidor remoto")
	case strings.Contains(text, "kex_exchange_identification"):
		return fmt.Errorf("falha na identificacao inicial do SSH")
	default:
		if text != "" {
			return errors.New(strings.TrimSpace(stderr))
		}
		return err
	}
}

func isAuthReadyError(stderr string) bool {
	text := strings.ToLower(stderr)
	return strings.Contains(text, "permission denied") ||
		strings.Contains(text, "publickey") ||
		strings.Contains(text, "keyboard-interactive") ||
		strings.Contains(text, "too many authentication failures")
}

func appendOption(options []string, option string) []string {
	if slices.Contains(options, option) {
		return options
	}
	return append(options, option)
}
