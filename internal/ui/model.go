package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"zenssh/internal/config"
	"zenssh/internal/sshcfg"
	"zenssh/internal/style"
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeBusy
	modeImport
	modeSearch
	modeDetails
	modeConfirmRestore
)

type operation int

const (
	opNone operation = iota
	opConnect
	opValidate
	opGenerateKey
	opPushKey
	opAuthCheck
)

type formAction int

const (
	formActionSaveExit formAction = iota
	formActionSaveConnect
	formActionSaveAddAnother
)

type actionResultMsg struct {
	op       operation
	err      error
	text     string
	keepForm bool
	form     formState
}

type execRequestMsg struct {
	op           operation
	host         config.Host
	cmd          *exec.Cmd
	keepForm     bool
	form         formState
	connectAfter bool
	successText  string
}

type execFinishedMsg struct {
	op           operation
	host         config.Host
	err          error
	keepForm     bool
	form         formState
	connectAfter bool
	successText  string
}

type authResultMsg struct {
	alias     string
	checkedAt time.Time
	err       error
}

type formState struct {
	title      string
	editing    bool
	original   string
	inputs     []textinput.Model
	sshOptions []string
	sendKey    bool
	cursor     int
	base       config.Host
}

type importState struct {
	candidates []sshcfg.Candidate
	keys       []string
	cursor     int
	firstRun   bool
}

type Model struct {
	store       *config.Store
	theme       style.Theme
	hosts       []config.Host
	cursor      int
	width       int
	height      int
	mode        mode
	form        formState
	status      string
	statusStyle lipgloss.Style
	spinner     spinner.Model
	pendingOp   operation
	importer    importState
	search      textinput.Model
	query       string
	details     string
	knownHosts  map[string]bool
}

