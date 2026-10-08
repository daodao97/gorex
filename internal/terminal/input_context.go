package terminal

import (
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/go-text/typesetting/segmenter"
	"gorex/internal/terminal/internal/vt"
)

// inputContext mirrors only locally typed, unsubmitted text. The remote
// program remains the editor; native caret changes become terminal keys.
// Output/prompt decorations are never copied into the keyboard's document.
type inputContext struct {
	mu    sync.Mutex
	text  []rune
	caret int
}

func (t *Terminal) textContext() (string, int) {
	t.context.mu.Lock()
	defer t.context.mu.Unlock()
	return string(t.context.text), t.context.caret
}
func (t *Terminal) clearInputContext() {
	t.context.mu.Lock()
	defer t.context.mu.Unlock()
	t.context.text, t.context.caret = nil, 0
}
func (t *Terminal) contextText(text string) {
	if !t.opts.InputContext {
		return
	}
	// Use the same lock order as pausing input, so a discarded keystroke
	// cannot leave editable native context behind during recovery.
	t.in.mu.Lock()
	defer t.in.mu.Unlock()
	if t.in.paused {
		return
	}
	t.context.mu.Lock()
	defer t.context.mu.Unlock()
	d := &t.context
	if strings.ContainsAny(text, "\x00\x1b\r") {
		d.text, d.caret = nil, 0
		return
	}
	insert := []rune(text)
	d.text = append(append(append([]rune(nil), d.text[:d.caret]...), insert...), d.text[d.caret:]...)
	d.caret += len(insert)
	// This is keyboard context, not a second persistent editor.
	if len(d.text) > 4096 {
		d.text, d.caret = nil, 0
	}
}
func contextBoundaries(text []rune) []int {
	var seg segmenter.Segmenter
	seg.Init(text)
	bounds := []int{0}
	it := seg.GraphemeIterator()
	for it.Next() {
		g := it.Grapheme()
		bounds = append(bounds, g.Offset+len(g.Text))
	}
	return bounds
}
func contextIndex(bounds []int, caret int) int {
	i := 0
	for i+1 < len(bounds) && bounds[i+1] <= caret {
		i++
	}
	return i
}
func (t *Terminal) contextKey(key vt.Key, mods ui.Modifiers, text string) {
	if !t.opts.InputContext {
		return
	}
	if key == vt.KeyEnter && mods == ui.Alt {
		t.contextText("\n")
		return
	}
	if mods&(ui.Ctrl|ui.Alt|ui.Super) != 0 {
		t.clearInputContext()
		return
	}
	if text != "" {
		t.contextText(text)
		return
	}
	t.context.mu.Lock()
	defer t.context.mu.Unlock()
	d := &t.context
	bounds := contextBoundaries(d.text)
	i := contextIndex(bounds, d.caret)
	switch key {
	case vt.KeyArrowLeft:
		d.caret = bounds[max(0, i-1)]
	case vt.KeyArrowRight:
		d.caret = bounds[min(len(bounds)-1, i+1)]
	case vt.KeyBackspace:
		if i > 0 {
			a, b := bounds[i-1], bounds[i]
			d.text = append(d.text[:a], d.text[b:]...)
			d.caret = a
		}
	case vt.KeyDelete:
		if i+1 < len(bounds) {
			a, b := bounds[i], bounds[i+1]
			d.text = append(d.text[:a], d.text[b:]...)
		}
	default:
		// History, completion, submission and navigation with unknown geometry
		// invalidate the mirror. Ordinary arrow keys still edit the live PTY.
		d.text, d.caret = nil, 0
	}
}
func (v *view) moveInputCaret(caret int) {
	t := v.t
	t.context.mu.Lock()
	d := &t.context
	bounds := contextBoundaries(d.text)
	from, to := contextIndex(bounds, d.caret), contextIndex(bounds, max(0, min(caret, len(d.text))))
	d.caret = bounds[to]
	t.context.mu.Unlock()
	key := ui.KeyRight
	if to < from {
		key = ui.KeyLeft
	}
	for n := 0; n < absInt(to-from); n++ {
		t.sendSoftwareKey(key, 0)
	}
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (v *view) replaceInput(ev ui.InputEvent) {
	t := v.t
	t.context.mu.Lock()
	d := &t.context
	bounds := contextBoundaries(d.text)
	from, to := max(0, min(ev.From, len(d.text))), max(0, min(ev.To, len(d.text)))
	if from > to {
		from, to = to, from
	}
	a, b := contextIndex(bounds, from), contextIndex(bounds, to)
	if bounds[b] < to {
		b++
	}
	// Expand partial native replacements to grapheme boundaries. Emoji and
	// combining accents must be deleted with one terminal key per grapheme.
	text := string(d.text[bounds[a]:from]) + ev.Text + string(d.text[to:bounds[b]])
	oldCaret := contextIndex(bounds, d.caret)
	// Apply the mirror mutation atomically; a snapshot restore may clear it
	// from the reader goroutine while terminal keys are being queued.
	d.text = append(d.text[:bounds[a]], d.text[bounds[b]:]...)
	d.caret = bounds[a]
	t.context.mu.Unlock()
	key := ui.KeyRight
	if b < oldCaret {
		key = ui.KeyLeft
	}
	for n := 0; n < absInt(b-oldCaret); n++ {
		t.sendSoftwareKey(key, 0)
	}
	for n := a; n < b; n++ {
		t.sendSoftwareKey(ui.KeyBackspace, 0)
	}
	if ev.Mods != 0 {
		v.modifiedText(text, ev.Mods)
	} else {
		v.typed(text)
	}
	if ev.Mods == 0 {
		v.moveInputCaret(ev.Caret)
	}
	v.preedit = ""
}

// InsertNewline inserts a line break without sending an unmodified Return.
// Bracketed paste works across agent composers, including legacy keyboards;
// Alt+Return is the fallback when the program has not enabled paste mode.
func (t *Terminal) InsertNewline() {
	t.mu.Lock()
	paste := t.term != nil && t.term.Mode(vt.ModeBracketedPaste)
	t.mu.Unlock()
	if paste {
		t.Paste("\n")
	} else {
		t.SendKey(ui.KeyEnter, ui.Alt)
	}
}
