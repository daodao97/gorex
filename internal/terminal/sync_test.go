package terminal

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSynchronizedOutputKeepsCompletedFrame(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe(), NoBlink: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 500, 180)
	term.Feed([]byte("\x1b[?25l\x1b[Hcompleted frame"))
	tt.Frame()
	before := append([]byte(nil), tt.Image().Pix...)
	for _, data := range []string{"\x1b[?2026h", "\x1b[H\x1b[2J", "partial", "\x1b[?2026h", " update"} {
		term.Feed([]byte(data))
		tt.Frame()
		if !bytes.Equal(before, tt.Image().Pix) {
			t.Fatalf("incomplete synchronized output became visible after %q", data)
		}
	}
	term.Feed([]byte("\x1b[?2026l"))
	tt.Frame()
	if bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("completed synchronized output was never displayed")
	}
}

func TestSynchronizedOutputTimeoutReleasesFrame(t *testing.T) {
	loadLib(t)
	var reported []RenderEvent
	var term *Terminal
	var err error
	term, err = New(Options{Conn: newPipe(), NoBlink: true, OnRenderEvent: func(event RenderEvent) {
		term.Size() // A diagnostic callback must run outside terminal locks.
		reported = append(reported, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 500, 180)
	term.Feed([]byte("\x1b[?25l\x1b[Hold"))
	tt.Frame()
	before := append([]byte(nil), tt.Image().Pix...)
	term.Feed([]byte("\x1b[?2026h\x1b[H\x1b[2Jnew"))
	term.mu.Lock()
	term.heldSince = time.Now().Add(-2 * time.Second)
	term.holdStarted = term.heldSince
	term.mu.Unlock()
	tt.Frame()
	if term.held || bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("expired synchronized update did not release the new frame")
	}
	if len(reported) != 1 || reported[0].Kind != "sync_timeout" || reported[0].Duration < 2*time.Second {
		t.Fatalf("missing watchdog diagnostic: %+v", reported)
	}
}

func TestSynchronizedOutputMultipleUpdatesBeforeFrame(t *testing.T) {
	loadLib(t)
	makeTerminal := func() (*Terminal, *ui.Tester) {
		term, err := New(Options{Conn: newPipe(), NoBlink: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { term.Close() })
		tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 500, 180)
		term.Feed([]byte("\x1b[?25l\x1b[Hold1\r\nold2\r\nold3"))
		tt.Frame()
		return term, tt
	}
	term, tt := makeTerminal()
	want, expected := makeTerminal()
	for i := range 3 {
		data := fmt.Sprintf("\x1b[%d;1Hnew%d", i+1, i+1)
		term.Feed([]byte("\x1b[?2026h" + data + "\x1b[?2026l"))
		want.Feed([]byte(data))
	}
	tt.Frame()
	expected.Frame()
	if !bytes.Equal(tt.Image().Pix, expected.Image().Pix) {
		t.Fatal("successive synchronized updates lost a row before the next UI frame")
	}
}

func TestSynchronizedOutputActiveLongUpdateKeepsCompletedFrame(t *testing.T) {
	loadLib(t)
	var reported []RenderEvent
	var term *Terminal
	var err error
	term, err = New(Options{Conn: newPipe(), NoBlink: true, OnRenderEvent: func(event RenderEvent) {
		term.Size()
		reported = append(reported, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 500, 180)
	term.Feed([]byte("\x1b[?25l\x1b[Hcompleted frame"))
	tt.Frame()
	before := append([]byte(nil), tt.Image().Pix...)
	term.Feed([]byte("\x1b[?2026h\x1b[H\x1b[2J"))
	// A large redraw can take longer than the watchdog interval while
	// still making progress. Its clear/partial frame must stay hidden.
	term.mu.Lock()
	term.heldSince = time.Now().Add(-2 * time.Second)
	term.holdStarted = term.heldSince
	term.mu.Unlock()
	term.Feed([]byte("still writing a large frame"))
	tt.Frame()
	if !bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("active synchronized output was forcibly exposed mid-frame")
	}
	term.Feed([]byte("\x1b[?2026l"))
	tt.Frame()
	if bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("finished long update stayed hidden")
	}
	if len(reported) != 1 || reported[0].Kind != "slow_sync" || reported[0].Duration < 2*time.Second {
		t.Fatalf("missing slow redraw diagnostic: %+v", reported)
	}
}
