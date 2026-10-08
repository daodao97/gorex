package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRemoteInputCaretMatchesPaintedCursorOnResize(t *testing.T) {
	loadLib(t)
	for _, fit := range []bool{false, true} {
		term, err := New(Options{Conn: newPipe(), FixedCols: 100, FixedRows: 60, ReflowView: true, FitToView: fit, Font: Font{Size: 13}, Theme: DarkTheme(), ActiveCursor: true})
		if err != nil {
			t.Fatal(err)
		}
		term.Feed([]byte("\x1b[?1049h\x1b[2J\x1b[58;1H> 输入内容\x1b[58;9H\x1b[2 q"))
		var element *ui.Element
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Children(func() {
				ui.Box(c).Height(54).FillWidth()
				element = View(c, term).Fill().AutoFocus()
			})
		}, 393, 680)
		check := func() {
			t.Helper()
			v, b := term.v, element.Bounds()
			r, _, _, _ := v.cursorRect(b.X+float32(v.ox)/v.scale, b.Y+float32(v.oy)/v.scale, float32(v.cellW)/v.scale, float32(v.cellH)/v.scale)
			caret, active := tt.TextCaret()
			if !active || r.H == 0 || caret.X != r.X || caret.Y != r.Y || caret.H != r.H {
				t.Fatalf("fit=%v input and painted cursor disagree: caret=%+v painted=%+v", fit, caret, r)
			}
		}
		check() // The first frame must already have valid font/grid geometry.
		for _, size := range [][2]int{{393, 370}, {393, 274}, {780, 190}, {393, 680}} {
			tt.SetSize(size[0], size[1])
			check() // No extra frame to catch up after keyboard/panel/rotation changes.
		}
		term.Feed([]byte("\x1b[58;40H新位置\x1b[58;41H"))
		tt.Frame()
		check() // A cursor on the tail of a wide character shares its painted anchor.
		if cols, rows := term.Size(); cols != 100 || rows != 60 {
			t.Fatalf("phone moved the desktop grid: %dx%d", cols, rows)
		}
		term.Close()
	}
}
