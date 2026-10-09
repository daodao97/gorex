package terminal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/terminal/internal/pty"
	"retty/internal/terminal/internal/vt"
)

// Research-only replay of a disposable, single-process OpenCode capture.
// This establishes neither live scheduling correctness nor support for other
// applications; no production path enables dual-size redraw from this test.
func TestDualSizeRedrawCaptureExperiment(t *testing.T) {
	dir := os.Getenv("RETTY_DUAL_RENDER_CAPTURE")
	if dir == "" {
		t.Skip("requires owned OpenCode dual-size capture")
	}
	loadLib(t)
	var manifest []struct {
		File                     string
		Cols, Rows, Starts, Ends int
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest) == 0 {
		t.Fatal("empty capture")
	}
	var summary struct{ Mode string }
	if data, err := os.ReadFile(filepath.Join(dir, "summary.json")); err == nil {
		if err := json.Unmarshal(data, &summary); err != nil {
			t.Fatal(err)
		}
	}
	streaming, control := strings.HasPrefix(summary.Mode, "stream"), summary.Mode == "stream-control"
	source, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	phone, err := New(Options{Conn: newPipe(), FixedCols: 48, FixedRows: 35})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	desktop, err := New(Options{Conn: newPipe(), FixedCols: 108, FixedRows: 58})
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	phoneUI := ui.NewTester(func(c *ui.Context) { View(c, phone).Fill() }, 393, 680)
	desktopUI := ui.NewTester(func(c *ui.Context) { View(c, desktop).Fill() }, 1000, 920)
	phoneUI.SetScale(3)
	desktopUI.SetScale(2)
	draft, phoneFrames, desktopFrames, held := false, 0, 0, 0
	for _, record := range manifest {
		if filepath.Base(record.File) != record.File {
			t.Fatal("capture filename must be local")
		}
		if !((record.Cols == 48 && record.Rows == 35) || (record.Cols == 108 && record.Rows == 58)) {
			t.Fatal("unexpected experiment geometry")
		}
		data, err := os.ReadFile(filepath.Join(dir, record.File))
		if err != nil {
			t.Fatal(err)
		}
		if record.Starts != bytes.Count(data, []byte("\x1b[?2026h")) || record.Ends != bytes.Count(data, []byte("\x1b[?2026l")) {
			t.Fatalf("capture markers disagree with manifest: %s", record.File)
		}
		source.Resize(record.Cols, record.Rows)
		source.Feed(data)
		if !source.term.AltScreen() {
			t.Fatal("fixture left alternate screen")
		}
		if source.term.Mode(vt.ModeSyncOutput) {
			held++
			continue // never publish an unfinished batch
		}
		target, tester, other := desktop, desktopUI, phone
		if record.Cols == 48 {
			target, tester, other = phone, phoneUI, desktop
			phoneFrames++
		} else {
			desktopFrames++
		}
		unchanged := other.Text()
		target.restoreSnapshot(source.Snapshot(), record.Cols, record.Rows)
		tester.Frame()
		if other.Text() != unchanged {
			t.Fatal("publishing one viewport changed the other")
		}
		if target.v.reflow || target.v.font.size != 13 {
			t.Fatal("viewport used projection or tiny font")
		}
		if strings.Contains(record.File, "draft") {
			draft = true
		}
		if draft && !streaming && !strings.Contains(target.Text(), "dual render fixture") {
			t.Fatalf("shared draft missing in %s", record.File)
		}
	}
	if !draft {
		t.Fatal("capture did not exercise input")
	}
	if (!control && phoneFrames == 0) || desktopFrames == 0 || source.term.Mode(vt.ModeSyncOutput) {
		t.Fatal("capture lacks complete updates for both viewports")
	}
	if streaming {
		if !strings.Contains(desktop.Text(), "fixture-099") || (!control && !strings.Contains(phone.Text(), "fixture-")) {
			t.Fatal("local streamed response missing from cached views")
		}
	}
	if control {
		save(t, desktopUI, "control-desktop")
		// Compare a fresh decoder/view with repeated cached-frame replacement.
		fresh, err := New(Options{Conn: newPipe(), FixedCols: 108, FixedRows: 58})
		if err != nil {
			t.Fatal(err)
		}
		defer fresh.Close()
		fresh.Feed(source.Snapshot())
		freshUI := ui.NewTester(func(c *ui.Context) { View(c, fresh).Fill() }, 1000, 920)
		freshUI.SetScale(2)
		freshUI.Frame()
		save(t, freshUI, "control-fresh")
		direct, err := New(Options{Conn: newPipe(), FixedCols: 108, FixedRows: 58})
		if err != nil {
			t.Fatal(err)
		}
		defer direct.Close()
		for _, record := range manifest {
			data, err := os.ReadFile(filepath.Join(dir, record.File))
			if err != nil {
				t.Fatal(err)
			}
			direct.Feed(data)
		}
		if direct.Text() != source.Text() {
			t.Fatal("fixed-size control decoders disagree")
		}
		directUI := ui.NewTester(func(c *ui.Context) { View(c, direct).Fill() }, 1000, 920)
		directUI.SetScale(2)
		directUI.Frame()
		save(t, directUI, "control-direct")
		different := 0
		for y := range 58 {
			original, _ := direct.term.RowText(y)
			restored, _ := fresh.term.RowText(y)
			if string(original) != string(restored) {
				different++
			}
		}
		t.Logf("fixed-size snapshot round trip changed %d visible text rows; see control-direct/control-fresh images", different)
		if dir := os.Getenv("MYGO_TEST_IMAGES"); dir != "" {
			data, err := json.MarshalIndent(map[string]int{"snapshot_changed_visible_rows": different}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "control-diagnostics.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		save(t, phoneUI, "dual-render-phone")
		save(t, desktopUI, "dual-render-desktop")
	}
	t.Logf("%d phone and %d desktop cached updates, %d unfinished chunks held; batch completion does not confirm resize", phoneFrames, desktopFrames, held)
}

// A program can finish a valid synchronized frame before applying SIGWINCH to
// its layout. This real PTY fixture deliberately defers layout until the next
// event, disproving the proposed first-batch-is-resize-ack rule without relying
// on timers or on any particular Agent's current implementation.
func TestDualSizeRedrawFirstBatchDoesNotConfirmGeometry(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("uses POSIX SIGWINCH")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires Python PTY fixture")
	}
	script := `import os, signal, tty
tty.setraw(0)
layout = os.get_terminal_size(0)
def frame():
    os.write(1, ('\x1b[?2026h\x1b[?1049h\x1b[2J\x1b[1;1HGEOMETRY=%dx%d\x1b[?2026l' % layout).encode())
def resize(sig, context):
    frame()  # finished output batch using the previous application layout
signal.signal(signal.SIGWINCH, resize)
frame()
while os.read(0, 1):
    layout = os.get_terminal_size(0)
    frame()
`
	process, err := pty.Start(pty.Options{Path: python, Args: []string{python, "-u", "-c", script}, Dir: t.TempDir(), Env: []string{"TERM=xterm-256color"}, Cols: 108, Rows: 58})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { process.Kill(); process.Close(); process.Wait() })
	batches := make(chan []byte)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		reader := bufio.NewReader(process)
		for {
			data, err := reader.ReadBytes('l') // fixture emits only one ending 'l'
			if err != nil {
				return
			}
			select {
			case batches <- data:
			case <-done:
				return
			}
		}
	}()
	read := func() []byte {
		t.Helper()
		select {
		case data := <-batches:
			if !bytes.HasSuffix(data, []byte("\x1b[?2026l")) {
				t.Fatal("unfinished fixture batch")
			}
			return data
		case <-time.After(3 * time.Second):
			t.Fatal("owned PTY did not redraw")
			return nil
		}
	}
	if !bytes.Contains(read(), []byte("GEOMETRY=108x58")) {
		t.Fatal("incorrect initial fixture geometry")
	}
	if err := process.Resize(48, 35, 0, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(read(), []byte("GEOMETRY=108x58")) {
		t.Fatal("fixture did not reproduce a complete stale-geometry batch")
	}
	if _, err := process.Write([]byte("r")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(read(), []byte("GEOMETRY=48x35")) {
		t.Fatal("fixture did not eventually apply kernel geometry")
	}
	t.Log("first completed batch after 48x35 resize still drew 108x58; cannot publish it as a confirmed phone frame")
}
