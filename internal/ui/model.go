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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"zenssh/internal/config"
	"zenssh/internal/sshcfg"
	"zenssh/internal/style"
)

type mode int

const ungroupedFilter = "\x00"

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeBusy
	modeImport
	modeSearch
	modeDetails
	modeConfirmRestore
	modeHelp
	modeBulkGroup
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
	identities []textinput.Model
	sshOptions []string
	termType   string
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
	store           *config.Store
	theme           style.Theme
	hosts           []config.Host
	cursor          int
	width           int
	height          int
	mode            mode
	form            formState
	status          string
	statusStyle     lipgloss.Style
	spinner         spinner.Model
	pendingOp       operation
	importer        importState
	search          textinput.Model
	query           string
	groupFilter     string
	selected        map[string]bool
	selectionMode   bool
	groupInput      textinput.Model
	details         string
	knownHosts      map[string]bool
	handoffCmd      *exec.Cmd
	handoffTermType string
	keys            keyMap
	help            help.Model
	viewport        viewport.Model
	layout          layoutState
}

func NewModel(store *config.Store) (Model, error) {
	hosts, err := store.LoadHosts()
	if err != nil {
		return Model{}, err
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	h := help.New()
	h.ShowAll = false
	theme := style.New()
	h.Styles.ShortKey = theme.Accent
	h.Styles.FullKey = theme.Accent
	h.Styles.ShortDesc = theme.Help
	h.Styles.FullDesc = theme.Help
	h.Styles.ShortSeparator = theme.Subtle
	h.Styles.FullSeparator = theme.Subtle
	h.Styles.Ellipsis = theme.Subtle

	m := Model{
		store:       store,
		theme:       theme,
		hosts:       hosts,
		mode:        modeList,
		status:      "Pronto. Use a para adicionar um host e Enter para conectar.",
		statusStyle: style.New().Subtle,
		spinner:     s,
		knownHosts:  map[string]bool{},
		selected:    map[string]bool{},
		keys:        newKeyMap(),
		help:        h,
		viewport:    viewport.New(80, 12),
		layout:      calculateLayout(100, 30),
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
		m.layout = calculateLayout(msg.Width, msg.Height)
		m.help.Width = m.layout.contentWidth
		m.viewport.Width = maxInt(20, m.layout.listWidth-4)
		m.viewport.Height = maxInt(3, m.layout.listHeight-4)
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
		if msg.op == opConnect {
			if err := persistHostOptions(m.store, msg.host); err != nil {
				m.mode = modeList
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
				return m, nil
			}
			m.handoffCmd = msg.cmd
			m.handoffTermType = msg.host.TermType
			return m, tea.Quit
		}
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
				if err := persistHostOptions(m.store, updatedHost); err != nil {
					m.pendingOp = opNone
					m.mode = modeList
					m.status = fmt.Sprintf("Falha: %v", err)
					m.statusStyle = m.theme.Danger
					return m, nil
				}
				m.pendingOp = opConnect
				m.handoffCmd = cmd
				m.handoffTermType = msg.host.TermType
				return m, tea.Quit
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
		case modeHelp:
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.mode = modeList
			}
			return m, nil
		case modeBulkGroup:
			return m.updateBulkGroup(msg)
		}
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "S":
		m.selectionMode = !m.selectionMode
		if m.selectionMode {
			m.status = "Modo de selecao ativo. Use Espaco para marcar hosts e Shift+G para agrupá-los."
		} else {
			m.selected = map[string]bool{}
			m.status = "Modo de selecao encerrado."
		}
		m.statusStyle = m.theme.Subtle
	case " ":
		if !m.selectionMode {
			return m, nil
		}
		if host, ok := m.currentHost(); ok {
			if m.selected == nil {
				m.selected = map[string]bool{}
			}
			if m.selected[host.Alias] {
				delete(m.selected, host.Alias)
			} else {
				m.selected[host.Alias] = true
			}
			m.status = fmt.Sprintf("%d host(s) selecionado(s).", len(m.selected))
			m.statusStyle = m.theme.Subtle
		}
	case "x":
		m.selected = map[string]bool{}
		m.selectionMode = false
		m.status = "Modo de selecao encerrado."
		m.statusStyle = m.theme.Subtle
	case "esc":
		if m.selectionMode {
			m.selected = map[string]bool{}
			m.selectionMode = false
			m.status = "Modo de selecao encerrado."
			m.statusStyle = m.theme.Subtle
		}
	case "[":
		m.cycleGroup(-1)
	case "]":
		m.cycleGroup(1)
	case "G":
		if !m.selectionMode || len(m.selected) == 0 {
			m.status = "Selecione um ou mais hosts com Espaco antes de agrupar."
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		m.groupInput = textinput.New()
		m.groupInput.Placeholder = "nome do grupo; vazio remove o grupo"
		m.groupInput.Width = maxInt(24, minInt(50, m.layout.contentWidth-8))
		m.groupInput.Focus()
		m.mode = modeBulkGroup
		return m, textinput.Blink
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
			if err := persistHostOptions(m.store, updatedHost); err != nil {
				m.status = fmt.Sprintf("Falha: %v", err)
				m.statusStyle = m.theme.Danger
				return m, nil
			}
			m.pendingOp = opConnect
			m.handoffCmd = cmd
			m.handoffTermType = updatedHost.TermType
			return m, tea.Quit
		}
	}
	return m, nil
}

