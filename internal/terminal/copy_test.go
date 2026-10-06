package terminal

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCopyModesUseSameSelection(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	term.Feed([]byte("\x1b[2m1.\x1b[0m \x1b[8m**\x1b[28m\x1b[1mfirst\x1b[22m\x1b[8m**\x1b[0m\r\n2. second"))
	tt.Frame()
	mods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		mods = ui.Super
	}
	for _, raw := range []bool{false, true} {
		term.SetCopyRawText(raw)
		want := "1. first\n2. second"
		if raw {
			want = "1. **first**\n2. second"
		}
		x0, y0 := cellCenter(term, 0, 0)
		x1, y1 := cellCenter(term, 8, 1)
		tt.Press(x0-2, y0)
		tt.Move(x1+2, y1)
		tt.Release(x1+2, y1)
		if got := tt.Clipboard(); got != want {
			t.Fatalf("automatic copy (raw=%v) = %q, want %q", raw, got, want)
		}
		tt.SetClipboard("previous")
		tt.Key(mods, ui.KeyC)
		if got := tt.Clipboard(); got != want {
			t.Fatalf("keyboard copy (raw=%v) = %q", raw, got)
		}
		tt.SetClipboard("previous")
		tt.RightClickAt(x0, y0)
		if err := tt.ChooseMenuItem("Copy"); err != nil {
			t.Fatal(err)
		}
		if got := tt.Clipboard(); got != want {
			t.Fatalf("menu copy (raw=%v) = %q", raw, got)
		}
	}
	// A selection containing only concealed characters leaves the clipboard.
	term.SetCopyRawText(false)
	term.Feed([]byte("\x1b[H\x1b[2J\x1b[8mhidden\x1b[0m"))
	tt.Frame()
	tt.SetClipboard("keep")
	tt.Command("selectAll")
	tt.Command("copy")
	if tt.Clipboard() != "keep" {
		t.Fatal("empty visible selection replaced the clipboard")
	}
}
