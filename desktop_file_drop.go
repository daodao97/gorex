package main

import (
	"strconv"
	"strings"
	"unicode"
)

func (p *Pane) acceptsFileDrop() bool {
	return p.term != nil && !p.closed && (p.host == nil || p.remoteView != nil && p.remoteView.inputReady())
}

func (a *App) pasteDroppedFiles(t *Tab, p *Pane, paths []string) {
	if !p.acceptsFileDrop() {
		return
	}
	text := droppedFilePaths(paths)
	if text == "" {
		return
	}
	t.setFocus(p)
	a.focusReq = p
	a.changed()
	p.term.Paste(text)
}

// Treat each path as one shell argument, never as shell syntax. Leave a space
// after the last path so another argument can be typed without joining it.
func droppedFilePaths(paths []string) string {
	var words []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		switch {
		case strings.IndexFunc(path, unicode.IsControl) >= 0:
			// ANSI-C quoting keeps newlines and other controls out of terminal
			// input events, where they could submit an Agent prompt.
			quoted := strconv.Quote(path)
			words = append(words, "$'"+strings.ReplaceAll(quoted[1:len(quoted)-1], "'", "\\'")+"'")
		case strings.IndexFunc(path, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_-.", r))
		}) < 0:
			words = append(words, path)
		default:
			words = append(words, "'"+strings.ReplaceAll(path, "'", "'\\''")+"'")
		}
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ") + " "
}
