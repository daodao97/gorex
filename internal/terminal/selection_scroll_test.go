package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSelectionDragScrollsAcrossPagesAtViewportEdges(t *testing.T) {
	loadLib(t)
	for _, scale := range []float32{1, 2} {
		for _, tracking := range []bool{false, true} {
			t.Run(fmt.Sprintf("scale%g-tracking%v", scale, tracking), func(t *testing.T) {
				conn := newPipe()
				term, err := New(Options{Conn: conn, SelectOnDrag: true, NoBlink: true})
				if err != nil {
					t.Fatal(err)
				}
				defer term.Close()
				for i := range 100 {
					term.Feed([]byte(fmt.Sprintf("line %03d 中文\r\n", i)))
				}
				if tracking {
					term.Feed([]byte("\x1b[?1002h\x1b[?1006h"))
				}
				tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 500, 300)
				tt.SetScale(scale)
				tt.Frame()
				v := term.v
				term.mu.Lock()
				v.screen().ScrollBy(-40)
				term.mu.Unlock()
				tt.Frame()
				before := v.screen().Scrollbar().Offset
				x, y := cellCenter(term, 2, v.rows/2)
				v.input(ui.InputEvent{Kind: ui.InputPointerDown, X: x, Y: y})
				bottom := float32(v.oy+v.rows*v.cellH-1) / v.scale
				v.input(ui.InputEvent{Kind: ui.InputPointerMove, X: x, Y: bottom})
				for range v.rows + 5 {
					tt.Frame()
				}
				down := v.screen().Scrollbar().Offset
				if down <= before+uint64(v.rows) {
					t.Fatalf("drag did not scroll down beyond one page: %d -> %d, rows %d", before, down, v.rows)
				}
				selected, ok := term.SelectedText()
				if !ok || strings.Count(selected, "\n") < v.rows {
					t.Fatalf("selection did not extend into newly visible rows: %q", selected)
				}
				v.input(ui.InputEvent{Kind: ui.InputPointerMove, X: x, Y: y})
				for range 3 {
					tt.Frame()
				}
				if v.ticking || v.screen().Scrollbar().Offset != down {
					t.Fatal("moving back inside did not pause scrolling")
				}
				top := float32(v.oy+1) / v.scale
				v.input(ui.InputEvent{Kind: ui.InputPointerMove, X: x, Y: top})
				for range 2*v.rows + 10 {
					tt.Frame()
				}
				up := v.screen().Scrollbar().Offset
				if up >= before {
					t.Fatalf("drag did not reverse and scroll up: %d -> %d -> %d", before, down, up)
				}
				v.input(ui.InputEvent{Kind: ui.InputPointerUp, X: x, Y: top})
				tt.Frame()
				stopped := v.screen().Scrollbar().Offset
				for range 3 {
					tt.Frame()
				}
				if v.ticking || v.screen().Scrollbar().Offset != stopped || tt.Clipboard() == "" {
					t.Fatal("release did not stop scrolling and copy the expanded selection")
				}
				if data := conn.take(1); data != "" {
					t.Fatalf("local selection reached the mouse-aware program: %q", data)
				}
			})
		}
	}
}