func NewModel(store *config.Store) (Model, error) {
	hosts, err := store.LoadHosts()
	if err != nil {
		return Model{}, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))

	m := Model{
		store:       store,
		theme:       style.New(),
		hosts:       hosts,
		mode:        modeList,
		status:      "Pronto. Use a para adicionar um host e Enter para conectar.",
		statusStyle: style.New().Subtle,
		spinner:     s,
		knownHosts:  map[string]bool{},
	}
	if store.IsFirstRun() {
		discovery, discoverErr := sshcfg.Discover(store.ManagedSSHConfigPath())
		if discoverErr != nil {
			m.status = fmt.Sprintf("Falha na descoberta inicial: %v", discoverErr)
			m.statusStyle = m.theme.Danger
		} else {
			discovery.Candidates = sshcfg.ReconcileCandidates(discovery.Candidates, hosts)
			m.mode = modeImport
			m.importer = importState{candidates: discovery.Candidates, keys: discovery.Keys, firstRun: true}
			m.status = "Revise os hosts encontrados antes de importar."
		}
	} else if err := sshcfg.SaveAll(store, hosts); err != nil {
		return Model{}, err
	}
	m.refreshKnownHosts()
	return m, nil
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case spinner.TickMsg:
		if m.mode == modeBusy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case actionResultMsg:
		if msg.keepForm {
			m.mode = modeForm
			m.form = msg.form
		} else {
			m.mode = modeList
		}
		m.pendingOp = opNone
		if msg.err != nil {
			m.status = fmt.Sprintf("Falha: %v", msg.err)
			m.statusStyle = m.theme.Danger
		} else {
			m.status = msg.text
			m.statusStyle = m.theme.Success
		}
		if hosts, err := m.store.LoadHosts(); err == nil {
			m.hosts = hosts
			m.syncCursor()
			m.refreshKnownHosts()
		}
		return m, nil
	case authResultMsg:
		m.pendingOp = opNone
		m.mode = modeList
		if err := persistAuthResult(m.store, msg); err != nil {
			m.status = fmt.Sprintf("Falha ao salvar validacao: %v", err)
			m.statusStyle = m.theme.Danger
		} else if msg.err != nil {
			m.status = "Autenticacao por chave nao validada: " + compactError(msg.err.Error())
			m.statusStyle = m.theme.Danger
		} else {
			m.status = "Autenticacao por chave validada com sucesso."
			m.statusStyle = m.theme.Success
		}
		m.hosts, _ = m.store.LoadHosts()
		m.refreshKnownHosts()
		return m, nil
	case execRequestMsg:
		return m, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			return execFinishedMsg{
				op:           msg.op,
				host:         msg.host,
				err:          err,
				keepForm:     msg.keepForm,
				form:         msg.form,
				connectAfter: msg.connectAfter,
				successText:  msg.successText,
			}
		})
	case execFinishedMsg:
		if msg.err != nil {
			m.pendingOp = opNone
			if msg.keepForm {
				m.mode = modeForm
				m.form = msg.form
			} else {
				m.mode = modeList
			}
			m.status = fmt.Sprintf("Falha: %v", msg.err)
			m.statusStyle = m.theme.Danger
			if hosts, err := m.store.LoadHosts(); err == nil {
				m.hosts = hosts
				m.syncCursor()
			}
			return m, nil
		}

		switch msg.op {
		case opPushKey:
			if err := markHostKeySent(m.store, msg.host.Alias); err != nil {
				m.pendingOp = opNone
				m.mode = modeList
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
				return m, nil
			}
			if msg.connectAfter {
				updatedHost, cmd, err := sshcfg.PrepareConnect(msg.host)
				if err != nil {
					m.pendingOp = opNone
					m.mode = modeList
					m.status = fmt.Sprintf("Falha: %v", err)
					m.statusStyle = m.theme.Danger
					return m, nil
				}
				m.pendingOp = opConnect
				return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
					return execFinishedMsg{op: opConnect, host: updatedHost, err: err}
				})
			}

			m.pendingOp = opNone
			if msg.keepForm {
				m.mode = modeForm
				m.form = msg.form
			} else {
				m.mode = modeList
			}
			m.status = msg.successText
			m.statusStyle = m.theme.Success
		case opConnect:
			m.pendingOp = opNone
			m.mode = modeList
			if err := persistHostOptions(m.store, msg.host); err != nil {
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
			} else {
				m.status = fmt.Sprintf("Sessao SSH encerrada para %s.", msg.host.Alias)
				m.statusStyle = m.theme.Success
			}
		default:
			m.pendingOp = opNone
			m.mode = modeList
			m.status = msg.successText
			m.statusStyle = m.theme.Success
		}

		if hosts, err := m.store.LoadHosts(); err == nil {
			m.hosts = hosts
			m.syncCursor()
			m.refreshKnownHosts()
		}
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeForm:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateDelete(msg)
		case modeBusy:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		case modeImport:
			return m.updateImport(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeDetails:
			if msg.String() == "esc" || msg.String() == "q" {
				m.mode = modeList
			}
			return m, nil
		case modeConfirmRestore:
			return m.updateRestore(msg)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visibleHosts())-1 {
			m.cursor++
		}
	case "/":
		m.search = textinput.New()
		m.search.Placeholder = "alias, host, grupo ou origem"
		m.search.SetValue(m.query)
		m.search.Focus()
		m.mode = modeSearch
		return m, textinput.Blink
	case "v":
		if host, ok := m.currentHost(); ok {
			m.details = diagnosticSummary(host)
			m.mode = modeDetails
		}
	case "m":
		if host, ok := m.currentHost(); ok && host.Source != "" {
			if host.Management == config.ManagementReadOnly {
				host.Management = config.ManagementManaged
			} else {
				host.Management = config.ManagementReadOnly
			}
			if err := upsertHost(m.store, host, host.Alias); err != nil {
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
			} else {
				m.hosts, _ = m.store.LoadHosts()
				m.status = "Modo de gerenciamento atualizado."
				m.statusStyle = m.theme.Success
			}
		}
	case "b":
		m.mode = modeConfirmRestore
	case "a":
		m.form = newForm(config.Host{})
		m.mode = modeForm
	case "i":
		discovery, err := sshcfg.Discover(m.store.ManagedSSHConfigPath())
		if err != nil {
			m.status = fmt.Sprintf("Falha ao descobrir hosts: %v", err)
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		discovery.Candidates = sshcfg.ReconcileCandidates(discovery.Candidates, m.hosts)
		m.importer = importState{candidates: discovery.Candidates, keys: discovery.Keys}
		m.mode = modeImport
	case "e":
		if host, ok := m.currentHost(); ok {
			m.form = newEditForm(host)
			m.mode = modeForm
		}
	case "d":
		if _, ok := m.currentHost(); ok {
			m.mode = modeConfirmDelete
		}
	case "g":
		if host, ok := m.currentHost(); ok {
			m.mode = modeBusy
			m.pendingOp = opGenerateKey
			return m, tea.Batch(m.spinner.Tick, runGenerateKey(host))
		}
	case "s":
		if host, ok := m.currentHost(); ok {
			m.mode = modeBusy
			m.pendingOp = opPushKey
			return m, tea.Batch(m.spinner.Tick, runPushKey(m.store, host))
		}
	case "t":
		if host, ok := m.currentHost(); ok {
			m.mode = modeBusy
			m.pendingOp = opAuthCheck
			return m, tea.Batch(m.spinner.Tick, runAuthCheck(host))
		}
	case "enter":
		if host, ok := m.currentHost(); ok {
			updatedHost, cmd, err := sshcfg.PrepareConnect(host)
			if err != nil {
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
				return m, nil
			}
			m.mode = modeBusy
			m.pendingOp = opConnect
			return m, tea.Batch(
				m.spinner.Tick,
				tea.ExecProcess(cmd, func(err error) tea.Msg {
					return execFinishedMsg{op: opConnect, host: updatedHost, err: err}
				}),
			)
		}
	}
	return m, nil
}

