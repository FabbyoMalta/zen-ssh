package recording

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecordingCapturesOutputAndPreservesArgumentsAndExitCode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux recorder")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script unavailable")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/does/not/exist")
	for _, exitCode := range []string{"0", "7"} {
		payload := "quotes ' spaces ; $(touch SHOULD_NOT_EXIST)"
		child := exec.Command("sh", "-c", `printf '%s\n' "$1"; printf 'stderr-marker\n' >&2; exit "$2"`, "sh", payload, exitCode)
		child.Dir = t.TempDir()
		cmd, path, err := Prepare(child, "../../prod host")
		if err != nil {
			t.Fatal(err)
		}
		output, runErr := cmd.CombinedOutput()
		if exitCode == "0" && runErr != nil {
			t.Fatalf("recording failed: %v: %s", runErr, output)
		}
		if exitCode == "7" {
			exitErr, ok := runErr.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 7 {
				t.Fatalf("exit code not preserved: %v", runErr)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{payload, "stderr-marker"} {
			if !strings.Contains(string(data), marker) || !strings.Contains(string(output), marker) {
				t.Fatalf("missing %q in terminal output or recording", marker)
			}
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("log permissions: %v, %v", info, err)
		}
		if _, err := os.Stat(filepath.Join(child.Dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
			t.Fatal("argument executed as shell code")
		}
		if filepath.Dir(path) != filepath.Join(os.Getenv("HOME"), ".local", "state", "zenssh", "sessions") {
			t.Fatalf("log outside session directory: %s", path)
		}
	}
}
