package vt

import (
	"testing"
	"unsafe"
)

func TestSelectionTextCopiesVisibleList(t *testing.T) {
	loadLib(t)
	term, err := NewTerminal(60, 4, Effects{})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Free()
	term.Write([]byte("\x1b[2m1.\x1b[0m \x1b[8m**\x1b[28m\x1b[1m第一项\x1b[22m\x1b[8m**\x1b[0m\r\n2. 第二项"))
	sel, ok := term.SelectAll()
	if !ok {
		t.Fatal("no selection")
	}
	term.SetSelection(&sel)
	if got, ok := term.VisibleSelectionText(); !ok || got != "1. 第一项\n2. 第二项" {
		t.Fatalf("selected text = %q, %v", got, ok)
	}
	if got, ok := term.SelectionText(); !ok || got != "1. **第一项**\n2. 第二项" {
		t.Fatalf("raw text = %q, %v", got, ok)
	}
}

func TestVisibleSelectionTextRangesAndScrollback(t *testing.T) {
	loadLib(t)
	term, err := NewTerminal(12, 2, Effects{})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Free()
	if _, ok := term.VisibleSelectionText(); ok {
		t.Fatal("copied without a selection")
	}
	term.Write([]byte("10. \x1b[8m**\x1b[28mabcdefghijklmnop\x1b[8m**\x1b[0m\r\n11. 👩‍💻 é\r\n12. `**code**`"))
	sel, ok := term.SelectAll()
	if !ok {
		t.Fatal("no selection")
	}
	term.SetSelection(&sel)
	want := "10. abcdefghijklmnop\n11. 👩‍💻 é\n12. `**code**`"
	if got, ok := term.VisibleSelectionText(); !ok || got != want {
		t.Fatalf("wrapped scrollback copy = %q, %v", got, ok)
	}
	// Formatting must not move the viewport or alter the user's selection.
	before := term.Scrollbar()
	term.VisibleSelectionText()
	if term.Scrollbar() != before {
		t.Fatal("copy changed the viewport")
	}
	term.ScrollToBottom()
	term.Write([]byte("\x1b[3J\x1b[H\x1b[2J1. first\r\n2. second"))
	a, okA := term.CellAt(0, 0)
	b, okB := term.CellAt(8, 1)
	if !okA || !okB {
		t.Fatal("missing range endpoints")
	}
	reverse := Selection{size: unsafe.Sizeof(Selection{}), start: b, end: a}
	term.SetSelection(&reverse)
	if got, ok := term.VisibleSelectionText(); !ok || got != "1. first\n2. second" {
		t.Fatalf("reverse selection = %q, %v", got, ok)
	}
	b, _ = term.CellAt(1, 1)
	rect := Selection{size: unsafe.Sizeof(Selection{}), start: a, end: b, rectangle: true}
	term.SetSelection(&rect)
	if got, ok := term.VisibleSelectionText(); !ok || got != "1.\n2." {
		t.Fatalf("rectangular list markers = %q, %v", got, ok)
	}
}

func TestVisibleFormattedTextStylesAndLinks(t *testing.T) {
	for _, tt := range []struct{ name, styled, want string }{
		{"resets", "\x1b[8mhidden\x1b[28m1. one\x1b[8mhidden\x1b[m\n2. two", "1. one\n2. two"},
		{"palette", "\x1b[38;5;8m1. dim\x1b[8;48;5;0mhidden\x1b[28m visible", "1. dim visible"},
		{"rgb", "\x1b[38;2;0;8;28m1. color\x1b[8;48;2;28;0;8mhidden\x1b[0m visible", "1. color visible"},
		{"colon colors", "\x1b[38:2::0:8:28m1. color\x1b[8;48:5:0mhidden\x1b[0m visible", "1. color visible"},
		{"hyperlink", "1. \x1b]8;;https://example.com/raw.md\x1b\\显示文字\x1b]8;;\x1b\\\r\n2. text", "1. 显示文字\n2. text"},
		{"literal markdown", "1. `**code**`\n2. #heading [link](path.md)", "1. `**code**`\n2. #heading [link](path.md)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := visibleFormattedText(tt.styled); got != tt.want {
				t.Fatalf("copy = %q, want %q", got, tt.want)
			}
		})
	}
}