// HandoffCommand returns the command that should take over the terminal after
// Bubble Tea has restored it. A nil command means the user only quit.
func (m Model) HandoffCommand() *exec.Cmd {
	return m.handoffCmd
}

// HandoffTermType returns the TERM value to apply before SSH starts.
func (m Model) HandoffTermType() string {
	return m.handoffTermType
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

func (m Model) updateBulkGroup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.status = "Alteracao de grupo cancelada."
		m.statusStyle = m.theme.Subtle
		return m, nil
	case "enter":
		group := strings.TrimSpace(m.groupInput.Value())
		hosts := append([]config.Host{}, m.hosts...)
		updated := 0
		for i := range hosts {
			if !m.selected[hosts[i].Alias] {
				continue
			}
			hosts[i].Group = group
			hosts[i].UpdatedAt = time.Now()
			updated++
		}
		if err := sshcfg.SaveAll(m.store, hosts); err != nil {
			m.status = fmt.Sprintf("Falha ao atualizar grupos: %v", err)
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		m.hosts = hosts
		m.selected = map[string]bool{}
		m.selectionMode = false
		if group == "" {
			m.groupFilter = ungroupedFilter
		} else {
			m.groupFilter = group
		}
		m.cursor = 0
		m.mode = modeList
		m.status = fmt.Sprintf("%d host(s) movido(s) para %s.", updated, groupDisplayName(m.groupFilter))
		m.statusStyle = m.theme.Success
		return m, nil
	}
	var cmd tea.Cmd
	m.groupInput, cmd = m.groupInput.Update(msg)
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
	fieldCount := m.form.fieldCount()
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.status = "Cadastro cancelado."
		m.statusStyle = m.theme.Subtle
		return m, nil
	case "ctrl+n":
		identity := newIdentityInput("")
		m.form.blurAll()
		m.form.identities = append(m.form.identities, identity)
		m.form.cursor = len(m.form.inputs) + len(m.form.identities) - 1
		m.form.focusCurrent()
		return m, textinput.Blink
	case "ctrl+d":
		identityIndex := m.form.cursor - len(m.form.inputs)
		if identityIndex >= 0 && identityIndex < len(m.form.identities) && len(m.form.identities) > 1 {
			m.form.identities = append(m.form.identities[:identityIndex], m.form.identities[identityIndex+1:]...)
			if identityIndex >= len(m.form.identities) {
				identityIndex = len(m.form.identities) - 1
			}
			m.form.cursor = len(m.form.inputs) + identityIndex
			m.form.focusCurrent()
		}
		return m, nil
	case "tab", "shift+tab", "up", "down":
		delta := 1
		if msg.String() == "shift+tab" || msg.String() == "up" {
			delta = -1
		}
		m.form.blurAll()
		totalItems := fieldCount + 5
		m.form.cursor = (m.form.cursor + delta + totalItems) % totalItems
		m.form.focusCurrent()
		return m, nil
	case " ":
		if m.form.cursor == fieldCount {
			m.form.termType = nextTermType(m.form.termType, 1)
			return m, nil
		}
		if m.form.cursor == fieldCount+1 {
			m.form.sendKey = !m.form.sendKey
			return m, nil
		}
	case "left", "right":
		if m.form.cursor == fieldCount {
			delta := 1
			if msg.String() == "left" {
				delta = -1
			}
			m.form.termType = nextTermType(m.form.termType, delta)
			return m, nil
		}
	case "enter":
		if m.form.cursor < fieldCount {
			m.form.blurAll()
			m.form.cursor++
			m.form.focusCurrent()
			return m, nil
		}
		if m.form.cursor == fieldCount {
			m.form.termType = nextTermType(m.form.termType, 1)
			return m, nil
		}
		if m.form.cursor == fieldCount+1 {
			m.form.sendKey = !m.form.sendKey
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
	if m.form.cursor < len(m.form.inputs) {
		m.form.inputs[m.form.cursor], cmd = m.form.inputs[m.form.cursor].Update(msg)
	} else if identityIndex := m.form.cursor - len(m.form.inputs); identityIndex >= 0 && identityIndex < len(m.form.identities) {
		m.form.identities[identityIndex], cmd = m.form.identities[identityIndex].Update(msg)
	}
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
	case modeHelp:
		body = m.renderHelp()
	case modeBulkGroup:
		body = m.renderBulkGroup()
	}

	footer := m.renderFooter()
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return m.theme.App.Render(content)
}

