package style

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	App        lipgloss.Style
	Panel      lipgloss.Style
	Header     lipgloss.Style
	Subtle     lipgloss.Style
	Accent     lipgloss.Style
	Highlight  lipgloss.Style
	Key        lipgloss.Style
	Selected   lipgloss.Style
	Danger     lipgloss.Style
	Success    lipgloss.Style
	Help       lipgloss.Style
	InputLabel lipgloss.Style
}

func New() Theme {
	return Theme{
		App: lipgloss.NewStyle().
			Padding(1, 2),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2),
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("25")).
			Padding(0, 1),
		Subtle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")),
		Accent: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("81")),
		Highlight: lipgloss.NewStyle().
			Foreground(lipgloss.Color("228")),
		Key: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("31")).
			Padding(0, 1),
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Bold(true),
		Danger: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("110")),
		InputLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("117")).
			Bold(true),
	}
}
