package app

import (
	"os"
	"strings"
	"syscall"

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
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	result, ok := finalModel.(ui.Model)
	if !ok || result.HandoffCommand() == nil {
		return nil
	}
	cmd := result.HandoffCommand()
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = environmentWithTerm(env, result.HandoffTermType())
	return syscall.Exec(cmd.Path, cmd.Args, env)
}

func environmentWithTerm(env []string, termType string) []string {
	if termType == "" || termType == config.TermSystem {
		return env
	}
	result := make([]string, 0, len(env)+1)
	for _, value := range env {
		if !strings.HasPrefix(value, "TERM=") {
			result = append(result, value)
		}
	}
	return append(result, "TERM="+termType)
}