func (m Model) renderImport() string {
	rows := []string{m.theme.Accent.Render("Sincronizar configuracao SSH"), m.theme.Subtle.Render("Revise o que sera trazido para o ZenSSH antes de confirmar."), ""}
	if len(m.importer.candidates) == 0 {
		rows = append(rows, "Nenhum alias SSH ou candidato em /etc/hosts foi encontrado.")
	}
	compact := m.layout.variant == layoutCompact
	if compact {
		rows = append(rows, m.theme.TableHeader.Render(fmt.Sprintf("   %-3s %-14s %-18s", "SEL", "ALIAS", "DESTINO")))
	} else {
		rows = append(rows, m.theme.TableHeader.Render(fmt.Sprintf("   %-3s %-18s %-25s %-10s", "SEL", "ALIAS", "DESTINO", "STATUS")))
	}
	visibleHeight := maxInt(4, m.layout.contentHeight-11)
	start := maxInt(0, m.importer.cursor-visibleHeight+1)
	end := minInt(len(m.importer.candidates), start+visibleHeight)
	for i := start; i < end; i++ {
		candidate := m.importer.candidates[i]
		mark := "[ ]"
		if candidate.Selected {
			mark = "[x]"
		}
		pointer := "  "
		if i == m.importer.cursor {
			pointer = "▶ "
		}
		line := fmt.Sprintf("%s%-3s %-18s %-25s %-10s", pointer, mark, fitText(candidate.Host.Alias, 18), fitText(candidate.Host.Address(), 25), fitText(candidate.Status, 10))
		if compact {
			line = fmt.Sprintf("%s%-3s %-14s %-18s", pointer, mark, fitText(candidate.Host.Alias, 14), fitText(candidate.Host.Address(), 18))
		}
		if i == m.importer.cursor {
			line = m.theme.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	if len(m.importer.candidates) > 0 {
		candidate := m.importer.candidates[m.importer.cursor]
		origin := candidate.Host.Source
		if candidate.Optional {
			origin = "/etc/hosts (opcional)"
		}
		detail := fmt.Sprintf("Origem: %s · Modo: %s · Chave: %s", origin, candidate.Host.Management, filepath.Base(candidate.Host.PrimaryIdentity()))
		rows = append(rows, "", m.theme.PanelTitle.Render("Item selecionado"), fitText(detail, maxInt(24, m.layout.contentWidth-5)))
	}
	rows = append(rows, "", fmt.Sprintf("%d chaves privadas encontradas; os arquivos sao apenas referenciados.", len(m.importer.keys)), "", renderShortcuts(m.theme,
		shortcut{key: "Espaco", label: "marcar"}, shortcut{key: "a", label: "todos"}, shortcut{key: "n", label: "nenhum"}, shortcut{key: "c", label: "alternar chave"}, shortcut{key: "o", label: "gerenciado/leitura"}, shortcut{key: "Enter", label: "importar"}, shortcut{key: "Esc", label: "cancelar"},
	))
	return m.theme.Panel.Render(strings.Join(rows, "\n"))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m Model) renderHeader() string {
	title := m.theme.Header.Render("ZenSSH")
	count := fmt.Sprintf("%d hosts", len(m.visibleHosts()))
	if m.groupFilter != "" {
		count += " · grupo: " + groupDisplayName(m.groupFilter)
	}
	if m.query != "" {
		count += fmt.Sprintf(" · filtro: %q", m.query)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, title, m.theme.Subtle.Render("  "+count))
}

func (m Model) renderList() string {
	return m.renderDashboard()
}

func (m Model) renderBulkGroup() string {
	content := strings.Join([]string{
		m.theme.PanelTitle.Render("Alterar grupo em massa"),
		"",
		fmt.Sprintf("%d host(s) selecionado(s)", len(m.selected)),
		m.theme.Subtle.Render("Informe o novo grupo. Deixe vazio para remover o grupo atual."),
		"",
		m.groupInput.View(),
		"",
		renderShortcuts(m.theme, shortcut{key: "Enter", label: "aplicar"}, shortcut{key: "Esc", label: "cancelar"}),
	}, "\n")
	return m.theme.Panel.Width(maxInt(30, minInt(64, m.layout.contentWidth-2))).Render(content)
}

func (m Model) renderForm() string {
	fieldCount := m.form.fieldCount()
	totalItems := fieldCount + 5
	step := minInt(maxInt(m.form.cursor, 0), totalItems-1)
	compact := m.layout.contentHeight < 14
	rows := []string{lipgloss.JoinHorizontal(lipgloss.Center,
		m.theme.Accent.Render(m.form.title),
		m.theme.Subtle.Render(fmt.Sprintf("  ·  %d/%d", step+1, totalItems)),
	), ""}
	inputWidth := maxInt(18, minInt(52, m.layout.contentWidth-8))
	labels := []string{"Alias", "Host/IP", "Porta", "Usuario", "Grupo"}

	switch {
	case step < len(m.form.inputs):
		input := m.form.inputs[step]
		input.Width = inputWidth
		if !compact {
			rows = append(rows, m.theme.PanelTitle.Render("Conexao"), "")
		}
		rows = append(rows, m.theme.InputLabel.Render(labels[step]), input.View())
	case step < fieldCount:
		identityIndex := step - len(m.form.inputs)
		input := m.form.identities[identityIndex]
		input.Width = inputWidth
		if !compact {
			rows = append(rows, m.theme.PanelTitle.Render("Identidade SSH"), "")
		}
		rows = append(rows, m.theme.InputLabel.Render(fmt.Sprintf("Chave %d", identityIndex+1)), input.View())
		if !compact {
			rows = append(rows, "", m.theme.Subtle.Render("Ctrl+N adiciona outra chave · Ctrl+D remove esta chave"))
		}
	case step == fieldCount:
		if !compact {
			rows = append(rows, m.theme.PanelTitle.Render("Terminal remoto"), "")
		}
		rows = append(rows, fmt.Sprintf("TERM: [%s]", termTypeLabel(m.form.termType)))
		if !compact {
			rows = append(rows, "", m.theme.Subtle.Render("Use ←/→, Espaco ou Enter para alterar."))
		}
	case step == fieldCount+1:
		value := "Nao"
		if m.form.sendKey {
			value = "Sim"
		}
		if !compact {
			rows = append(rows, m.theme.PanelTitle.Render("Autenticacao"), "")
		}
		rows = append(rows, fmt.Sprintf("Enviar chave agora: [%s]", value))
		if !compact {
			rows = append(rows, "", m.theme.Subtle.Render("Use Espaco ou Enter para alterar."))
		}
	default:
		action := "Salvar e sair"
		if step == fieldCount+3 {
			action = "Salvar, testar e conectar"
		} else if step == fieldCount+4 {
			action = "Salvar e adicionar outro"
		}
		if !compact {
			rows = append(rows, m.theme.PanelTitle.Render("Concluir"), "")
		}
		rows = append(rows, m.theme.Selected.Render("  "+action+"  "))
		if !compact {
			rows = append(rows, "", m.theme.Subtle.Render("Pressione Enter para executar esta acao."))
		}
	}

	shortcuts := []shortcut{{key: "Tab", label: "proxima"}, {key: "Esc", label: "cancelar"}}
	if !compact {
		shortcuts = []shortcut{{key: "Tab", label: "proxima"}, {key: "Shift+Tab", label: "anterior"}, {key: "Esc", label: "cancelar"}}
	}
	rows = append(rows, "", renderShortcuts(m.theme, shortcuts...))
	return m.theme.Panel.Width(maxInt(28, m.layout.contentWidth-2)).Render(strings.Join(rows, "\n"))
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
	helpView := m.help.View(m.keys)
	if m.selectionMode {
		helpView = m.help.ShortHelpView(m.keys.SelectionHelp())
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.statusStyle.Render(fitText(m.status, m.layout.contentWidth)), m.theme.Help.Render(helpView))
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
	result := make([]config.Host, 0, len(m.hosts))
	for _, host := range m.hosts {
		if m.groupFilter == ungroupedFilter && strings.TrimSpace(host.Group) != "" {
			continue
		}
		if m.groupFilter != "" && m.groupFilter != ungroupedFilter && !strings.EqualFold(host.Group, m.groupFilter) {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{host.Alias, host.HostName, host.User, host.Group, host.Source, host.SourcePath}, " "))
		if query == "" || strings.Contains(haystack, query) {
			result = append(result, host)
		}
	}
	return result
}

func (m Model) groupTabs() []string {
	tabs := []string{""}
	seen := map[string]bool{}
	hasUngrouped := false
	groups := []string{}
	for _, host := range m.hosts {
		group := strings.TrimSpace(host.Group)
		if group == "" {
			hasUngrouped = true
			continue
		}
		key := strings.ToLower(group)
		if !seen[key] {
			seen[key] = true
			groups = append(groups, group)
		}
	}
	slices.SortFunc(groups, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	tabs = append(tabs, groups...)
	if hasUngrouped {
		tabs = append(tabs, ungroupedFilter)
	}
	return tabs
}

func (m *Model) cycleGroup(delta int) {
	tabs := m.groupTabs()
	current := 0
	for i, group := range tabs {
		if group == m.groupFilter {
			current = i
			break
		}
	}
	m.groupFilter = tabs[(current+delta+len(tabs))%len(tabs)]
	m.cursor = 0
	m.status = "Filtro de grupo: " + groupDisplayName(m.groupFilter)
	m.statusStyle = m.theme.Subtle
}

func groupDisplayName(group string) string {
	switch group {
	case "":
		return "Todos"
	case ungroupedFilter:
		return "Sem grupo"
	default:
		return group
	}
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
	return fmt.Sprintf("Diagnostico de %s\n\nDestino: %s@%s:%d\nOrigem: %s\nArquivo: %s\nModo: %s\nIdentidades: %s\nTERM remoto: %s\nAutenticacao por chave: %s\nUltimo teste: %s\nOpcoes efetivas: %s\n\nComando equivalente:\nssh -p %d %s@%s", host.Alias, host.User, host.HostName, host.Port, sourceLabel(host), host.SourcePath, host.Management, identities, termTypeLabel(host.TermType), keyStatusLabel(host), lastCheck, strings.Join(optionNames, ", "), host.Port, host.User, host.HostName)
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
	if m.form.original != "" && m.selected[m.form.original] {
		delete(m.selected, m.form.original)
		m.selected[host.Alias] = true
	}
	visibleHosts := m.visibleHosts()
	if index := indexOfAlias(visibleHosts, host.Alias); index >= 0 {
		m.cursor = index
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
	delete(m.selected, alias)
	m.syncCursor()
	return nil
}

func newForm(host config.Host) formState {
	inputs := make([]textinput.Model, 5)
	placeholders := []string{
		"app-prod",
		"10.0.0.25 ou server.exemplo.com",
		"22",
		"ubuntu",
		"producao",
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
	}

	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Placeholder = placeholders[i]
		inputs[i].SetValue(values[i])
		inputs[i].Width = 42
	}
	inputs[0].Focus()
	identityValues := append([]string{}, host.IdentityFiles...)
	if len(identityValues) == 0 {
		identityValues = []string{""}
	}
	identities := make([]textinput.Model, len(identityValues))
	for i, value := range identityValues {
		identities[i] = newIdentityInput(value)
	}

	return formState{
		title:      "Novo host",
		inputs:     inputs,
		identities: identities,
		sshOptions: append([]string{}, host.SSHOptions...),
		termType:   host.TermType,
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

	port, err := strconv.Atoi(portValue)
	if err != nil {
		return config.Host{}, fmt.Errorf("porta inválida")
	}
	var identities []string
	for _, input := range f.identities {
		identity := strings.TrimSpace(input.Value())
		if strings.HasPrefix(identity, "~/") {
			home, _ := os.UserHomeDir()
			identity = filepath.Join(home, strings.TrimPrefix(identity, "~/"))
		}
		if identity != "" {
			identities = append(identities, identity)
		}
	}
	if len(identities) == 0 {
		identities = []string{config.DefaultIdentityFile()}
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
	host.TermType = normalizedTermType(f.termType)
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
	return -1
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
	fieldCount := form.fieldCount()
	switch form.cursor {
	case fieldCount + 3:
		return formActionSaveConnect
	case fieldCount + 4:
		return formActionSaveAddAnother
	default:
		return formActionSaveExit
	}
}

func normalizedTermType(mode string) string {
	if mode == "" {
		return config.TermSystem
	}
	return mode
}

func nextTermType(mode string, delta int) string {
	modes := []string{config.TermSystem, config.TermXterm}
	mode = normalizedTermType(mode)
	index := 0
	for i := range modes {
		if modes[i] == mode {
			index = i
			break
		}
	}
	return modes[(index+delta+len(modes))%len(modes)]
}

func termTypeLabel(mode string) string {
	switch normalizedTermType(mode) {
	case config.TermXterm:
		return "xterm"
	default:
		return "Padrao do sistema"
	}
}

func newIdentityInput(value string) textinput.Model {
	input := textinput.New()
	input.Placeholder = config.DefaultIdentityFile()
	input.SetValue(value)
	input.Width = 42
	return input
}

func (f formState) fieldCount() int {
	return len(f.inputs) + len(f.identities)
}

func (f *formState) blurAll() {
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	for i := range f.identities {
		f.identities[i].Blur()
	}
}

func (f *formState) focusCurrent() {
	if f.cursor < len(f.inputs) {
		f.inputs[f.cursor].Focus()
		return
	}
	identityIndex := f.cursor - len(f.inputs)
	if identityIndex >= 0 && identityIndex < len(f.identities) {
		f.identities[identityIndex].Focus()
	}
}
