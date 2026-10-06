//go:build darwin || linux

package rex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestZshPromptResize(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	t.Setenv("GOREX_DIR", t.TempDir())
	config := t.TempDir()
	for file, text := range map[string]string{
		".zshenv": "export STARTUP_ENV=loaded\n",
		".zshrc":  "print -r -- startup:$STARTUP_ENV:$ZDOTDIR\nPROMPT=$'resize-prompt\\n> '\nRPROMPT=right-prompt\n",
	} {
		if err := os.WriteFile(filepath.Join(config, file), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := newSession("resize-test", CreateOptions{
		Command: []string{zsh, "-i"}, Dir: config, Cols: 100, Rows: 24,
		Env: []string{"ZDOTDIR=" + config},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.kill()
	wait := func(what string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %q", what, s.vt.Text())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("prompt", func() bool { return strings.Contains(s.vt.Text(), "right-prompt") })
	if got := s.vt.Text(); !strings.Contains(got, "startup:loaded:"+config) {
		t.Fatalf("user startup files or ZDOTDIR changed: %q", got)
	}
	s.input([]byte("echo pending-command"))
	wait("pending input", func() bool { return strings.Contains(s.vt.Text(), "echo pending-command") })
	for _, cols := range []int{40, 120, 40, 100} {
		s.resize(cols, 24)
		wait("redrawn input", func() bool {
			text := s.vt.Text()
			return strings.Contains(text, "resize-prompt") && strings.Contains(text, "echo pending-command")
		})
		if got := s.vt.Text(); strings.Count(got, "resize-prompt") != 1 {
			t.Fatalf("prompt duplicated at %d columns: %q", cols, got)
		}
	}
}
