package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"zenssh/internal/config"
	"zenssh/internal/ui"
)

func Run() error {
	store, err := config.NewStore()
	if err != nil {
		return err
	}
	if err := store.Ensure(); err != nil {
		return err
	}
	model, err := ui.NewModel(store)
	if err != nil {
		return err
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
