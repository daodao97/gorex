package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSnapshotReplacementPreservesReadingAcrossAppendedOutput(t *testing.T) {
	loadLib(t)
	for _, history := range []int{200, 900} {
		for _, reflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("history%d-reflow%v", history, reflow), func(t *testing.T) {
				source, err := New(Options{Conn: newPipe(), FixedCols: 80, FixedRows: 24})
				if err != nil {
					t.Fatal(err)
				}
				defer source.Close()
				phone, err := New(Options{Conn: newPipe(), FixedCols: 80, FixedRows: 24, ReflowView: reflow, FitToView: !reflow, NoBlink: true})
				if err != nil {
					t.Fatal(err)
				}
				defer phone.Close()
				for i := 0; i < history; i++ {
					source.Feed([]byte(fmt.Sprintf("row %04d 中文🙂 reading position\r\n", i)))
				}
				phone.Feed(source.Snapshot())
				tt := ui.NewTester(func(c *ui.Context) { View(c, phone).Fill() }, 390, 620)
				phone.mu.Lock()
				screen := phone.v.screen()
				screen.ScrollBy(-100)
				phone.mu.Unlock()
				tt.Frame()
				phone.mu.Lock()
				before, _ := phone.v.screen().RowText(0)
				offset := phone.v.screen().Scrollbar().Offset
				phone.mu.Unlock()
				for i := history; i < history+40; i++ {
					source.Feed([]byte(fmt.Sprintf("row %04d 中文🙂 reading position\r\n", i)))
				}
				phone.restoreSnapshot(source.Snapshot(), 80, 24)
				tt.Frame()
				phone.mu.Lock()
				after, _ := phone.v.screen().RowText(0)
				bar := phone.v.screen().Scrollbar()
				phone.mu.Unlock()
				if strings.TrimSpace(string(before)) == "" || string(before) != string(after) || offset != bar.Offset {
					t.Fatalf("reading position moved: before=%q after=%q offset=%d/%d", string(before), string(after), offset, bar.Offset)
				}
				if phone.Text() != source.Text() {
					t.Fatal("replacement appended duplicate history or lost output")
				}
			})
		}
	}
}

func TestSnapshotReplacementKeepsBottomAndChangesBuffers(t *testing.T) {
	loadLib(t)
	phone, err := New(Options{Conn: newPipe(), FixedCols: 80, FixedRows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	source, err := New(Options{Conn: newPipe(), FixedCols: 100, FixedRows: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	phone.Feed([]byte("\x1b[?1049hOLD FULLSCREEN"))
	source.Feed([]byte("fresh shell 中文🙂"))
	phone.restoreSnapshot(source.Snapshot(), 100, 32)
	if phone.Text() != source.Text() || phone.term.AltScreen() || !phone.term.AtBottom() {
		t.Fatal("snapshot retained stale alternate screen or lost bottom follow")
	}
	if c, r := phone.Size(); c != 100 || r != 32 {
		t.Fatal("replacement decoded with stale source geometry")
	}
}

func TestDisabledInputDiscardsPendingKeysAndAllowsReading(t *testing.T) {
	loadLib(t)
	phone, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	phone.Feed([]byte("kept while offline"))
	phone.SetInputEnabled(false)
	phone.Send([]byte("dangerous command\r"))
	phone.in.mu.Lock()
	queued := len(phone.in.queue)
	phone.in.mu.Unlock()
	if queued != 0 || !strings.Contains(phone.Text(), "kept while offline") {
		t.Fatal("disabled input queued commands or cleared reading content")
	}
	phone.SetInputEnabled(true)
	phone.in.mu.Lock()
	queued = len(phone.in.queue)
	phone.in.mu.Unlock()
	if queued != 0 {
		t.Fatal("reenabling input resurrected offline commands")
	}
}
