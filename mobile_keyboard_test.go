package main

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gorex/internal/rex"
	"gorex/internal/terminal"
)

func TestMobileModifierArmingLockingAndSessionIsolation(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	term, err := terminal.New(terminal.Options{Conn: conn, OptionAsAlt: true, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm})
	if err != nil {
		t.Fatal(err)
	}
	m := &mobileApp{client: &rex.Client{}, term: term, selected: rex.SessionInfo{ID: "fixture"}}
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
	tt.Click("键盘")
	tt.Click("Ctrl")
	tt.Type("c")
	take("\x03")
	if m.keyboardModifiers != 0 || m.keyboardLocked != 0 {
		t.Fatal("one-shot Ctrl persisted")
	}
	m.keyboardAction(current, "lock:ctrl")
	tt.Frame()
	tt.Type("d")
	take("\x04")
	tt.Type("r")
	take("\x12")
	if m.keyboardModifiers != ui.Ctrl || m.keyboardLocked != ui.Ctrl {
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
	m.keyboardAction(current, "lock:ctrl")
	tt.Frame()
	tt.Click("收起")
	if m.keyboardModifiers != 0 || m.keyboardLocked != 0 {
		t.Fatal("hidden keyboard retained a modifier")
	}
	// Background/recovery boundaries must not carry held control keys into
	// another live session, even if the keyboard remains mounted by UIKit.
	m.client = nil
	m.keyboardAction(current, "lock:option")
	m.enterBackground()
	if m.keyboardModifiers != 0 || m.keyboardLocked != 0 {
		t.Fatal("background retained a modifier")
	}
	m.background = false
	m.keyboardAction(current, "lock:ctrl")
	m.pauseConnection()
	if m.keyboardModifiers != 0 || m.keyboardLocked != 0 {
		t.Fatal("recovery retained a modifier")
	}
}
