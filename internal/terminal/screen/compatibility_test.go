//go:build !retty_cli

package screen

import (
	"io"
	"retty/internal/terminal"
	"strings"
	"testing"
)

func TestScreenMatchesDesktopEmulatorOnResizeAndSnapshot(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	gui, err := terminal.New(terminal.Options{Conn: discard{pr}, Scrollback: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	headless, err := New(80, 24, 8<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer headless.Close()
	for _, data := range []string{
		strings.Repeat("history 中文 👩‍💻\r\n", 30),
		"\x1b[?1049h\x1b[H\x1b[2J\x1b[32magent screen\x1b[0m\x1b[?2004h",
		"\x1b[?1049l\x1b]133;A;redraw=1\a\r\n> pending-command\x1b]133;B\a",
	} {
		gui.Feed([]byte(data))
		headless.Feed([]byte(data))
		for _, size := range [][2]int{{40, 12}, {120, 30}, {80, 24}} {
			gui.Resize(size[0], size[1])
			headless.Resize(size[0], size[1])
			if string(gui.Snapshot()) != string(headless.Snapshot()) {
				t.Fatalf("snapshot differs at %dx%d", size[0], size[1])
			}
		}
	}
}

type discard struct{ *io.PipeReader }

func (discard) Write(p []byte) (int, error) { return len(p), nil }