func (m Model) updateImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := len(m.importer.candidates)
	switch msg.String() {
	case "up", "k":
		if m.importer.cursor > 0 {
			m.importer.cursor--
		}
	case "down", "j":
		if m.importer.cursor < items-1 {
			m.importer.cursor++
		}
	case " ":
		if items > 0 && m.importer.candidates[m.importer.cursor].Status != "falha ao resolver" {
			m.importer.candidates[m.importer.cursor].Selected = !m.importer.candidates[m.importer.cursor].Selected
		}
	case "a":
		for i := range m.importer.candidates {
			m.importer.candidates[i].Selected = m.importer.candidates[i].Status != "falha ao resolver"
		}
	case "n":
		for i := range m.importer.candidates {
			m.importer.candidates[i].Selected = false
		}
	case "c":
		if items > 0 && len(m.importer.keys) > 0 {
			candidate := &m.importer.candidates[m.importer.cursor]
			next := 0
			for i, key := range m.importer.keys {
				if key == candidate.Host.PrimaryIdentity() {
					next = (i + 1) % len(m.importer.keys)
					break
				}
			}
			key := m.importer.keys[next]
			candidate.Host.IdentityFiles = []string{key}
			candidate.Host.SSHOptions = slices.DeleteFunc(candidate.Host.SSHOptions, func(option string) bool {
				return strings.HasPrefix(strings.ToLower(option), "certificatefile=")
			})
			cert := key + "-cert.pub"
			if _, err := os.Stat(cert); err == nil && !slices.Contains(candidate.Host.SSHOptions, "CertificateFile="+cert) {
				candidate.Host.SSHOptions = append(candidate.Host.SSHOptions, "CertificateFile="+cert)
			}
		}
	case "o":
		if items > 0 {
			candidate := &m.importer.candidates[m.importer.cursor]
			if candidate.Host.Management == config.ManagementReadOnly {
				candidate.Host.Management = config.ManagementManaged
			} else {
				candidate.Host.Management = config.ManagementReadOnly
			}
		}
	case "enter":
		result, err := sshcfg.ImportCandidates(m.store, m.importer.candidates)
		if err != nil {
			m.status = fmt.Sprintf("Falha ao importar: %v", err)
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		if err := m.store.MarkOnboardingComplete(); err != nil {
			m.status = fmt.Sprintf("Falha ao finalizar onboarding: %v", err)
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		m.hosts, _ = m.store.LoadHosts()
		m.syncCursor()
		m.refreshKnownHosts()
		m.mode = modeList
		m.status = fmt.Sprintf("Sincronizacao: %d novos, %d atualizados, %d removidos, %d mantidos.", result.Imported, result.Updated, result.Removed, result.Skipped)
		m.statusStyle = m.theme.Success
	case "esc", "q":
		m.mode = modeList
		m.status = "Importacao cancelada; nenhuma configuracao foi alterada."
		m.statusStyle = m.theme.Subtle
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.query = strings.TrimSpace(m.search.Value())
		m.cursor = 0
		m.mode = modeList
		return m, nil
	case "esc":
		m.mode = modeList
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m Model) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		err := sshcfg.RestoreMainConfigBackup()
		m.mode = modeList
		if err != nil {
			m.status = fmt.Sprintf("Falha ao restaurar backup: %v", err)
			m.statusStyle = m.theme.Danger
		} else {
			m.status = "Backup restaurado em ~/.ssh/config."
			m.statusStyle = m.theme.Success
		}
	case "n", "esc":
		m.mode = modeList
	}
	return m, nil
}

