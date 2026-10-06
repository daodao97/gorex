package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestControlTabSwitchesTabs(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	a, tt := newTestApp(t)
	a.newTab("/tmp")
	a.newTab("/tmp")
	for _, compact := range []bool{false, true} {
		prefs.CompactMode = compact
		a.selectTab(0)
		tt.Frame()
		for _, step := range []struct {
			mods ui.Modifiers
			key  ui.Key
			want int
		}{
			{ui.Ctrl, ui.KeyTab, 1},
			{ui.Ctrl, ui.KeyTab, 2},
			{ui.Ctrl, ui.KeyTab, 0},
			{ui.Ctrl | ui.Shift, ui.KeyTab, 2},
			{ui.Ctrl | ui.Shift, ui.KeyTab, 1},
			{ui.Ctrl | ui.Shift, ui.KeyTab, 0},
			{ui.Cmd | ui.Shift, ui.KeyBracketRight, 1},
			{ui.Cmd | ui.Shift, ui.KeyBracketLeft, 0},
		} {
			tt.Key(step.mods, step.key)
			if a.active != step.want || a.focusReq != nil || !tt.Focused("Terminal") {
				t.Fatalf("compact %v, modifiers %v: active %d, want %d; pending focus %v, terminal focused %v", compact, step.mods, a.active, step.want, a.focusReq != nil, tt.Focused("Terminal"))
			}
		}
	}
}
