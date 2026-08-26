package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"zenssh/internal/config"
)

func (m Model) renderDashboard() string {
	hosts := m.visibleHosts()
	if len(hosts) == 0 {
		message := "Nenhum host cadastrado"
		if m.query != "" {
			message = "Nenhum host corresponde ao filtro"
		}
		return m.theme.Panel.Width(maxInt(28, m.layout.contentWidth-2)).Render(
			m.renderActiveSearch() + m.renderGroupTabs() + "\n\n" + m.theme.PanelTitle.Render(message) + "\n\n" +
				m.theme.Subtle.Render("Use a para adicionar ou i para importar sua configuracao SSH."),
		)
	}

	cursor := m.cursor
	if cursor < 0 {
		cursor = 0
	} else if cursor >= len(hosts) {
		cursor = len(hosts) - 1
	}
	m.cursor = cursor
	list := m.renderHostList(hosts)
	detail := m.renderHostDetail(hosts[cursor])
	switch m.layout.variant {
	case layoutSplit:
		return lipgloss.JoinHorizontal(lipgloss.Top, list, " ", detail)
	case layoutStacked:
		return lipgloss.JoinVertical(lipgloss.Left, list, detail)
	default:
		return list
	}
}

func (m Model) renderHostList(hosts []config.Host) string {
	innerWidth := maxInt(18, m.layout.listWidth-4)
	aliasWidth := 16
	destinationWidth := maxInt(12, innerWidth-aliasWidth-25)
	if m.layout.variant == layoutCompact {
		aliasWidth = maxInt(8, innerWidth/3)
		destinationWidth = maxInt(6, innerWidth-aliasWidth-14)
	}

	lines := make([]string, 0, len(hosts)+2)
	if m.layout.variant == layoutCompact {
		lines = append(lines, m.theme.TableHeader.Render(fmt.Sprintf("      %-*s %-*s %s", aliasWidth, "HOST", destinationWidth, "DESTINO", "CHAVE")))
	} else {
		lines = append(lines, m.theme.TableHeader.Render(fmt.Sprintf("      %-*s %-*s %-8s %-8s", aliasWidth, "HOST", destinationWidth, "DESTINO", "CHAVE", "ORIGEM")))
	}
	for i, host := range hosts {
		alias := fitText(host.Alias, aliasWidth)
		destination := fitText(host.Address(), destinationWidth)
		key := compactKeyStatus(host)
		var line string
		if m.selectionMode {
			mark := "[ ]"
			if m.selected[host.Alias] {
				mark = "[x]"
			}
			if m.layout.variant == layoutCompact {
				line = fmt.Sprintf("  %s %-*s %-*s %-6s", mark, aliasWidth, alias, destinationWidth, destination, fitText(key, 6))
			} else {
				line = fmt.Sprintf("  %s %-*s %-*s %-8s %-8s", mark, aliasWidth, alias, destinationWidth, destination, key, fitText(sourceLabel(host), 8))
			}
		} else if m.layout.variant == layoutCompact {
			line = fmt.Sprintf("      %-*s %-*s %-6s", aliasWidth, alias, destinationWidth, destination, fitText(key, 6))
		} else {
			line = fmt.Sprintf("      %-*s %-*s %-8s %-8s", aliasWidth, alias, destinationWidth, destination, key, fitText(sourceLabel(host), 8))
		}
		if i == m.cursor {
			line = m.theme.Selected.Width(innerWidth).Render("›" + line[1:])
		}
		lines = append(lines, line)
	}

	vp := m.viewport
	vp.Width = innerWidth
	vp.Height = maxInt(3, m.layout.listHeight-4)
	if len(m.recentHosts()) > 0 {
		vp.Height = maxInt(3, vp.Height-1)
	}
	if m.mode == modeSearch {
		vp.Height = maxInt(3, vp.Height-2)
	}
	vp.SetContent(strings.Join(lines, "\n"))
	selectedLine := m.cursor + 1
	if selectedLine < vp.YOffset {
		vp.SetYOffset(selectedLine)
	} else if selectedLine >= vp.YOffset+vp.Height {
		vp.SetYOffset(selectedLine - vp.Height + 1)
	}
	title := m.theme.PanelTitle.Render("Hosts")
	if m.selectionMode {
		title += m.theme.Subtle.Render(fmt.Sprintf(" · %d selecionados", len(m.selected)))
	}
	position := m.theme.Subtle.Render(fmt.Sprintf("%d/%d", m.cursor+1, len(hosts)))
	heading := lipgloss.JoinHorizontal(lipgloss.Center, title, strings.Repeat(" ", maxInt(1, innerWidth-lipgloss.Width(title)-lipgloss.Width(position))), position)
	recent := m.renderRecentConnections(innerWidth)
	return m.theme.Panel.Width(m.layout.listWidth - 2).Height(maxInt(4, m.layout.listHeight-2)).Render(m.renderActiveSearch() + recent + m.renderGroupTabs() + "\n" + heading + "\n" + vp.View())
}