func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.status = "Cadastro cancelado."
		m.statusStyle = m.theme.Subtle
		return m, nil
	case "tab", "shift+tab", "up", "down":
		delta := 1
		if msg.String() == "shift+tab" || msg.String() == "up" {
			delta = -1
		}
		if m.form.cursor < len(m.form.inputs) {
			m.form.inputs[m.form.cursor].Blur()
		}
		totalItems := len(m.form.inputs) + 4
		m.form.cursor = (m.form.cursor + delta + totalItems) % totalItems
		if m.form.cursor < len(m.form.inputs) {
			m.form.inputs[m.form.cursor].Focus()
		}
		return m, nil
	case " ":
		if m.form.cursor == len(m.form.inputs) {
			m.form.sendKey = !m.form.sendKey
			return m, nil
		}
	case "enter":
		if m.form.cursor < len(m.form.inputs)-1 {
			m.form.inputs[m.form.cursor].Blur()
			m.form.cursor++
			m.form.inputs[m.form.cursor].Focus()
			return m, nil
		}
		if m.form.cursor < len(m.form.inputs) {
			m.form.inputs[m.form.cursor].Blur()
			m.form.cursor++
			return m, nil
		}

		host, err := m.form.host()
		if err != nil {
			m.status = err.Error()
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		switch selectedFormAction(m.form) {
		case formActionSaveConnect:
			m.mode = modeBusy
			m.pendingOp = opConnect
			return m, tea.Batch(m.spinner.Tick, runSaveAndConnect(m.store, host, m.form.original, m.form.sendKey))
		case formActionSaveAddAnother:
			if err := m.saveHost(host); err != nil {
				m.status = err.Error()
				m.statusStyle = m.theme.Danger
				return m, nil
			}
			if m.form.sendKey {
				m.mode = modeBusy
				m.pendingOp = opPushKey
				return m, tea.Batch(m.spinner.Tick, runPushKeyAndAddAnother(m.store, host))
			}

			m.form = newForm(config.Host{})
			m.mode = modeForm
			m.status = fmt.Sprintf("Host %s salvo. Preencha o proximo cadastro.", host.Alias)
			m.statusStyle = m.theme.Success
			return m, nil
		default:
			if err := m.saveHost(host); err != nil {
				m.status = err.Error()
				m.statusStyle = m.theme.Danger
				return m, nil
			}

			if m.form.sendKey {
				m.mode = modeBusy
				m.pendingOp = opPushKey
				return m, tea.Batch(m.spinner.Tick, runPushKey(m.store, host))
			}

			m.mode = modeList
			m.status = fmt.Sprintf("Host %s salvo com sucesso.", host.Alias)
			m.statusStyle = m.theme.Success
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.form.inputs[m.form.cursor], cmd = m.form.inputs[m.form.cursor].Update(msg)
	return m, cmd
}

func (m Model) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		host, ok := m.currentHost()
		if !ok {
			m.mode = modeList
			return m, nil
		}
		if err := m.deleteHost(host.Alias); err != nil {
			m.status = err.Error()
			m.statusStyle = m.theme.Danger
		} else {
			m.status = fmt.Sprintf("Host %s removido.", host.Alias)
			m.statusStyle = m.theme.Success
		}
		m.mode = modeList
		return m, nil
	case "n", "esc":
		m.mode = modeList
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	header := m.renderHeader()
	body := ""

	switch m.mode {
	case modeList:
		body = m.renderList()
	case modeForm:
		body = m.renderForm()
	case modeConfirmDelete:
		body = m.renderDelete()
	case modeBusy:
		body = m.renderBusy()
	case modeImport:
		body = m.renderImport()
	case modeSearch:
		body = m.theme.Panel.Render("Buscar hosts\n\n" + m.search.View() + "\n\nEnter aplica · Esc cancela")
	case modeDetails:
		body = m.theme.Panel.Render(m.details + "\n\nEsc volta")
	case modeConfirmRestore:
		body = m.theme.Panel.Render("Restaurar ~/.ssh/config.zenssh.bak?\n\nIsso substitui o arquivo SSH atual.\n\ny confirma · n cancela")
	}

	footer := m.renderFooter()
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return m.theme.App.Render(content)
}

func (m Model) renderImport() string {
	rows := []string{m.theme.Accent.Render("Importar configuracao existente"), ""}
	if len(m.importer.candidates) == 0 {
		rows = append(rows, "Nenhum alias SSH ou candidato em /etc/hosts foi encontrado.")
	}
	for i, candidate := range m.importer.candidates {
		mark := "[ ]"
		if candidate.Selected {
			mark = "[x]"
		}
		pointer := "  "
		if i == m.importer.cursor {
			pointer = "▶ "
		}
		kind := "SSH"
		if candidate.Optional {
			kind = "/etc/hosts (opcional)"
		}
		rows = append(rows, fmt.Sprintf("%s%s %-22s %s@%s:%d  %s · %s · %s · chave:%s", pointer, mark, candidate.Host.Alias, candidate.Host.User, candidate.Host.HostName, candidate.Host.Port, kind, candidate.Status, candidate.Host.Management, filepath.Base(candidate.Host.PrimaryIdentity())))
	}
	rows = append(rows, "", fmt.Sprintf("Chaves privadas encontradas: %d (somente referenciadas, nunca copiadas)", len(m.importer.keys)), "", renderShortcuts(m.theme,
		shortcut{key: "Espaco", label: "marcar"}, shortcut{key: "a", label: "todos"}, shortcut{key: "n", label: "nenhum"}, shortcut{key: "c", label: "alternar chave"}, shortcut{key: "o", label: "gerenciado/leitura"}, shortcut{key: "Enter", label: "importar"}, shortcut{key: "Esc", label: "cancelar"},
	))
	return m.theme.Panel.Render(strings.Join(rows, "\n"))
}

func (m Model) renderHeader() string {
	title := m.theme.Header.Render("ZenSSH")
	subtitle := m.theme.Subtle.Render("Gerencie aliases SSH com uma TUI elegante, direta e funcional.")
	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle)
}

