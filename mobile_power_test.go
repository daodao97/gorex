package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"retty/internal/terminal"
)

func TestMobileSessionDisplayAwakeLifecycle(t *testing.T) {
	acquired, released := 0, 0
	m := &mobileApp{screenActive: true}
	m.keepScreenAwake = func(reason string, display bool) func() {
		if reason == "" || !display {
			t.Fatal("session requested system sleep inhibition without a display lease")
		}
		acquired++
		return func() { released++ }
	}
	m.invalidate()
	if acquired != 0 {
		t.Fatal("session list kept screen awake")
	}
	term, err := terminal.New(terminal.Options{FixedCols: 40, FixedRows: 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	m.term = term
	m.invalidate()
	m.invalidate()
	if acquired != 1 || released != 0 {
		t.Fatal("repeated rendering acquired multiple display leases")
	}
	m.screenActive = false
	m.invalidate()
	m.invalidate()
	if released != 1 {
		t.Fatal("inactive app retained its display lease")
	}
	m.screenActive = true
	m.invalidate()
	m.enterBackground()
	if acquired != 2 || released != 2 {
		t.Fatal("background app kept the screen awake")
	}
	m.background = false
	m.invalidate()
	m.detach()
	if acquired != 3 || released != 3 || m.releaseScreenAwake != nil {
		t.Fatal("leaving session details retained the display lease")
	}
	m.screenActive = false
	m.invalidate()
	m.screenActive = true
	m.invalidate()
	if acquired != 3 {
		t.Fatal("foregrounding the session list kept screen awake")
	}
}

func TestMobileSessionBackReleasesDisplayAwake(t *testing.T) {
	term, err := terminal.New(terminal.Options{FixedCols: 40, FixedRows: 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	acquired, released := 0, 0
	m := &mobileApp{screenActive: true, term: term}
	m.keepScreenAwake = func(string, bool) func() {
		acquired++
		return func() { released++ }
	}
	tt := ui.NewTester(m.view, 390, 750)
	if acquired != 1 {
		t.Fatal("session details did not acquire a display lease")
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if m.term != nil || acquired != 1 || released != 1 {
		t.Fatal("back navigation retained the display lease")
	}
}
