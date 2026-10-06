package terminal

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func searchTerminal(t *testing.T, cols, rows int) *Terminal {
	t.Helper()
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	term.Resize(cols, rows)
	t.Cleanup(func() { term.Close() })
	return term
}

func settledSearch(t *testing.T, term *Terminal) SearchState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := term.SearchState()
		if s.Err != nil {
			t.Fatal(s.Err)
		}
		if !s.Busy {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("search did not complete")
		}
	}
}

func TestSearchHistoryNavigationAndLiveOutput(t *testing.T) {
	term := searchTerminal(t, 32, 4)
	for i := range 80 {
		term.Feed([]byte(fmt.Sprintf("row %02d needle\r\n", i)))
	}
	before := term.Text()
	if err := term.SetSearch("NEEDLE"); err != nil {
		t.Fatal(err)
	}
	if s := settledSearch(t, term); s.Total != 80 || s.Current != 1 {
		t.Fatalf("initial search %+v", s)
	}
	for range 79 {
		term.SearchNext(1)
	}
	if s := settledSearch(t, term); s.Current != 80 {
		t.Fatalf("oldest match %+v", s)
	}
	if sb := term.term.Scrollbar(); sb.Offset != 0 {
		t.Fatalf("oldest match did not scroll into view: %+v", sb)
	}
	term.SearchNext(1)
	if s := settledSearch(t, term); s.Current != 1 {
		t.Fatalf("next did not wrap: %+v", s)
	}
	term.SearchNext(-1)
	if s := settledSearch(t, term); s.Current != 80 {
		t.Fatalf("previous did not wrap: %+v", s)
	}
	term.Feed([]byte("another needle\r\n"))
	if s := settledSearch(t, term); s.Total != 81 {
		t.Fatalf("live output missing: %+v", s)
	}
	if !strings.HasPrefix(term.Text(), before) {
		t.Fatal("search changed terminal contents")
	}
	if err := term.SetSearch(""); err != nil {
		t.Fatal(err)
	}
	if term.search != nil {
		t.Fatal("closing search retained native search state")
	}
}

func TestSearchWrappedUnicodeAndResize(t *testing.T) {
	term := searchTerminal(t, 10, 5)
	term.Feed([]byte("12345678中文搜索🙂abcd\r\n"))
	if err := term.SetSearch("中文搜索🙂"); err != nil {
		t.Fatal(err)
	}
	if s := settledSearch(t, term); s.Total != 1 {
		t.Fatalf("Unicode match %+v", s)
	}
	buf, rects := term.search.Highlights(term.term, nil, nil)
	if len(rects) != 2 {
		t.Fatalf("wrapped highlight %+v", rects)
	}
	if rects[0].Start != 8 || rects[0].End != 10 || rects[1].End != 8 || !rects[0].Selected {
		t.Fatalf("wide glyph highlight %+v", rects)
	}
	term.Resize(24, 5)
	if s := settledSearch(t, term); s.Total != 1 || s.Current != 1 {
		t.Fatalf("search lost after reflow %+v", s)
	}
	_, rects = term.search.Highlights(term.term, buf, rects)
	if len(rects) != 1 || rects[0].Start != 8 || rects[0].End != 18 {
		t.Fatalf("reflowed highlight %+v", rects)
	}
	term.SetSearch("missing")
	if s := settledSearch(t, term); s.Total != 0 || s.Current != 0 {
		t.Fatalf("stale match %+v", s)
	}
	term.SetSearch("abcd")
	if s := settledSearch(t, term); s.Total != 1 {
		t.Fatalf("replacement query %+v", s)
	}
}

func TestSearchClearAndAlternateScreen(t *testing.T) {
	term := searchTerminal(t, 40, 4)
	term.Feed([]byte("primary target\r\n"))
	term.SetSearch("target")
	if s := settledSearch(t, term); s.Total != 1 {
		t.Fatal(s)
	}
	term.Feed([]byte("\x1b[?1049hfullscreen target target"))
	if s := settledSearch(t, term); s.Total != 2 {
		t.Fatalf("alternate screen %+v", s)
	}
	term.Feed([]byte("\x1b[?1049l"))
	if s := settledSearch(t, term); s.Total != 1 {
		t.Fatalf("primary screen %+v", s)
	}
	term.Feed([]byte("\x1b[H\x1b[2J\x1b[3J"))
	if s := settledSearch(t, term); s.Total != 0 {
		t.Fatalf("cleared content still matched %+v", s)
	}
	term.mu.Lock()
	_, selected := term.term.SelectionText()
	term.mu.Unlock()
	if selected {
		t.Fatal("search overwrote the terminal's normal selection")
	}
}

func TestSearchHighlightsClipWrappedMatch(t *testing.T) {
	term := searchTerminal(t, 5, 2)
	term.Feed([]byte("abcdefghijklmnopqrst\r\n"))
	term.SetSearch("abcdefghijklmnopqrst")
	settledSearch(t, term)
	term.term.ScrollToRow(1)
	term.search.Feed()
	_, rects := term.search.Highlights(term.term, nil, nil)
	if len(rects) != 2 {
		t.Fatalf("partial viewport match %+v", rects)
	}
	for _, r := range rects {
		if r.Start != 0 || r.End != 5 {
			t.Fatalf("partial match %+v", r)
		}
	}
}