func (m Model) renderList() string {
	welcome := m.renderWelcome()
	hosts := m.visibleHosts()

	if len(hosts) == 0 {
		panel := m.theme.Panel.Render("Nenhum host cadastrado.\n\nPressione a para iniciar o cadastro guiado.")
		return lipgloss.JoinVertical(lipgloss.Left, welcome, panel)
	}

	lines := []string{}
	for i, host := range hosts {
		pointer := "  "
		line := fmt.Sprintf("%s  %s  %s  %s  chave:%s  servidor:%s  [%s/%s]", badge(host.Group), host.Alias, host.Address(), portLabel(host.Port), keyStatusLabel(host), knownStatusLabel(m.knownHosts[strings.ToLower(host.Alias)]), sourceLabel(host), host.Management)
		if i == m.cursor {
			pointer = "▶ "
			line = m.theme.Selected.Render(line)
		}
		lines = append(lines, pointer+line)
	}

	commands := renderShortcuts(
		m.theme,
		shortcut{key: "Enter", label: "conectar"},
		shortcut{key: "a", label: "adicionar"},
		shortcut{key: "i", label: "importar config"},
		shortcut{key: "/", label: "buscar"},
		shortcut{key: "v", label: "diagnostico"},
		shortcut{key: "m", label: "modo"},
		shortcut{key: "b", label: "restaurar"},
		shortcut{key: "e", label: "editar"},
		shortcut{key: "d", label: "remover"},
		shortcut{key: "g", label: "gerar chave"},
		shortcut{key: "s", label: "enviar chave"},
		shortcut{key: "t", label: "validar chave"},
		shortcut{key: "q", label: "sair"},
	)
	panel := m.theme.Panel.Render(strings.Join(lines, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, welcome, panel, commands)
}

func (m Model) renderForm() string {
	rows := []string{m.theme.Accent.Render(m.form.title), ""}
	labels := []string{"Alias", "Host/IP", "Porta", "Usuario", "Grupo", "Arquivos das chaves (separados por virgula)"}

	for i, input := range m.form.inputs {
		prefix := "  "
		if i == m.form.cursor {
			prefix = "▶ "
		}
		rows = append(rows, fmt.Sprintf("%s%s\n%s", prefix, m.theme.InputLabel.Render(labels[i]), input.View()))
		rows = append(rows, "")
	}

	sendLine := "Nao"
	if m.form.sendKey {
		sendLine = "Sim"
	}
	rows = append(rows, fmt.Sprintf("%sEnviar chave agora\n[%s]", pointerForCursor(m.form.cursor == len(m.form.inputs)), sendLine))
	rows = append(rows, "")
	rows = append(rows, fmt.Sprintf("%sSalvar e sair", pointerForCursor(m.form.cursor == len(m.form.inputs)+1)))
	rows = append(rows, "")
	rows = append(rows, fmt.Sprintf("%sSalvar, testar e conectar", pointerForCursor(m.form.cursor == len(m.form.inputs)+2)))
	rows = append(rows, "")
	rows = append(rows, fmt.Sprintf("%sSalvar e adicionar outro", pointerForCursor(m.form.cursor == len(m.form.inputs)+3)))
	rows = append(rows, "")
	rows = append(rows, renderShortcuts(
		m.theme,
		shortcut{key: "Tab", label: "navega"},
		shortcut{key: "Espaco", label: "alterna envio da chave"},
		shortcut{key: "Enter", label: "executa acao"},
		shortcut{key: "Esc", label: "volta"},
	))

	return m.theme.Panel.Render(strings.Join(rows, "\n"))
}

func (m Model) renderDelete() string {
	host, _ := m.currentHost()
	rows := []string{
		fmt.Sprintf("Remover o host %s?", host.Alias),
		"",
		"Essa ação atualiza o arquivo gerenciado do SSH.",
		"",
		renderShortcuts(
			m.theme,
			shortcut{key: "y", label: "confirmar"},
			shortcut{key: "n", label: "cancelar"},
		),
	}
	return m.theme.Panel.Render(strings.Join(rows, "\n"))
}

func (m Model) renderBusy() string {
	host, _ := m.currentHost()
	label := "Processando"
	detail := "O terminal volta para a TUI quando a ação terminar."
	switch m.pendingOp {
	case opConnect:
		label = fmt.Sprintf("Abrindo sessão SSH para %s", host.Alias)
		detail = "O terminal sera entregue ao ssh para prompts interativos, incluindo senha e confirmacao de chave do host."
	case opValidate:
		label = "Validando host SSH"
	case opGenerateKey:
		label = fmt.Sprintf("Gerando chave para %s", host.Alias)
	case opPushKey:
		label = fmt.Sprintf("Enviando chave para %s", host.Alias)
	case opAuthCheck:
		label = fmt.Sprintf("Validando autenticacao por chave para %s", host.Alias)
		detail = "O teste usa BatchMode, desativa senha e nao aceita chaves de servidor desconhecidas."
	}
	return m.theme.Panel.Render(fmt.Sprintf("%s %s\n\n%s", m.spinner.View(), label, detail))
}

func (m Model) renderFooter() string {
	return m.statusStyle.Render(m.status)
}

func (m Model) renderWelcome() string {
	art := m.theme.Accent.Render(strings.Join([]string{
		"-----------------+##*#####+##=-----------------",
		"---------------*##############*=---------------",
		"--------------####*----------=##---------------",
		"------------*###*--------------=##+------------",
		"-----------####=----------------=#+------------",
		"-----------####------------------=#+-----------",
		"-----------####-------------------#+-----------",
		"-----------####------------------#++-----------",
		"-----------#####-----------------*##-----------",
		"------------#####=#-------------#*#------------",
		"-------------###########------**##-------------",
		"---------------###########----#+---------------",
		"-----------------########=--+------------------",
	}, "\n"))

	message := lipgloss.JoinVertical(
		lipgloss.Left,
		m.theme.Highlight.Render("Bem-vindo ao ZenSSH"),
		m.theme.Subtle.Render("Respire, escolha um host e conecte com calma."),
		m.theme.Subtle.Render("Gerencie aliases SSH com uma interface direta e organizada."),
	)

	return m.theme.Panel.Render(lipgloss.JoinVertical(lipgloss.Left, art, "", message))
}

type shortcut struct {
	key   string
	label string
}

func renderShortcuts(theme style.Theme, items ...shortcut) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, lipgloss.JoinHorizontal(
			lipgloss.Center,
			theme.Key.Render(item.key),
			theme.Help.Render(" "+item.label),
		))
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, appendInterleaved(parts, theme.Subtle.Render("  •  "))...)
}

