package ui

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"zenssh/internal/filetransfer"
	"zenssh/internal/sshcfg"
)

type filePane struct {
	dir     string
	entries []filetransfer.Entry
	cursor  int
}

type filesState struct {
	client        filetransfer.Client
	alias         string
	local, remote filePane
	remoteActive  bool
	confirm       bool
	input         *textinput.Model
}

type filesListedMsg struct {
	remote  bool
	dir     string
	entries []filetransfer.Entry
	err     error
}
type fileCopiedMsg struct{ err error }

func (m Model) openFiles() (tea.Model, tea.Cmd) {
	host, ok := m.currentHost()
	if !ok {
		return m, nil
	}
	_, ssh, err := sshcfg.PrepareConnect(host)
	if err != nil {
		m.status = "Falha: " + err.Error()
		m.statusStyle = m.theme.Danger
		return m, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		m.status = err.Error()
		m.statusStyle = m.theme.Danger
		return m, nil
	}
	dir, entries, err := filetransfer.ListLocal(dir)
	if err != nil {
		m.status = err.Error()
		m.statusStyle = m.theme.Danger
		return m, nil
	}
	m.files = filesState{client: filetransfer.New(ssh), alias: host.Alias, local: filePane{dir: dir, entries: entries}}
	m.mode = modeFiles
	m.status = "Arquivos · c copia o item selecionado para a pasta do outro painel."
	m.statusStyle = m.theme.Subtle
	return m, m.listRemote(".")
}

func (m Model) listRemote(dir string) tea.Cmd {
	cmd := m.files.client.ListCommand(dir)
	var output bytes.Buffer
	cmd.Stdout = &output
	// Release the TUI terminal for host-key/password prompts. stdout alone is
	// captured; SSH diagnostics and authentication remain on the real terminal.
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return filesListedMsg{remote: true, err: fmt.Errorf("SSH: %w (veja a mensagem no terminal)", err)}
		}
		resolved, entries, err := filetransfer.ParseListing(output.Bytes())
		return filesListedMsg{remote: true, dir: resolved, entries: entries, err: err}
	})
}

func (m Model) listLocal(dir string) tea.Cmd {
	return func() tea.Msg {
		resolved, entries, err := filetransfer.ListLocal(dir)
		return filesListedMsg{dir: resolved, entries: entries, err: err}
	}
}

func (m *Model) activeFilePane() *filePane {
	if m.files.remoteActive {
		return &m.files.remote
	}
	return &m.files.local
}

func (m Model) updateFiles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.files.input != nil {
		switch k {
		case "esc":
			m.files.input = nil
			return m, nil
		case "enter":
			dir := m.files.input.Value()
			m.files.input = nil
			if dir == "" {
				return m, nil
			}
			if m.files.remoteActive {
				if !path.IsAbs(dir) {
					dir = path.Join(m.files.remote.dir, dir)
				}
				return m, m.listRemote(dir)
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(m.files.local.dir, dir)
			}
			return m, m.listLocal(dir)
		}
		input, cmd := m.files.input.Update(msg)
		m.files.input = &input
		return m, cmd
	}
	if m.files.confirm {
		if k == "n" || k == "esc" {
			m.files.confirm = false
			return m, nil
		}
		if k != "y" {
			return m, nil
		}
		m.files.confirm = false
		return m.copySelectedFile()
	}
	pane := m.activeFilePane()
	switch k {
	case "esc", "q":
		m.mode = modeList
		m.files = filesState{}
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "tab", "shift+tab":
		m.files.remoteActive = !m.files.remoteActive
	case "up", "k":
		if pane.cursor > 0 {
			pane.cursor--
		}
	case "down", "j":
		if pane.cursor < len(pane.entries) {
			pane.cursor++
		}
	case "r":
		if m.files.remoteActive {
			dir := pane.dir
			if dir == "" {
				dir = "."
			}
			return m, m.listRemote(dir)
		}
		return m, m.listLocal(pane.dir)
	case "/":
		input := textinput.New()
		input.Prompt = "Pasta: "
		input.SetValue(pane.dir)
		input.CharLimit = 4096
		input.Focus()
		m.files.input = &input
		return m, textinput.Blink
	case "backspace", "enter":
		dir := pane.dir
		if dir == "" {
			return m, nil
		}
		if k == "backspace" || pane.cursor == 0 {
			if m.files.remoteActive {
				dir = path.Dir(dir)
			} else {
				dir = filepath.Dir(dir)
			}
		} else if pane.cursor <= len(pane.entries) && pane.entries[pane.cursor-1].IsDir() {
			name := pane.entries[pane.cursor-1].Name
			if m.files.remoteActive {
				dir = path.Join(dir, name)
			} else {
				dir = filepath.Join(dir, name)
			}
		} else {
			return m, nil
		}
		if m.files.remoteActive {
			return m, m.listRemote(dir)
		}
		return m, m.listLocal(dir)
	case "c":
		if pane.cursor == 0 || pane.cursor > len(pane.entries) {
			return m, nil
		}
		if !pane.entries[pane.cursor-1].CanCopy() {
			m.status = "Selecione um arquivo regular ou pasta; links e arquivos especiais nao sao copiados diretamente."
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		if m.files.remote.dir == "" {
			m.status = "Atualize o painel remoto com r antes de transferir."
			m.statusStyle = m.theme.Danger
			return m, nil
		}
		m.files.confirm = true
	}
	return m, nil
}

