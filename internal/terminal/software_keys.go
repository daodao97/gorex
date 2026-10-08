package terminal

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal/internal/vt"
)

// SendKey encodes a software key using the session's keyboard protocol and
// cursor mode. It is safe from any goroutine and includes release reporting
// when the application requests it.
func (t *Terminal) SendKey(key ui.Key, mods ui.Modifiers) bool {
	ok := t.sendSoftwareKey(key, mods)
	if ok {
		k, r := vtKey(key)
		text := ""
		if typesText(key) && mods&(ui.Ctrl|ui.Alt|ui.Super) == 0 {
			text = string(r)
			if mods&ui.Shift != 0 {
				text = shiftedText(key, r)
			}
		}
		t.contextKey(k, mods, text)
	}
	return ok
}
func (t *Terminal) sendSoftwareKey(key ui.Key, mods ui.Modifiers) bool {
	k, unshifted := vtKey(key)
	if k == vt.KeyUnidentified {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil {
		return false
	}
	encoder, err := vt.NewKeyEncoder()
	if err != nil {
		return false
	}
	defer encoder.Free()
	encoder.Sync(t.term, t.opts.OptionAsAlt)
	text := ""
	if mods&ui.Ctrl == 0 && unshifted != 0 {
		text = string(unshifted)
		if mods&ui.Shift != 0 {
			text = shiftedText(key, unshifted)
		}
	}
	b := encoder.Encode(vt.KeyEvent{Action: vt.KeyPress, Key: k, Mods: vtMods(mods), Unshifted: unshifted, Text: text})
	if len(b) == 0 {
		return false
	}
	t.in.push(append([]byte(nil), b...))
	b = encoder.Encode(vt.KeyEvent{Action: vt.KeyRelease, Key: k, Mods: vtMods(mods), Unshifted: unshifted})
	if len(b) > 0 {
		t.in.push(append([]byte(nil), b...))
	}
	return true
}

// The software keyboard reports text rather than hardware key-down events.
// Keep virtual modifiers in the encoder instead of hardcoding control bytes.
func (v *view) modifiedText(text string, mods ui.Modifiers) {
	v.pending, v.preedit = nil, ""
	if mods == ui.Super {
		switch strings.ToLower(text) {
		case "c":
			v.copy()
			return
		case "v":
			v.paste()
			return
		case "a":
			v.selectAll()
			return
		}
	}
	r, size := utf8.DecodeRuneInString(text)
	if size != len(text) || r >= 128 {
		v.t.contextText(text)
		v.sendText(text)
		return
	}
	key, base, shift := keyOfRune(r)
	if key == vt.KeyUnidentified {
		v.t.contextText(text)
		v.sendText(text)
		return
	}
	if shift {
		mods |= ui.Shift
	}
	if mods&ui.Shift != 0 && !shift {
		for physical, mapped := range keys {
			if mapped.key == key {
				text = shiftedText(physical, base)
				break
			}
		}
	}
	v.t.contextKey(key, mods, text)
	var consumed vt.Mods
	if mods&ui.Shift != 0 && text != string(base) {
		consumed = vt.ModShift
	}
	v.sendKey(vt.KeyEvent{Action: vt.KeyPress, Key: key, Mods: vtMods(mods), Consumed: consumed, Text: text, Unshifted: base})
	v.encodeKey(vt.KeyEvent{Action: vt.KeyRelease, Key: key, Mods: vtMods(mods), Unshifted: base}, false)
}