func appendInterleaved(items []string, separator string) []string {
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			result = append(result, separator)
		}
		result = append(result, item)
	}

	return result
}

func (m *Model) currentHost() (config.Host, bool) {
	hosts := m.visibleHosts()
	if len(hosts) == 0 || m.cursor < 0 || m.cursor >= len(hosts) {
		return config.Host{}, false
	}
	return hosts[m.cursor], true
}

func (m Model) visibleHosts() []config.Host {
	query := strings.ToLower(strings.TrimSpace(m.query))
	if query == "" {
		return m.hosts
	}
	result := make([]config.Host, 0, len(m.hosts))
	for _, host := range m.hosts {
		haystack := strings.ToLower(strings.Join([]string{host.Alias, host.HostName, host.User, host.Group, host.Source, host.SourcePath}, " "))
		if strings.Contains(haystack, query) {
			result = append(result, host)
		}
	}
	return result
}

func sourceLabel(host config.Host) string {
	if host.Source == "" {
		return "manual"
	}
	return host.Source
}

func diagnosticSummary(host config.Host) string {
	identities := "agente/padrao do OpenSSH"
	if len(host.IdentityFiles) > 0 {
		identities = strings.Join(host.IdentityFiles, ", ")
	}
	optionNames := make([]string, 0, len(host.SSHOptions))
	for _, option := range host.SSHOptions {
		name := strings.FieldsFunc(option, func(r rune) bool { return r == '=' || r == ' ' })
		if len(name) > 0 {
			optionNames = append(optionNames, name[0])
		}
	}
	lastCheck := "nunca"
	if !host.KeyAuthCheckedAt.IsZero() {
		lastCheck = host.KeyAuthCheckedAt.Format("2006-01-02 15:04:05")
	}
	return fmt.Sprintf("Diagnostico de %s\n\nDestino: %s@%s:%d\nOrigem: %s\nArquivo: %s\nModo: %s\nIdentidades: %s\nAutenticacao por chave: %s\nUltimo teste: %s\nOpcoes efetivas: %s\n\nComando equivalente:\nssh -p %d %s@%s", host.Alias, host.User, host.HostName, host.Port, sourceLabel(host), host.SourcePath, host.Management, identities, keyStatusLabel(host), lastCheck, strings.Join(optionNames, ", "), host.Port, host.User, host.HostName)
}

func (m *Model) refreshKnownHosts() {
	if m.knownHosts == nil {
		m.knownHosts = map[string]bool{}
	}
	for _, host := range m.hosts {
		m.knownHosts[strings.ToLower(host.Alias)] = sshcfg.IsHostKnown(host)
	}
}

func keyStatusLabel(host config.Host) string {
	if host.KeyAuthStatus == config.KeyAuthValidated {
		return "validada"
	}
	if len(host.IdentityFiles) == 0 && host.Management != config.ManagementReadOnly {
		return "sem-chave"
	}
	if host.KeySent {
		return "envio-registrado"
	}
	if host.KeyAuthStatus == config.KeyAuthFailed {
		return "falhou"
	}
	return "configurada"
}

func knownStatusLabel(known bool) string {
	if known {
		return "conhecido"
	}
	return "desconhecido"
}

func (m *Model) syncCursor() {
	hosts := m.visibleHosts()
	if len(hosts) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(hosts) {
		m.cursor = len(hosts) - 1
	}
}

func (m *Model) saveHost(host config.Host) error {
	if err := upsertHost(m.store, host, m.form.original); err != nil {
		return err
	}
	hosts, err := m.store.LoadHosts()
	if err != nil {
		return err
	}
	m.hosts = hosts
	if m.query == "" {
		m.cursor = indexOfAlias(hosts, host.Alias)
	} else {
		m.cursor = 0
	}
	return nil
}

func (m *Model) deleteHost(alias string) error {
	hosts, err := m.store.LoadHosts()
	if err != nil {
		return err
	}
	filtered := hosts[:0]
	for _, host := range hosts {
		if host.Alias != alias {
			filtered = append(filtered, host)
		}
	}
	hosts = filtered
	if err := sshcfg.SaveAll(m.store, hosts); err != nil {
		return err
	}
	m.hosts = hosts
	m.syncCursor()
	return nil
}

