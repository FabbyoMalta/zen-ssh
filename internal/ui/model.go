package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
)

type operation int

const (
	opNone operation = iota
	opConnect
	opValidate
	opGenerateKey
	opPushKey
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

type formState struct {
	title      string
	editing    bool
	original   string
	inputs     []textinput.Model
	sshOptions []string
	sendKey    bool
	cursor     int
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
}

func NewModel(store *config.Store) (Model, error) {
	hosts, err := store.LoadHosts()
	if err != nil {
		return Model{}, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))

	return Model{
		store:       store,
		theme:       style.New(),
		hosts:       hosts,
		mode:        modeList,
		status:      "Pronto. Use a para adicionar um host e Enter para conectar.",
		statusStyle: style.New().Subtle,
		spinner:     s,
	}, nil
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
		}
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
		if m.cursor < len(m.hosts)-1 {
			m.cursor++
		}
	case "a":
		m.form = newForm(config.Host{})
		m.mode = modeForm
	case "i":
		result, err := sshcfg.ImportHosts(m.store)
		if err != nil {
			m.status = fmt.Sprintf("Falha ao importar hosts: %v", err)
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		hosts, loadErr := m.store.LoadHosts()
		if loadErr == nil {
			m.hosts = hosts
			m.syncCursor()
		}
		switch {
		case result.Imported == 0 && result.Skipped == 0:
			m.status = "Nenhum host importavel encontrado no ssh config."
			m.statusStyle = m.theme.Subtle
		case result.Imported == 0:
			m.status = fmt.Sprintf("Importacao concluida. Nenhum host novo; %d aliases ja existiam.", result.Skipped)
			m.statusStyle = m.theme.Subtle
		case result.Skipped == 0:
			m.status = fmt.Sprintf("Importacao concluida. %d hosts adicionados.", result.Imported)
			m.statusStyle = m.theme.Success
		default:
			m.status = fmt.Sprintf("Importacao concluida. %d hosts adicionados e %d ignorados por alias existente.", result.Imported, result.Skipped)
			m.statusStyle = m.theme.Success
		}
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
	}

	footer := m.renderFooter()
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return m.theme.App.Render(content)
}

func (m Model) renderHeader() string {
	title := m.theme.Header.Render("ZenSSH")
	subtitle := m.theme.Subtle.Render("Gerencie aliases SSH com uma TUI elegante, direta e funcional.")
	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle)
}

func (m Model) renderList() string {
	welcome := m.renderWelcome()

	if len(m.hosts) == 0 {
		panel := m.theme.Panel.Render("Nenhum host cadastrado.\n\nPressione a para iniciar o cadastro guiado.")
		return lipgloss.JoinVertical(lipgloss.Left, welcome, panel)
	}

	lines := []string{}
	for i, host := range m.hosts {
		pointer := "  "
		line := fmt.Sprintf("%s  %s  %s  %s", badge(host.Group), host.Alias, host.Address(), portLabel(host.Port))
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
		shortcut{key: "e", label: "editar"},
		shortcut{key: "d", label: "remover"},
		shortcut{key: "g", label: "gerar chave"},
		shortcut{key: "s", label: "enviar chave"},
		shortcut{key: "q", label: "sair"},
	)
	panel := m.theme.Panel.Render(strings.Join(lines, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, welcome, panel, commands)
}

func (m Model) renderForm() string {
	rows := []string{m.theme.Accent.Render(m.form.title), ""}
	labels := []string{"Alias", "Host/IP", "Porta", "Usuario", "Grupo", "Arquivo da chave"}

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
	if len(m.hosts) == 0 || m.cursor < 0 || m.cursor >= len(m.hosts) {
		return config.Host{}, false
	}
	return m.hosts[m.cursor], true
}

func (m *Model) syncCursor() {
	if len(m.hosts) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.hosts) {
		m.cursor = len(m.hosts) - 1
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
	m.cursor = indexOfAlias(hosts, host.Alias)
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
	if err := m.store.SaveHosts(hosts); err != nil {
		return err
	}
	if err := sshcfg.WriteManagedConfig(m.store.ManagedSSHConfigPath(), hosts); err != nil {
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
		host.IdentityFile,
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
	identity := strings.TrimSpace(f.inputs[5].Value())

	if alias == "" || hostName == "" || user == "" {
		return config.Host{}, fmt.Errorf("alias, host e usuario são obrigatórios")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port <= 0 {
		return config.Host{}, fmt.Errorf("porta inválida")
	}
	if identity == "" {
		identity = config.DefaultIdentityFile()
	}
	if strings.HasPrefix(identity, "~/") {
		home, _ := os.UserHomeDir()
		identity = filepath.Join(home, strings.TrimPrefix(identity, "~/"))
	}

	return config.Host{
		Alias:        alias,
		HostName:     hostName,
		Port:         port,
		User:         user,
		Group:        group,
		IdentityFile: identity,
		SSHOptions:   append([]string{}, f.sshOptions...),
	}, nil
}

func runGenerateKey(host config.Host) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(host.IdentityFile), 0o700); err != nil {
			return actionResultMsg{op: opGenerateKey, err: err}
		}
		err := sshcfg.GenerateKey(host.IdentityFile)
		text := fmt.Sprintf("Chave pronta em %s.", host.IdentityFile)
		return actionResultMsg{op: opGenerateKey, err: err, text: text}
	}
}

func runPushKey(_ *config.Store, host config.Host) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(host.IdentityFile), 0o700); err != nil {
			return actionResultMsg{op: opPushKey, err: err}
		}
		if err := sshcfg.GenerateKey(host.IdentityFile); err != nil {
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

func runSaveAndConnect(store *config.Store, host config.Host, originalAlias string, sendKey bool) tea.Cmd {
	return func() tea.Msg {
		if err := upsertHost(store, host, originalAlias); err != nil {
			return actionResultMsg{op: opConnect, err: err}
		}

		if sendKey {
			if err := os.MkdirAll(filepath.Dir(host.IdentityFile), 0o700); err != nil {
				return actionResultMsg{op: opPushKey, err: err}
			}
			if err := sshcfg.GenerateKey(host.IdentityFile); err != nil {
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
		if err := os.MkdirAll(filepath.Dir(host.IdentityFile), 0o700); err != nil {
			return actionResultMsg{op: opPushKey, err: err, keepForm: true, form: newForm(config.Host{})}
		}
		if err := sshcfg.GenerateKey(host.IdentityFile); err != nil {
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
	if err := store.SaveHosts(hosts); err != nil {
		return err
	}
	return sshcfg.WriteManagedConfig(store.ManagedSSHConfigPath(), hosts)
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
	if err := store.SaveHosts(hosts); err != nil {
		return err
	}
	return sshcfg.WriteManagedConfig(store.ManagedSSHConfigPath(), hosts)
}

func markHostKeySent(store *config.Store, alias string) error {
	hosts, err := store.LoadHosts()
	if err != nil {
		return err
	}
	for i := range hosts {
		if hosts[i].Alias == alias {
			hosts[i].KeySent = true
			hosts[i].UpdatedAt = time.Now()
		}
	}
	if err := store.SaveHosts(hosts); err != nil {
		return err
	}
	return sshcfg.WriteManagedConfig(store.ManagedSSHConfigPath(), hosts)
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
