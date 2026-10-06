package vt

import (
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// VisibleSelectionText formats just the selected cells, including scrollback,
// then omits concealed text and terminal controls. Literal punctuation,
// including list markers and code, stays intact without parsing Markdown.
func (t *Terminal) VisibleSelectionText() (string, bool) {
	sel := Selection{size: unsafe.Sizeof(Selection{})}
	if t.get(dataSelection, unsafe.Pointer(&sel)) != nil {
		return "", false
	}
	var pin runtime.Pinner
	pin.Pin(&sel)
	defer pin.Unpin()
	opts := formatterOptions{size: unsafe.Sizeof(formatterOptions{}), emit: formatVT, unwrap: true, trim: true, selection: uintptr(unsafe.Pointer(&sel))}
	opts.extra.size = unsafe.Sizeof(opts.extra)
	opts.extra.screen.size = unsafe.Sizeof(opts.extra.screen)
	var f uintptr
	if formatterTerminalNew(0, &f, t.h, opts) != 0 {
		return "", false
	}
	defer call(fnFormatterFree, f)
	var p, n uintptr
	if !ok(call(fnFormatterFormatAlloc, f, 0, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))) {
		return "", false
	}
	text := goString(p, n)
	if p != 0 {
		call(fnFree, 0, p, n)
	}
	return visibleFormattedText(text), true
}

// This reads the styled formatter's output, never the live PTY stream.
// The formatter already handled erases, reflow, ranges and soft wraps.
func visibleFormattedText(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	concealed := false
	for i := 0; i < len(text); {
		b := text[i]
		if b != '\x1b' {
			if b == '\n' || !concealed && (b >= ' ' || b == '\t') {
				out.WriteByte(b)
			}
			i++
			continue
		}
		i++
		if i == len(text) {
			break
		}
		switch text[i] {
		case '[':
			i++
			start := i
			for i < len(text) && (text[i] < 0x40 || text[i] > 0x7e) {
				i++
			}
			if i < len(text) {
				if text[i] == 'm' {
					concealed = sgrConcealed(text[start:i], concealed)
				}
				i++
			}
		case ']', 'P', '^', '_':
			// OSC hyperlinks/palette and string controls are not visible.
			i++
			for i < len(text) {
				if text[i] == '\a' {
					i++
					break
				}
				if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			for i < len(text) && text[i] >= 0x20 && text[i] <= 0x2f {
				i++
			}
			if i < len(text) {
				i++
			}
		}
	}
	return out.String()
}

func sgrConcealed(params string, concealed bool) bool {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		code := 0 // An empty SGR parameter resets the style.
		if parts[i] != "" {
			var err error
			code, err = strconv.Atoi(parts[i])
			if err != nil {
				continue // Colon subparameters aren't standalone SGR codes.
			}
		}
		switch code {
		case 0, 28:
			concealed = false
		case 8:
			concealed = true
		case 38, 48, 58:
			// Color values 0, 8 and 28 aren't reset/conceal instructions.
			if i+1 < len(parts) {
				switch parts[i+1] {
				case "2":
					i += 4
				case "5":
					i += 2
				}
			}
		}
	}
	return concealed
}