func newForm(host config.Host) formState {
	inputs := make([]textinput.Model, 6)
	placeholders := []string{
		"app-prod",
		"10.0.0.25 ou server.exemplo.com",
		"22",
		"ubuntu",
		"producao",
		config.DefaultIdentityFile(),
	}
	values := []string{
		host.Alias,
		host.HostName,
		func() string {
			if host.Port == 0 {
				return "22"
			}
			return strconv.Itoa(host.Port)
		}(),
		host.User,
		host.Group,
		strings.Join(host.IdentityFiles, ", "),
	}

	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Placeholder = placeholders[i]
		inputs[i].SetValue(values[i])
		inputs[i].Width = 42
	}
	inputs[0].Focus()

	return formState{
		title:      "Novo host",
		inputs:     inputs,
		sshOptions: append([]string{}, host.SSHOptions...),
		sendKey:    false,
		cursor:     0,
		base:       host,
	}
}

func newEditForm(host config.Host) formState {
	form := newForm(host)
	form.title = "Editar host"
	form.editing = true
	form.original = host.Alias
	return form
}

func (f formState) host() (config.Host, error) {
	alias := strings.TrimSpace(f.inputs[0].Value())
	hostName := strings.TrimSpace(f.inputs[1].Value())
	portValue := strings.TrimSpace(f.inputs[2].Value())
	user := strings.TrimSpace(f.inputs[3].Value())
	group := strings.TrimSpace(f.inputs[4].Value())
	identityValue := strings.TrimSpace(f.inputs[5].Value())

	port, err := strconv.Atoi(portValue)
	if err != nil {
		return config.Host{}, fmt.Errorf("porta inválida")
	}
	if identityValue == "" {
		identityValue = config.DefaultIdentityFile()
	}
	var identities []string
	for _, identity := range strings.Split(identityValue, ",") {
		identity = strings.TrimSpace(identity)
		if strings.HasPrefix(identity, "~/") {
			home, _ := os.UserHomeDir()
			identity = filepath.Join(home, strings.TrimPrefix(identity, "~/"))
		}
		if identity != "" {
			identities = append(identities, identity)
		}
	}

	host := f.base
	host.Alias = alias
	host.HostName = hostName
	host.Port = port
	host.User = user
	host.Group = group
	host.IdentityFiles = identities
	host.IdentityFile = ""
	host.SSHOptions = append([]string{}, f.sshOptions...)
	if host.Management == "" {
		host.Management = config.ManagementManual
	}
	if err := config.ValidateHost(host); err != nil {
		return config.Host{}, err
	}
	return host, nil
}

func runGenerateKey(host config.Host) tea.Cmd {
	return func() tea.Msg {
		identity := host.PrimaryIdentity()
		if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
			return actionResultMsg{op: opGenerateKey, err: err}
		}
		err := sshcfg.GenerateKey(identity)
		text := fmt.Sprintf("Chave pronta em %s.", identity)
		return actionResultMsg{op: opGenerateKey, err: err, text: text}
	}
}

func runPushKey(_ *config.Store, host config.Host) tea.Cmd {
	return func() tea.Msg {
		identity := host.PrimaryIdentity()
		if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
			return actionResultMsg{op: opPushKey, err: err}
		}
		if err := sshcfg.GenerateKey(identity); err != nil {
			return actionResultMsg{op: opPushKey, err: err}
		}
		updatedHost, cmd, err := sshcfg.PreparePushKey(host)
		if err != nil {
			return actionResultMsg{op: opPushKey, err: err}
		}
		return execRequestMsg{
			op:          opPushKey,
			host:        updatedHost,
			cmd:         cmd,
			successText: fmt.Sprintf("Chave enviada para %s.", host.Alias),
		}
	}
}

func runValidateHost(form formState, host config.Host) tea.Cmd {
	return func() tea.Msg {
		result, err := sshcfg.Probe(host)
		if err != nil {
			return actionResultMsg{op: opValidate, err: err, keepForm: true, form: form}
		}
		form.sshOptions = append([]string{}, result.Host.SSHOptions...)
		return actionResultMsg{op: opValidate, text: result.Message, keepForm: true, form: form}
	}
}

func runAuthCheck(host config.Host) tea.Cmd {
	return func() tea.Msg {
		return authResultMsg{alias: host.Alias, checkedAt: time.Now(), err: sshcfg.ValidateKeyAuthentication(host)}
	}
}

func runSaveAndConnect(store *config.Store, host config.Host, originalAlias string, sendKey bool) tea.Cmd {
	return func() tea.Msg {
		if err := upsertHost(store, host, originalAlias); err != nil {
			return actionResultMsg{op: opConnect, err: err}
		}

		if sendKey {
			identity := host.PrimaryIdentity()
			if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
				return actionResultMsg{op: opPushKey, err: err}
			}
			if err := sshcfg.GenerateKey(identity); err != nil {
				return actionResultMsg{op: opPushKey, err: err}
			}
			updatedHost, cmd, err := sshcfg.PreparePushKey(host)
			if err != nil {
				return actionResultMsg{op: opPushKey, err: err}
			}
			return execRequestMsg{op: opPushKey, host: updatedHost, cmd: cmd, connectAfter: true}
		}

		updatedHost, cmd, err := sshcfg.PrepareConnect(host)
		if err != nil {
			return actionResultMsg{op: opConnect, err: err}
		}
		return execRequestMsg{op: opConnect, host: updatedHost, cmd: cmd}
	}
}

