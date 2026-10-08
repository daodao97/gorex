package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAgentDragSelectsListMarkerWithMouseTracking(t *testing.T) {
	loadLib(t)
	for _, mode := range []int{1000, 1002, 1003} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			conn := newPipe()
			term, err := New(Options{Conn: conn, SelectOnDrag: true})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 640, 200)
			term.Feed([]byte(fmt.Sprintf("\x1b[?1049h\x1b[?%dh\x1b[?1006h", mode)))
			term.Feed([]byte("\x1b[2m4.\x1b[0m 命令起始位置显示结果"))
			tt.Frame()
			// Drag back from the body to the list marker, matching the
			// screenshot's attempted selection. No mouse events reach the
			// Agent, so its Markdown/body-only selection never starts.
			x0, y := cellCenter(term, 0, 0)
			x1, _ := cellCenter(term, 26, 0)
			tt.SetClipboard("previous")
			tt.Press(x1+2, y)
			tt.Move(x0-2, y)
			tt.Release(x0-2, y)
			if got := tt.Clipboard(); got != "4. 命令起始位置显示结果" {
				t.Fatalf("Agent drag copied %q", got)
			}
			if got := conn.take(1); strings.Contains(got, "\x1b[<0;") || strings.Contains(got, "\x1b[<32;") {
				t.Fatalf("selection sent mouse events to Agent: %q", got)
			}
			if line := term.v.lines[0]; !line.sel || line.selA != 0 {
				t.Fatal("the marker was copied without being visibly selected")
			}
		})
	}
}

func TestAgentSelectionKeepsClicksWheelAndOverrides(t *testing.T) {
	loadLib(t)
	conn := newPipe()
	term, err := New(Options{Conn: conn, SelectOnDrag: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	term.Feed([]byte("\x1b[?1002h\x1b[?1006h4. example"))
	tt.Frame()
	x, y := cellCenter(term, 2, 3)
	tt.Press(x, y)
	if got := conn.take(1); got != "" {
		t.Fatalf("Agent saw press before click/drag was decided: %q", got)
	}
	tt.Release(x, y)
	if got := conn.take(18); got != "\x1b[<0;3;4M\x1b[<0;3;4m" {
		t.Fatalf("plain click = %q", got)
	}
	tt.Scroll(x, y, 0, 40)
	if got := conn.take(1); !strings.Contains(got, "\x1b[<65;") {
		t.Fatalf("wheel was not passed to Agent: %q", got)
	}
	tt.ClickAtWith(ui.Alt, x, y)
	if got := conn.take(18); !strings.Contains(got, "\x1b[<8;3;4M") || !strings.Contains(got, "\x1b[<8;3;4m") {
		t.Fatalf("Option override = %q", got)
	}
	tt.SetClipboard("previous")
	x0, y0 := cellCenter(term, 0, 0)
	x1, _ := cellCenter(term, 2, 0)
	// Release at a different position without an intervening motion event.
	tt.Press(x0-2, y0)
	tt.Release(x1+2, y0)
	if got := tt.Clipboard(); got != "4. " && got != "4." {
		t.Fatalf("quick drag lost marker: %q", got)
	}
	if got := conn.take(1); got != "" {
		t.Fatalf("quick drag became an Agent click: %q", got)
	}
	tt.RightClickAt(x0, y0)
	if !strings.Contains(strings.Join(tt.Menu(), ","), "Copy") {
		t.Fatal("Agent selection lost terminal context menu")
	}
}
