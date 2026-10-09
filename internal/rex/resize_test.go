//go:build !gorex_cli && (darwin || linux)

package rex

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

func TestResyncScreenDoesNotExposeClearFrame(t *testing.T) {
	for _, alternate := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "alternate"}[alternate], func(t *testing.T) {
			server, window := testTerminal(t, 100, 10), testTerminal(t, 100, 10)
			if alternate {
				server.Feed([]byte("\x1b[?1049h"))
				window.Feed([]byte("\x1b[?1049h"))
			}
			server.Feed([]byte("\x1b[?25l\x1b[Hnew screen"))
			window.Feed([]byte("\x1b[?25l\x1b[Hold screen"))
			tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, window).Fill() }, 800, 200)
			tt.Frame()
			before := append([]byte(nil), tt.Image().Pix...)
			a := &attached{out: make(chan []byte, 1)}
			s := &session{vt: server, shell: "zsh", clients: map[*attached]struct{}{a: {}}}
			s.resyncScreen()
			msg := <-a.out
			// A socket read can end immediately after the clear-screen escape,
			// before any snapshot text arrives. Render at that exact boundary.
			clearEnd := bytes.Index(msg, []byte("\x1b[2J")) + len("\x1b[2J")
			if clearEnd < len("\x1b[2J") {
				t.Fatal("missing screen replacement")
			}
			window.Feed(msg[:clearEnd])
			tt.Frame()
			if !bytes.Equal(before, tt.Image().Pix) {
				t.Fatal("resync exposed a cleared frame before its snapshot arrived")
			}
			// Check every remaining byte boundary too: the snapshot itself
			// must not release the hold before its final commit escape.
			for i := clearEnd; i < len(msg); i++ {
				window.Feed(msg[i : i+1])
				tt.Frame()
				if i < len(msg)-1 && !bytes.Equal(before, tt.Image().Pix) {
					t.Fatalf("partial snapshot became visible at byte %d of %d", i, len(msg))
				}
			}
			if bytes.Equal(before, tt.Image().Pix) {
				t.Fatal("replacement screen was never displayed")
			}
			if got, want := window.Text(), server.Text(); got != want {
				t.Fatalf("resynced content %q, want %q", got, want)
			}
		})
	}
}

func testTerminal(t *testing.T, cols, rows int) *terminal.Terminal {
	t.Helper()
	pr, pw := io.Pipe()
	vt, err := terminal.New(terminal.Options{Conn: discard{pr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pw.Close(); vt.Close() })
	vt.Resize(cols, rows)
	vt.Feed([]byte(promptRedraw))
	return vt
}

func TestMarkedPromptResize(t *testing.T) {
	vt := testTerminal(t, 100, 20)
	s := &session{vt: vt, cols: 100, rows: 20, exited: true}
	vt.Feed([]byte("history\r\n"))
	prompt := []byte("\x1b]133;A\a\r\n\x1b[Aresize-prompt\x1b[90Gclock\r\n> pending-command\x1b]133;B\a")
	s.prompt.feed(prompt)
	vt.Feed(prompt)
	for _, cols := range []int{80, 100, 80, 100, 80} {
		s.resize(cols, 20)
		// zsh redraws the prompt above its input row on SIGWINCH.
		vt.Feed([]byte("\r\x1b[A"))
		redraw := []byte("\x1b]133;A\aresize-prompt\r\n> pending-command\x1b]133;B\a")
		s.prompt.feed(redraw)
		vt.Feed(redraw)
		if got := vt.Text(); got != "history\nresize-prompt\n> pending-command" {
			t.Fatalf("at %d columns: %q", cols, got)
		}
	}
}

func TestResizePreservesCommandOutput(t *testing.T) {
	vt := testTerminal(t, 100, 20)
	s := &session{vt: vt, cols: 100, rows: 20, exited: true}
	text := strings.Repeat("long output ", 20)
	vt.Feed([]byte(text))
	s.resize(40, 10)
	s.resize(120, 30)
	if got := vt.Text(); strings.TrimSpace(got) != strings.TrimSpace(text) {
		t.Fatalf("output changed on resize: %q", got)
	}
}

func TestResyncScreen(t *testing.T) {
	for _, alternate := range []bool{false, true} {
		name := "primary"
		if alternate {
			name = "alternate"
		}
		t.Run(name, func(t *testing.T) {
			server := testTerminal(t, 100, 10)
			window := testTerminal(t, 100, 10)
			server.Feed([]byte(strings.Repeat("saved output\r\n", 20)))
			window.Feed([]byte("old prompt\r\nmisplaced prompt\r\n"))
			if alternate {
				server.Feed([]byte("\x1b[?1049h\x1b[Hfull screen"))
				window.Feed([]byte("\x1b[?1049h\x1b[Hstale full screen"))
			}
			a := &attached{out: make(chan []byte, 1)}
			s := &session{vt: server, shell: "zsh", clients: map[*attached]struct{}{a: {}}}
			s.resyncScreen()
			select {
			case msg := <-a.out:
				window.Feed(msg)
			default:
				t.Fatal("no screen sent")
			}
			if got, want := window.Text(), server.Text(); got != want {
				t.Fatalf("window %q; server %q", got, want)
			}
			s.clear()
			window.Feed(<-a.out)
			s.resyncScreen()
			window.Feed(<-a.out)
			if got := window.Text(); got != "" {
				t.Fatalf("cleared text returned on resync: %q", got)
			}
		})
	}
}

func TestPromptTracker(t *testing.T) {
	var p promptTracker
	for _, part := range []string{"\x1b]13", "3;A;redraw=1\x1b", "\\prompt\x1b]133;B\a"} {
		p.feed([]byte(part))
	}
	if !p.active {
		t.Fatal("fragmented prompt was missed")
	}
	p.feed([]byte("\x1b]2;title\a"))
	if !p.active {
		t.Fatal("title changed prompt state")
	}
	p.feed([]byte("\x1b]133;C\a"))
	if p.active {
		t.Fatal("command still marked as a prompt")
	}
}
