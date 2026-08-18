package ui

import (
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Connect, Search, Add, Help, Quit  key.Binding
	Up, Down, Import, Edit, Delete    key.Binding
	Diagnose, ToggleMode, Restore     key.Binding
	GenerateKey, PushKey, ValidateKey key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Connect:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "conectar")),
		Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "buscar")),
		Add:         key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "adicionar")),
		Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "ajuda")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "sair")),
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "subir")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "descer")),
		Import:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "sincronizar")),
		Edit:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "editar")),
		Delete:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remover")),
		Diagnose:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "diagnostico")),
		ToggleMode:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "modo")),
		Restore:     key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "restaurar")),
		GenerateKey: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "gerar chave")),
		PushKey:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "enviar chave")),
		ValidateKey: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "validar chave")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Connect, k.Search, k.Add, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Connect, k.Search},
		{k.Add, k.Edit, k.Delete, k.Import},
		{k.ValidateKey, k.GenerateKey, k.PushKey},
		{k.Diagnose, k.ToggleMode, k.Restore, k.Help, k.Quit},
	}
}