func (m Model) renderActiveSearch() string {
	if m.mode != modeSearch {
		return ""
	}
	return m.theme.InputLabel.Render("Buscar: ") + m.search.View() + "\n" +
		m.theme.Subtle.Render("Resultados atualizados enquanto voce digita · Enter confirma · Esc cancela") + "\n"
}

func (m Model) recentHosts() []config.Host {
	hosts := slices.Clone(m.hosts)
	hosts = slices.DeleteFunc(hosts, func(host config.Host) bool { return host.LastConnectedAt.IsZero() })
	slices.SortFunc(hosts, func(a, b config.Host) int { return b.LastConnectedAt.Compare(a.LastConnectedAt) })
	if len(hosts) > 3 {
		hosts = hosts[:3]
	}
	return hosts
}

func (m Model) renderRecentConnections(width int) string {
	hosts := m.recentHosts()
	if len(hosts) == 0 {
		return ""
	}
	items := make([]string, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, host.Alias)
	}
	line := "Recentes: " + strings.Join(items, "  ·  ")
	return m.theme.Accent.Render(fitText(line, width)) + "\n"
}

func (m Model) renderGroupTabs() string {
	tabs := m.groupTabs()
	current := 0
	for i, group := range tabs {
		if group == m.groupFilter {
			current = i
			break
		}
	}
	start := maxInt(0, current-1)
	end := minInt(len(tabs), start+3)
	start = maxInt(0, end-3)
	parts := make([]string, 0, end-start)
	for _, group := range tabs[start:end] {
		label := groupDisplayName(group)
		if group == m.groupFilter {
			parts = append(parts, m.theme.Selected.Render(" "+label+" "))
		} else {
			parts = append(parts, m.theme.Subtle.Render(" "+label+" "))
		}
	}
	return "‹ [  " + strings.Join(parts, "  ") + "  ] ›"
}

func (m Model) renderHostDetail(host config.Host) string {
	width := maxInt(24, m.layout.detailWidth-4)
	identity := "OpenSSH / ssh-agent"
	if len(host.IdentityFiles) > 0 {
		identity = filepath.Base(host.PrimaryIdentity())
	}
	known := m.knownHosts[strings.ToLower(host.Alias)]
	rows := []string{
		m.theme.PanelTitle.Render(fitText(host.Alias, width)),
		m.theme.Subtle.Render(fitText(host.Group, width)),
		"",
		detailRow("Destino", host.Address(), width),
		detailRow("Porta", fmt.Sprintf("%d", host.Port), width),
		detailRow("Usuario", host.User, width),
		detailRow("Chave", identity, width),
		detailRow("Autenticacao", keyStatusLabel(host), width),
		detailRow("Servidor", knownStatusLabel(known), width),
		detailRow("Origem", sourceLabel(host), width),
		detailRow("Modo", string(host.Management), width),
	}
	return m.theme.Panel.Width(m.layout.detailWidth - 2).Height(maxInt(4, m.layout.detailHeight-2)).Render(strings.Join(rows, "\n"))
}

func (m Model) renderHelp() string {
	h := m.help
	h.ShowAll = true
	h.Width = maxInt(30, m.layout.contentWidth-4)
	content := m.theme.PanelTitle.Render("Atalhos") + "\n\n" + h.View(m.keys) + "\n\n" + m.theme.Subtle.Render("? ou Esc fecha a ajuda")
	return m.theme.Panel.Width(maxInt(28, m.layout.contentWidth-2)).Render(content)
}

func detailRow(label, value string, width int) string {
	const labelWidth = 13
	return fmt.Sprintf("%-*s %s", labelWidth, label, fitText(value, maxInt(4, width-labelWidth-1)))
}

func compactKeyStatus(host config.Host) string {
	switch host.KeyAuthStatus {
	case config.KeyAuthValidated:
		return "ok"
	case config.KeyAuthFailed:
		return "falha"
	}
	if host.KeySent {
		return "enviada"
	}
	if len(host.IdentityFiles) > 0 {
		return "config"
	}
	return "padrao"
}

func fitText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:width-1]) + "…"
}
