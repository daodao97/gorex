package terminal

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestTerminalNativeContextEditsLiveInput(t *testing.T) {
	loadLib(t)
	conn := newPipe()
	term, err := New(Options{Conn: conn, InputContext: true, OptionAsAlt: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	tt.Type("ab你好😀écd")
	if got := conn.take(len("ab你好😀écd")); got != "ab你好😀écd" {
		t.Fatal(got)
	}
	// Native offsets count runes; remote movement counts graphemes, not cells
	// or UTF-16 units (CJK, emoji, combining accents).
	term.v.input(ui.InputEvent{Kind: ui.InputSelection, Caret: 2})
	want := "\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D"
	if got := conn.take(len(want)); got != want {
		t.Fatal("native caret", got)
	}
	term.v.input(ui.InputEvent{Kind: ui.InputTextReplace, From: 2, To: 3, Text: "新", Caret: 3})
	want = "\x1b[C\x7f新"
	if got := conn.take(len(want)); got != want {
		t.Fatal("replace previous character", got)
	}
	text, caret := term.textContext()
	if text != "ab新好😀écd" || caret != 3 {
		t.Fatal(text, caret)
	}
	// An accent-only replacement expands to the terminal's grapheme.
	term.v.input(ui.InputEvent{Kind: ui.InputTextReplace, From: 6, To: 7, Text: "̀", Caret: 7})
	want = "\x1b[C\x1b[C\x1b[C\x7fè"
	if got := conn.take(len(want)); got != want {
		t.Fatal("grapheme replacement", got)
	}
	term.Feed([]byte("\x1b[?2004h"))
	term.InsertNewline()
	want = "\x1b[200~\n\x1b[201~"
	if got := conn.take(len(want)); got != want {
		t.Fatal("newline submitted", got)
	}
	if text, _ := term.textContext(); text != "ab新好😀è\ncd" {
		t.Fatal(text)
	}
	term.SendKey(ui.KeyEnter, 0)
	if conn.take(1) != "\r" {
		t.Fatal("Return no longer submits")
	}
	if text, caret := term.textContext(); text != "" || caret != 0 {
		t.Fatal("submitted input remained editable", text, caret)
	}
	tt.Type("stale")
	conn.take(5)
	term.SetInputEnabled(false)
	term.v.input(ui.InputEvent{Kind: ui.InputTextReplace, From: 0, To: 0, Text: "offline", Caret: 7})
	if text, _ := term.textContext(); text != "" {
		t.Fatal("offline native edit retained", text)
	}
}

func TestTerminalNewlineLegacyFallback(t *testing.T) {
	loadLib(t)
	conn := newPipe()
	term, err := New(Options{Conn: conn, InputContext: true, OptionAsAlt: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.InsertNewline()
	if got := conn.take(2); got != "\x1b\r" {
		t.Fatal("newline degraded to Return", got)
	}
	if text, caret := term.textContext(); text != "\n" || caret != 1 {
		t.Fatal(text, caret)
	}
}
