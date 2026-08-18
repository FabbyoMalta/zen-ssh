package style

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	App         lipgloss.Style
	Panel       lipgloss.Style
	Header      lipgloss.Style
	Subtle      lipgloss.Style
	Accent      lipgloss.Style
	Highlight   lipgloss.Style
	Key         lipgloss.Style
	Selected    lipgloss.Style
	Danger      lipgloss.Style
	Success     lipgloss.Style
	Help        lipgloss.Style
	InputLabel  lipgloss.Style
	PanelTitle  lipgloss.Style
	TableHeader lipgloss.Style
	BadgeGood   lipgloss.Style
	BadgeWarn   lipgloss.Style
	BadgeBad    lipgloss.Style
	BadgeMuted  lipgloss.Style
}

func New() Theme {
	accent := lipgloss.AdaptiveColor{Light: "25", Dark: "81"}
	text := lipgloss.AdaptiveColor{Light: "235", Dark: "252"}
	muted := lipgloss.AdaptiveColor{Light: "238", Dark: "250"}
	border := lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
	selected := lipgloss.AdaptiveColor{Light: "153", Dark: "24"}
	_, noColor := os.LookupEnv("NO_COLOR")
	if noColor {
		accent, text, muted, border, selected = lipgloss.AdaptiveColor{}, lipgloss.AdaptiveColor{}, lipgloss.AdaptiveColor{}, lipgloss.AdaptiveColor{}, lipgloss.AdaptiveColor{}
	}
	theme := Theme{
		App: lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(text),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent),
		Subtle: lipgloss.NewStyle().
			Foreground(muted),
		Accent: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent),
		Highlight: lipgloss.NewStyle().
			Foreground(lipgloss.Color("228")),
		Key: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("31")).
			Padding(0, 1),
		Selected: lipgloss.NewStyle().
			Foreground(text).
			Background(selected).
			Bold(true),
		Danger: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true),
		Help: lipgloss.NewStyle().
			Foreground(text),
		InputLabel: lipgloss.NewStyle().
			Foreground(accent).
			Bold(true),
		PanelTitle:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		TableHeader: lipgloss.NewStyle().Bold(true).Foreground(muted),
		BadgeGood:   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "42"}),
		BadgeWarn:   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"}),
		BadgeBad:    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"}),
		BadgeMuted:  lipgloss.NewStyle().Foreground(muted),
	}
	if noColor {
		plain := lipgloss.NewStyle()
		bold := lipgloss.NewStyle().Bold(true)
		theme.Header, theme.Accent, theme.Selected = bold, bold, bold
		theme.Danger, theme.Success, theme.InputLabel = bold, bold, bold
		theme.PanelTitle, theme.TableHeader = bold, bold
		theme.Subtle, theme.Highlight, theme.Key, theme.Help = plain, plain, bold, plain
		theme.BadgeGood, theme.BadgeWarn, theme.BadgeBad, theme.BadgeMuted = plain, plain, plain, plain
	}
	return theme
}