func (m Model) copySelectedFile() (tea.Model, tea.Cmd) {
	pane := m.activeFilePane()
	if pane.cursor < 1 || pane.cursor > len(pane.entries) {
		return m, nil
	}
	entry := pane.entries[pane.cursor-1]
	if !entry.CanCopy() || m.files.remote.dir == "" {
		return m, nil
	}
	local, remote := m.files.local.dir, m.files.remote.dir
	if m.files.remoteActive {
		remote = path.Join(remote, entry.Name)
	} else {
		local = filepath.Join(local, entry.Name)
	}
	cmd, err := m.files.client.CopyCommand(local, remote, !m.files.remoteActive, entry.IsDir())
	if err != nil {
		m.status = err.Error()
		m.statusStyle = m.theme.Danger
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return fileCopiedMsg{err: err} })
}

func (m Model) renderFiles() string {
	width := maxInt(24, (m.layout.contentWidth-5)/2)
	if m.width < 70 {
		width = maxInt(24, m.layout.contentWidth-4)
	}
	rows := maxInt(3, m.layout.contentHeight-9)
	if m.files.confirm {
		rows = maxInt(3, rows-5)
	}
	render := func(p filePane, title string, active bool) string {
		if active {
			title = "› " + title
		}
		lines := []string{m.theme.PanelTitle.Render(title), fitText(fileDisplayText(p.dir), width-2), ""}
		start := maxInt(0, p.cursor-rows+1)
		for i := start; i <= len(p.entries) && i < start+rows; i++ {
			label := "../"
			if i > 0 {
				e := p.entries[i-1]
				name := e.Name
				if e.IsDir() {
					name += "/"
				} else if e.Kind == "l" {
					name += " [link]"
				}
				label = fitText(fmt.Sprintf("%s  %d B", fileDisplayText(name), e.Size), width-2)
			}
			if i == p.cursor && active {
				label = m.theme.Selected.Render("› " + label)
			}
			lines = append(lines, label)
		}
		return m.theme.Panel.Width(width).Render(strings.Join(lines, "\n"))
	}
	local := render(m.files.local, "Local", !m.files.remoteActive)
	remote := render(m.files.remote, "Remoto · "+m.files.alias, m.files.remoteActive)
	body := lipgloss.JoinHorizontal(lipgloss.Top, local, " ", remote)
	if m.width < 70 {
		body = render(*m.activeFilePane(), map[bool]string{false: "Local", true: "Remoto · " + m.files.alias}[m.files.remoteActive], true)
	}
	if m.files.input != nil {
		body += "\n" + m.files.input.View()
	}
	if m.files.confirm {
		pane := m.activeFilePane()
		entry := pane.entries[pane.cursor-1]
		destination := m.files.remote.dir
		direction := "Enviar"
		if m.files.remoteActive {
			direction = "Baixar"
			destination = m.files.local.dir
		}
		body += fmt.Sprintf("\n%s %q para %q?\nArquivos existentes com o mesmo nome serao sobrescritos.\nPastas serao mescladas recursivamente; SCP segue links dentro delas.\ny confirma · n/Esc cancela", direction, entry.Name, destination)
	}
	return body + "\n\nTab: painel · ↑/↓: selecionar · Enter: abrir · Backspace: subir\nc: copiar · /: caminho · r: atualizar · Esc: hosts"
}

func fileDisplayText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r >= 127 && r <= 159 {
			return '�'
		}
		return r
	}, s)
}
