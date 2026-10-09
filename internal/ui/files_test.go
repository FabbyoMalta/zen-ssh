package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"zenssh/internal/filetransfer"
	"zenssh/internal/style"
)

func fileKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestFileBrowserRequiresExplicitConfirmationAndPreservesPanels(t *testing.T) {
	m := Model{mode: modeFiles, theme: style.New(), files: filesState{
		local:  filePane{dir: "/local", cursor: 1, entries: []filetransfer.Entry{{Name: "file", Kind: "f"}}},
		remote: filePane{dir: "/remote"},
	}}
	updated, cmd := m.updateFiles(fileKey("c"))
	m = updated.(Model)
	if cmd != nil || !m.files.confirm {
		t.Fatal("copy skipped confirmation")
	}
	updated, cmd = m.updateFiles(fileKey("n"))
	m = updated.(Model)
	if cmd != nil || m.files.confirm {
		t.Fatal("cancel started transfer")
	}
	updated, _ = m.updateFiles(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if !m.files.remoteActive || m.files.local.cursor != 1 {
		t.Fatal("panel switch lost cursor")
	}
	updated, _ = m.Update(filesListedMsg{remote: true, err: errors.New("permission denied")})
	m = updated.(Model)
	if m.files.remote.dir != "/remote" || !strings.Contains(m.status, "permission denied") {
		t.Fatal("failed listing changed directory")
	}
	updated, _ = m.updateFiles(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.mode != modeList || m.files.local.dir != "" {
		t.Fatal("browser not closed")
	}
}

func TestFileBrowserDoesNotCopyParentOrSymlink(t *testing.T) {
	for _, cursor := range []int{0, 1} {
		m := Model{mode: modeFiles, theme: style.New(), files: filesState{local: filePane{dir: "/local", cursor: cursor, entries: []filetransfer.Entry{{Name: "link", Kind: "l"}}}, remote: filePane{dir: "/remote"}}}
		updated, cmd := m.updateFiles(fileKey("c"))
		if cmd != nil || updated.(Model).files.confirm {
			t.Fatal("copy offered for parent or symlink")
		}
	}
}
