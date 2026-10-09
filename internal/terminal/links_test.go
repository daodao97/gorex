package terminal

import (
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
)

func TestFileAndURLMatches(t *testing.T) {
	for _, tc := range []struct {
		text, click string
		want        Link
	}{
		{"error: src/main.go:42:5: failed", "main", Link{Path: "src/main.go", Line: 42, Column: 5}},
		{"at (./src/main.ts(12,3))", "main", Link{Path: "./src/main.ts", Line: 12, Column: 3}},
		{"see ~/work/中文.go#L8C2", "中文", Link{Path: "~/work/中文.go", Line: 8, Column: 2}},
		{`File "/tmp/my project/中文.go":13:2`, "project", Link{Path: "/tmp/my project/中文.go", Line: 13, Column: 2}},
		{"(../README.md),", "README", Link{Path: "../README.md"}},
		{"Makefile:4", "Make", Link{Path: "Makefile", Line: 4}},
		{"目录 /tmp/project/", "project", Link{Path: "/tmp/project/"}},
		{"🙂 中文 https://example.com/a_(b)?q=1.", "example", Link{URL: "https://example.com/a_(b)?q=1"}},
		{"see file:///tmp/a%20b.go:4", "a%20", Link{URL: "file:///tmp/a%20b.go:4"}},
		{"plain words without a link", "words", Link{}},
		{"main.go:0", "main", Link{}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			text := []rune(tc.text)
			cols := make([]int, len(text))
			col := 0
			for i, r := range text {
				cols[i] = col
				col++
				if r > 0x2fff {
					col++
				}
			}
			i := len([]rune(tc.text[:strings.Index(tc.text, tc.click)]))
			if got := matchLink(text, cols, cols[i]).Link; got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCommandClickWrappedLinksAndMouseTracking(t *testing.T) {
	loadLib(t)
	conn := newPipe()
	var opened []Link
	var term *Terminal
	var err error
	term, err = New(Options{Conn: conn, OnOpenLink: func(link Link) {
		// Calling the emulator here also verifies the callback runs unlocked.
		_ = term.Text()
		opened = append(opened, link)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 220, 180)
	v := term.v
	point := func(col, row int) (float32, float32) {
		return float32(v.ox+col*v.cellW+v.cellW/2) / v.scale, float32(v.oy+row*v.cellH+v.cellH/2) / v.scale
	}
	url := "https://example.com/" + strings.Repeat("a", v.cols+10)
	term.Feed([]byte("\x1b[?1000h\x1b[?1006h" + url))
	tt.Frame()
	x, y := point(2, 1)
	tt.SetClipboard("keep clipboard")
	tt.ClickAtWith(ui.Cmd, x, y)
	if len(opened) != 1 || opened[0].URL != url {
		t.Fatalf("wrapped URL = %+v", opened)
	}
	if sent := conn.take(0); sent != "" {
		t.Fatalf("Command click reached program: %q", sent)
	}
	if tt.Clipboard() != "keep clipboard" {
		t.Fatal("link click replaced clipboard")
	}
	if tt.Cursor() != ui.CursorPointer || !v.hover.valid() {
		t.Fatal("Command hover not discoverable")
	}
	// Hard newlines must not concatenate unrelated text into a URL.
	term.Feed([]byte("\x1b[H\x1b[2Jhttps://example.com/\r\nsrc/main.go:17:3"))
	tt.Frame()
	x, y = point(5, 1)
	tt.ClickAtWith(ui.Cmd, x, y)
	if len(opened) != 2 || opened[1] != (Link{Path: "src/main.go", Line: 17, Column: 3}) {
		t.Fatalf("file location = %+v", opened)
	}
	// Ordinary pointer clicks still reach mouse-aware terminal programs.
	x, y = point(1, 0)
	tt.ClickAt(x, y)
	if sent := conn.take(1); !strings.Contains(sent, "\x1b[<0;") {
		t.Fatalf("normal mouse reporting broken: %q", sent)
	}
	// OSC 8 display labels retain their URL rather than becoming file paths.
	term.Feed([]byte("\x1b[H\x1b[2J\x1b]8;;https://example.com/hidden\x07label\x1b]8;;\x07"))
	tt.Frame()
	x, y = point(2, 0)
	tt.ClickAtWith(ui.Cmd, x, y)
	if len(opened) != 3 || opened[2].URL != "https://example.com/hidden" {
		t.Fatalf("OSC 8 = %+v", opened)
	}
	// Link opening works on history rows too.
	term.Feed([]byte("\r\n" + strings.Repeat("history\r\n", 50)))
	term.mu.Lock()
	term.term.ScrollToTop()
	term.mu.Unlock()
	tt.Frame()
	x, y = point(2, 0)
	tt.ClickAtWith(ui.Cmd, x, y)
	if len(opened) != 4 || opened[3].URL != "https://example.com/hidden" {
		t.Fatalf("history link = %+v", opened)
	}
}

func TestURLAt(t *testing.T) {
	row := "see https://example.com/a_(b)?q=1. and (http://x.org/y), mailto:me@x.org!"
	text := []rune(row)
	cols := make([]int, len(text))
	for i := range cols {
		cols[i] = i
	}
	for col, want := range map[int]string{
		0:  "",
		4:  "https://example.com/a_(b)?q=1",
		20: "https://example.com/a_(b)?q=1",
		33: "", // the period after it
		40: "http://x.org/y",
		58: "mailto:me@x.org",
	} {
		if got := urlAt(text, cols, col); got != want {
			t.Errorf("urlAt(%d) = %q, want %q", col, got, want)
		}
	}
}

func TestCommandClickTUIPaintedFileContinuations(t *testing.T) {
	loadLib(t)
	var opened []Link
	term, err := New(Options{Conn: newPipe(), OnOpenLink: func(link Link) { opened = append(opened, link) }})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 960, 240)
	v := term.v
	click := func(col, row int) Link {
		t.Helper()
		x := float32(v.ox+col*v.cellW+v.cellW/2) / v.scale
		y := float32(v.oy+row*v.cellH+v.cellH/2) / v.scale
		opened = nil
		tt.ClickAtWith(ui.Cmd, x, y)
		if len(opened) != 1 {
			t.Fatalf("click (%d,%d): opened %v", col, row, opened)
		}
		return opened[0]
	}
	for _, prefix := range []string{"file:///tmp/", "/tmp/", "./images/"} {
		t.Run(prefix, func(t *testing.T) {
			first := prefix + strings.Repeat("a", v.cols-len(prefix)-len("Saved to: ")-len("/exec-")-3) + "/exec-"
			last := "0e64fee3-e2b3-4f2a-99c7-7619d408d347.png"
			term.Feed([]byte("\x1b[H\x1b[2JSaved to: " + first + "\r\n  " + last))
			tt.Frame()
			want := Link{Path: first + last}
			if strings.HasPrefix(prefix, "file://") {
				want = Link{URL: first + last}
			}
			for _, cell := range [][2]int{{12, 0}, {v.cols - 5, 0}, {4, 1}, {len(last), 1}} {
				if got := click(cell[0], cell[1]); got != want {
					t.Fatalf("got %+v, want %+v", got, want)
				}
			}
		})
	}
	// A TUI can use cursor-addressed painting, without any wrap flag/newline.
	first := "/tmp/" + strings.Repeat("a", v.cols-8) + "-"
	middle := strings.Repeat("b", v.cols-4) + "-"
	term.Feed([]byte("\x1b[H\x1b[2J" + first + "\x1b[2;1H" + middle + "\x1b[3;1Hfinal.png:12:3"))
	tt.Frame()
	want := Link{Path: first + middle + "final.png", Line: 12, Column: 3}
	for row := range 3 {
		if got := click(4, row); got != want {
			t.Fatalf("three-row path = %+v, want %+v", got, want)
		}
	}
	for _, tc := range []struct {
		first, second string
		want          Link
	}{
		{"https://example.com/" + strings.Repeat("a", v.cols-23), "other.png", Link{Path: "other.png"}},
		{"/tmp/" + strings.Repeat("a", v.cols-14) + ".png", "other.png", Link{Path: "other.png"}},
		{first, "/tmp/other.png", Link{Path: "/tmp/other.png"}},
		{"file:///tmp/short-", "other.png", Link{Path: "other.png"}},
	} {
		term.Feed([]byte("\x1b[H\x1b[2J" + tc.first + "\r\n" + tc.second))
		tt.Frame()
		if got := click(4, 1); got != tc.want {
			t.Fatalf("unrelated rows %q/%q became %+v", tc.first, tc.second, got)
		}
	}
}
