package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/rex"
	"retty/internal/terminal"
)

func TestMobileNewSessionTabCompletesCommand(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("requires zsh")
	}
	a, _ := newTestApp(t)
	config := t.TempDir()
	command := "retty-completion-fixture"
	if err := os.WriteFile(filepath.Join(config, command), []byte("#!/bin/sh\nprintf 'COMPLETION_EXECUTED:%s\\n' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ".zshrc"), []byte("path=("+config+" $path)\nPROMPT='completion-fixture> '\nbindkey -e\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", zsh)
	t.Setenv("ZDOTDIR", config)
	session, err := a.client.Create(rex.CreateOptions{Dir: config, Cols: 48, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer a.client.Kill(session.ID)
	m := &mobileApp{client: a.client, sizeLock: true}
	m.hello.Version = rex.ProtocolVersion
	defer m.detach()
	m.openSession(session)
	tt := ui.NewTester(m.view, 390, 680)
	waitFor(t, tt, "new mobile shell", func() bool {
		return m.term != nil && strings.Contains(m.term.Text(), "completion-fixture>")
	})
	tt.Click("Terminal")
	tt.Type("retty-completion-f")
	tt.Click("更多")
	tt.Click("Tab")
	waitFor(t, tt, "command completion", func() bool {
		return strings.Contains(m.term.Text(), command)
	})
	// Keep typing after the shell has replaced the prefix with its completion.
	tt.Type("argument")
	tt.Key(0, ui.KeyEnter)
	if tt.Focused("Terminal") || m.keyboardMore {
		t.Fatal("submitted command kept the keyboard open")
	}
	waitFor(t, tt, "completed command execution", func() bool {
		return strings.Contains(m.term.Text(), "COMPLETION_EXECUTED:argument")
	})
}

func TestMobileModifierArmingLockingAndSessionIsolation(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	m := &mobileApp{client: &rex.Client{}, selected: rex.SessionInfo{ID: "fixture"}}
	term, err := terminal.New(terminal.Options{Conn: conn, OptionAsAlt: true, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm, OnSubmit: m.dismissKeyboard})
	if err != nil {
		t.Fatal(err)
	}
	m.term = term
	defer m.detach()
	var current *ui.Context
	tt := ui.NewTester(func(c *ui.Context) { current = c; m.view(c) }, 390, 750)
	take := func(want string) {
		t.Helper()
		peer.SetReadDeadline(time.Now().Add(time.Second))
		b := make([]byte, len(want))
		_, e := io.ReadFull(peer, b)
		if e != nil || string(b) != want {
			t.Fatalf("got %q, want %q: %v", b, want, e)
		}
	}
	tt.Click("Terminal")
	tt.Click("Ctrl")
	tt.Type("c")
	take("\x03")
	if m.keyboardLatch.Active() != 0 || m.keyboardLatch.Locked() != 0 {
		t.Fatal("one-shot Ctrl persisted")
	}
	m.keyboardAction(current.Services(), "lock:ctrl")
	tt.Frame()
	tt.Type("d")
	take("\x04")
	tt.Type("r")
	take("\x12")
	if m.keyboardLatch.Active() != ui.Ctrl || m.keyboardLatch.Locked() != ui.Ctrl {
		t.Fatal("locked Ctrl released")
	}
	tt.Click("Ctrl")
	tt.Type("c")
	take("c")
	tt.Click("Option")
	tt.Type("b")
	take("\x1bb")
	tt.Click("Ctrl")
	tt.Click("Option")
	tt.Type("c")
	take("\x1b\x03")
	tt.Click("更多")
	tt.Click("Shift")
	tt.Type("a")
	take("A")
	// A modifier change must not alter shared action definitions.
	if mobileKeyboardActions[1].Selected || mobileKeyboardActions[1].Locked {
		t.Fatal("modifier state mutated action definitions")
	}
	tt.Click("换行")
	take("\x1b\r")
	if !tt.Focused("Terminal") {
		t.Fatal("inserting a newline dismissed the keyboard")
	}
	tt.Key(0, ui.KeyEnter)
	take("\r")
	if tt.Focused("Terminal") || m.keyboardMore {
		t.Fatal("sending did not dismiss the keyboard and accessory panel")
	}
	tt.Click("Terminal")
	m.keyboardAction(current.Services(), "lock:ctrl")
	tt.Frame()
	tt.Click("收起")
	if m.keyboardLatch.Active() != 0 || m.keyboardLatch.Locked() != 0 {
		t.Fatal("hidden keyboard retained a modifier")
	}
	// Background/recovery boundaries must not carry held control keys into
	// another live session, even if the keyboard remains mounted by UIKit.
	m.client = nil
	m.keyboardAction(current.Services(), "lock:option")
	m.enterBackground()
	if m.keyboardLatch.Active() != 0 || m.keyboardLatch.Locked() != 0 {
		t.Fatal("background retained a modifier")
	}
	m.background = false
	m.keyboardAction(current.Services(), "lock:ctrl")
	m.pauseConnection()
	if m.keyboardLatch.Active() != 0 || m.keyboardLatch.Locked() != 0 {
		t.Fatal("recovery retained a modifier")
	}
}
