package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAgentPaneNativeDragIncludesListMarker(t *testing.T) {
	previous := prefs
	prefs = settings{FontSize: defaultFontSize}
	t.Cleanup(func() { prefs = previous })
	a, tt := newStaticTestApp(t)
	p := a.tab().Focus
	for _, compact := range []bool{false, true} {
		prefs.CompactMode = compact
		for _, agent := range []string{"codex", "claude"} {
			p.info.Program, p.info.Idle, p.info.Args = agent, false, nil
			p.term.Feed([]byte("\x1b[?1049h\x1b[?1002h\x1b[?1006h\x1b[H\x1b[2J4. example"))
			tt.Frame()
			r, ok := tt.Find("Terminal")
			if !ok {
				t.Fatal("missing terminal")
			}
			cols, rows := p.term.Size()
			cw, ch := r.W/float32(cols), r.H/float32(rows)
			y, left, right := r.Y+ch/2, r.X+cw/4, r.X+9.75*cw
			tt.SetClipboard("previous")
			tt.Press(right, y)
			tt.Move(left, y)
			tt.Release(left, y)
			if got := tt.Clipboard(); got != "4. example" {
				t.Fatalf("%s compact=%v selection = %q", agent, compact, got)
			}
			// Copy commands continue to use the same terminal selection.
			tt.SetClipboard("previous")
			tt.Key(ui.Cmd, ui.KeyC)
			if tt.Clipboard() != "4. example" {
				t.Fatal("keyboard copy lost the Agent's selected marker")
			}
		}
	}
	// Editors keep their existing mouse protocol instead of acquiring the
	// Agent-specific drag override.
	p.info.Program = "vim"
	tt.Frame()
	r, _ := tt.Find("Terminal")
	tt.SetClipboard("keep")
	tt.Press(r.X+15, r.Y+10)
	tt.Move(r.X+80, r.Y+10)
	tt.Release(r.X+80, r.Y+10)
	if tt.Clipboard() != "keep" {
		t.Fatal("editor mouse drag was intercepted as text selection")
	}
}
