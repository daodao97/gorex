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
	t.Setenv("RETTY_DIR", t.TempDir())
	config := t.TempDir()
	for file, text := range map[string]string{
		".zshenv": "export STARTUP_ENV=loaded\nprint -r -- color-env:${NO_COLOR-unset}:${FORCE_COLOR-unset}:${CLICOLOR-unset}:${CLICOLOR_FORCE-unset}\n",
		".zshrc":  "print -r -- startup:$STARTUP_ENV:$ZDOTDIR\nPROMPT=$'resize-prompt\\n> '\nRPROMPT=right-prompt\n",
	} {
		if err := os.WriteFile(filepath.Join(config, file), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SHELL", zsh)
	s, err := newSession("resize-test", CreateOptions{
		Dir: config, Cols: 100, Rows: 24,
		// Explicit color preferences must survive ordinary session creation.
		Env: []string{"ZDOTDIR=" + config, "NO_COLOR=1", "FORCE_COLOR=0", "CLICOLOR=0", "CLICOLOR_FORCE=0"},
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
	if got := s.vt.Text(); strings.Count(got, "color-env:1:0:0:0") != 1 {
		t.Fatalf("explicit preferences lost or startup files ran twice: %q", got)
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

func TestColorLoginShellPreservesUserPreferences(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	t.Setenv("RETTY_DIR", t.TempDir())
	t.Setenv("SHELL", zsh)
	config := t.TempDir()
	if err := os.WriteFile(filepath.Join(config, ".zshenv"), []byte("export NO_COLOR=user-preference\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ".zshrc"), []byte("print -r -- preference:$NO_COLOR\nPROMPT='fixture> '\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := newSession("color-preference", CreateOptions{
		Dir: config, Cols: 80, Rows: 24,
		Env: []string{"ZDOTDIR=" + config, "NO_COLOR=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.kill()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.vt.Text(), "preference:user-preference") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("user color preference lost: %q", s.vt.Text())
}
