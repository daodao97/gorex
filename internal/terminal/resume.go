package terminal

import "retty/internal/terminal/internal/vt"

type readingPosition struct {
	offset, distance int
	absolute         bool
}

func readingAt(screen *vt.Terminal) *readingPosition {
	if screen == nil || screen.AtBottom() {
		return nil
	}
	bar := screen.Scrollbar()
	return &readingPosition{offset: int(bar.Offset), distance: int(bar.Total - bar.Offset - bar.Len)}
}

func (p *readingPosition) restore(screen *vt.Terminal) {
	if p == nil || screen == nil {
		return
	}
	if p.absolute {
		screen.ScrollToRow(p.offset)
	} else {
		screen.ScrollToBottom()
		screen.ScrollBy(-p.distance)
	}
}

// A replacement arrives only after its entire authenticated frame is read.
// Keep the old emulator and reading viewport intact until that point, then
// replace the screen in one critical section. No PTY resize is sent by viewers.
func (t *Terminal) restoreSnapshot(data []byte, cols, rows int) {
	t.clearInputContext()
	t.mu.Lock()
	if t.term == nil || t.closed {
		t.mu.Unlock()
		return
	}
	screen := t.term
	if t.opts.ReflowView && !t.opts.FitToView && t.presentation != nil {
		screen = t.presentation
	}
	position := readingAt(screen)
	oldTotal := int(t.term.Scrollbar().Total)
	oldCols, oldRows := t.size.cols, t.size.rows
	if cols > 0 && rows > 0 && t.opts.FixedCols > 0 && t.opts.FixedRows > 0 {
		t.opts.FixedCols, t.opts.FixedRows = cols, rows
		size := t.size
		size.cols, size.rows = cols, rows
		t.resize(size)
	}
	t.term.Reset()
	t.term.Write(data)
	t.revision++
	if position != nil {
		newTotal := int(t.term.Scrollbar().Total)
		// With retained history and unchanged source geometry, appended output
		// must not push the reader toward newer content. Load enough history in
		// the local projection to preserve its original starting row.
		position.absolute = newTotal >= oldTotal && oldCols == t.size.cols && oldRows == t.size.rows
		if screen == t.presentation {
			if position.absolute {
				t.presentationHistory += newTotal - oldTotal
			}
			t.resumeReading = position
		} else {
			position.restore(t.term)
		}
	}
	ev := t.events
	t.events = events{}
	title := ""
	if ev.title {
		title = t.term.Title()
	}
	t.mu.Unlock()
	t.fire(ev, title)
	t.redraw()
}
