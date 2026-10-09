package ui

import (
	"errors"
	"os"
	"path/filepath"
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

func TestRemoteNavigationRunsInBackgroundAndCapturesBanners(t *testing.T) {
	ssh := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nprintf 'login banner\\n'\nprintf 'ssh banner\\n' >&2\nprintf '\\0ZENSSH-LIST-V1\\0/remote/child\\0f\\0003\\0file\\0\\0ZENSSH-LIST-END\\0'\n"
	if err := os.WriteFile(ssh, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := Model{mode: modeFiles, theme: style.New(), files: filesState{remoteActive: true, client: filetransfer.Client{SSHPath: ssh, Target: "server"}, remote: filePane{dir: "/remote", cursor: 1, entries: []filetransfer.Entry{{Name: "child", Kind: "d"}}}}}
	updated, cmd := m.updateFiles(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.files.loading || m.files.remote.dir != "/remote" {
		t.Fatal("navigation did not preserve the visible directory while loading")
	}
	msg, ok := cmd().(filesListedMsg)
	if !ok {
		t.Fatal("navigation suspended the terminal instead of returning a background listing")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.files.loading || m.files.remote.dir != "/remote/child" || len(m.files.remote.entries) != 1 {
		t.Fatalf("panel not updated: %#v", m.files.remote)
	}
}

func TestRemoteNavigationIgnoresOldResponsesAndBlocksDuplicateRequests(t *testing.T) {
	m := Model{mode: modeFiles, theme: style.New(), fileRequest: 3, files: filesState{loading: true, remoteActive: true, remote: filePane{dir: "/current"}}}
	updated, cmd := m.updateFiles(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !updated.(Model).files.loading {
		t.Fatal("duplicate remote navigation started")
	}
	updated, _ = m.Update(filesListedMsg{remote: true, request: 2, dir: "/old"})
	m = updated.(Model)
	if m.files.remote.dir != "/current" || !m.files.loading {
		t.Fatal("old response replaced current panel")
	}
	updated, _ = m.Update(filesListedMsg{remote: true, request: 3, err: errors.New("authentication failed")})
	m = updated.(Model)
	if m.files.loading || m.files.remote.dir != "/current" || !strings.Contains(m.status, "authentication failed") {
		t.Fatal("failed request lost current directory or remained loading")
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