func runPushKeyAndAddAnother(_ *config.Store, host config.Host) tea.Cmd {
	return func() tea.Msg {
		identity := host.PrimaryIdentity()
		if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
			return actionResultMsg{op: opPushKey, err: err, keepForm: true, form: newForm(config.Host{})}
		}
		if err := sshcfg.GenerateKey(identity); err != nil {
			return actionResultMsg{op: opPushKey, err: err, keepForm: true, form: newForm(config.Host{})}
		}
		updatedHost, cmd, err := sshcfg.PreparePushKey(host)
		if err != nil {
			return actionResultMsg{op: opPushKey, err: err, keepForm: true, form: newForm(config.Host{})}
		}
		return execRequestMsg{
			op:          opPushKey,
			host:        updatedHost,
			cmd:         cmd,
			keepForm:    true,
			form:        newForm(config.Host{}),
			successText: fmt.Sprintf("Host %s salvo e chave enviada. Preencha o proximo cadastro.", host.Alias),
		}
	}
}

func persistHostOptions(store *config.Store, host config.Host) error {
	hosts, err := store.LoadHosts()
	if err != nil {
		return err
	}

	changed := false
	for i := range hosts {
		if hosts[i].Alias != host.Alias {
			continue
		}
		if strings.Join(hosts[i].SSHOptions, "\x00") == strings.Join(host.SSHOptions, "\x00") {
			return nil
		}
		hosts[i].SSHOptions = append([]string{}, host.SSHOptions...)
		hosts[i].UpdatedAt = time.Now()
		changed = true
		break
	}
	if !changed {
		return nil
	}
	return sshcfg.SaveAll(store, hosts)
}

func upsertHost(store *config.Store, host config.Host, originalAlias string) error {
	hosts, err := store.LoadHosts()
	if err != nil {
		return err
	}

	replaced := false
	now := time.Now()
	for i := range hosts {
		if hosts[i].Alias == host.Alias || (originalAlias != "" && hosts[i].Alias == originalAlias) {
			if config.HostFingerprint(hosts[i]) == config.HostFingerprint(host) {
				host.KeyAuthStatus = hosts[i].KeyAuthStatus
				host.KeyAuthCheckedAt = hosts[i].KeyAuthCheckedAt
				host.KeyAuthError = hosts[i].KeyAuthError
			} else {
				host.KeyAuthStatus = config.KeyAuthUnknown
				host.KeyAuthCheckedAt = time.Time{}
				host.KeyAuthError = ""
			}
			host.CreatedAt = hosts[i].CreatedAt
			host.UpdatedAt = now
			host.KeySent = hosts[i].KeySent
			hosts[i] = host
			replaced = true
			break
		}
	}
	if !replaced {
		host.CreatedAt = now
		host.UpdatedAt = now
		hosts = append(hosts, host)
	}
	return sshcfg.SaveAll(store, hosts)
}

func markHostKeySent(store *config.Store, alias string) error {
	hosts, err := store.LoadHosts()
	if err != nil {
		return err
	}
	for i := range hosts {
		if hosts[i].Alias == alias {
			hosts[i].KeySent = true
			hosts[i].KeyAuthStatus = config.KeyAuthUnknown
			hosts[i].KeyAuthCheckedAt = time.Time{}
			hosts[i].KeyAuthError = ""
			hosts[i].UpdatedAt = time.Now()
		}
	}
	return sshcfg.SaveAll(store, hosts)
}

func persistAuthResult(store *config.Store, result authResultMsg) error {
	hosts, err := store.LoadHosts()
	if err != nil {
		return err
	}
	for i := range hosts {
		if !strings.EqualFold(hosts[i].Alias, result.alias) {
			continue
		}
		hosts[i].KeyAuthCheckedAt = result.checkedAt
		if result.err == nil {
			hosts[i].KeyAuthStatus = config.KeyAuthValidated
			hosts[i].KeyAuthError = ""
		} else {
			hosts[i].KeyAuthStatus = config.KeyAuthFailed
			hosts[i].KeyAuthError = compactError(result.err.Error())
		}
		break
	}
	return sshcfg.SaveAll(store, hosts)
}

func compactError(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	const limit = 180
	if len(message) > limit {
		return message[:limit] + "..."
	}
	return message
}

func indexOfAlias(hosts []config.Host, alias string) int {
	for i, host := range hosts {
		if host.Alias == alias {
			return i
		}
	}
	return 0
}

func pointerForCursor(selected bool) string {
	if selected {
		return "▶ "
	}
	return "  "
}

func badge(group string) string {
	if group == "" {
		return "•"
	}
	return "◈ " + group
}

func portLabel(port int) string {
	if port == 0 {
		return ":22"
	}
	return ":" + strconv.Itoa(port)
}

func selectedFormAction(form formState) formAction {
	switch form.cursor {
	case len(form.inputs) + 2:
		return formActionSaveConnect
	case len(form.inputs) + 3:
		return formActionSaveAddAnother
	default:
		return formActionSaveExit
	}
}
