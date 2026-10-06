package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRowBackgroundReachesRightEdge(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe(), Theme: LightTheme(), NoBlink: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 301, 100)
	for _, scale := range []float32{1, 2} {
		tt.SetScale(scale)
		sawRemainder := false
		for _, width := range []int{301, 304} {
			tt.SetSize(width, 100)
			// The first row has background through its last column; the
			// second has only a short colored run at its start.
			term.Feed([]byte("\x1b[?25l\x1b[1;1H\x1b[48;2;80;90;100m\x1b[2K\x1b[0m\x1b[2;1H\x1b[2K\x1b[48;2;80;90;100m   \x1b[0m"))
			tt.Frame()
			v, img := term.v, tt.Image()
			right := img.Bounds().Max.X - 1
			if right >= v.ox+v.cols*v.cellW {
				sawRemainder = true
			}
			for row, want := range []ui.Color{ui.RGB(80, 90, 100), LightTheme().Background} {
				got := img.RGBAAt(right, v.oy+row*v.cellH+v.cellH/2)
				if got.R != want.R || got.G != want.G || got.B != want.B {
					t.Fatalf("width %d, scale %g, row %d: right edge = %v, want %v", width, scale, row, got, want)
				}
			}
			if col, _ := v.cellAt(float32(width)-0.1, float32(v.oy+v.cellH/2)/scale); col != v.cols-1 {
				t.Fatalf("right-edge mouse position maps to column %d, want %d", col, v.cols-1)
			}
		}
		if !sawRemainder {
			t.Fatalf("scale %g did not exercise a partial character column", scale)
		}
	}
}
