package screen

import (
	"gorex/internal/terminal/internal/vt"
	"testing"
)

func TestScreenSnapshotRetainsHistoryUnicodeModesAndTitle(t *testing.T) {
	bells := 0
	s, err := New(80, 24, 8<<20, func() { bells++ })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Feed([]byte("saved history\r\n\x1b]2;server task\a\x1b[31m中文 👩‍💻\x1b[0m\a\x1b[?2004h"))
	if bells != 1 || s.Title() != "server task" {
		t.Fatal("lost bell or title")
	}
	s.Resize(40, 12)
	copy, err := New(40, 12, 8<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	copy.Feed(s.Snapshot())
	if got, want := copy.term.Text(), s.term.Text(); got != want {
		t.Fatalf("reconnected screen differs: %q / %q", got, want)
	}
	if !copy.term.Mode(vt.ModeBracketedPaste) {
		t.Fatal("lost bracketed paste")
	}
	s.Feed([]byte("\x1b[?1049h\x1b[H\x1b[2Jfull screen\x1b[?25l"))
	copy.Feed(s.Snapshot())
	if !copy.term.AltScreen() || copy.term.Mode(vt.ModeCursorVisible) || copy.term.Text() != s.term.Text() {
		t.Fatal("lost alternate screen or cursor mode")
	}
	s.Close()
	s.Close()
	s.Feed([]byte("closed"))
	s.Resize(80, 24)
	if s.Snapshot() != nil || s.Title() != "" {
		t.Fatal("closed screen retained native memory")
	}
}
