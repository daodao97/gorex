package terminal

import (
	"time"

	"gorex/internal/terminal/internal/vt"
)

// SearchState describes a literal, ASCII case-insensitive search of the active
// screen and its scrollback. Current is 1-based, ordered newest to oldest.
type SearchState struct {
	Total, Current int
	Busy           bool
	Err            error
}

// SetSearch changes the query without sending anything to the running program.
// An empty query removes highlights and releases the search's tracked state.
func (t *Terminal) SetSearch(query string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil {
		return nil
	}
	if query == "" {
		if t.search != nil {
			t.search.Free()
			t.search = nil
		}
		t.searchMoves = 0
	} else {
		if t.search == nil {
			var err error
			t.search, err = vt.NewSearch(t.term)
			if err != nil {
				t.search = nil
				return err
			}
		}
		if err := t.search.Needle(query); err != nil {
			return err
		}
		t.searchMoves = 1
	}
	t.redraw()
	return nil
}

// SearchNext selects toward older output (positive) or newer output (negative),
// wrapping around. Navigation waits for the incremental scan to finish.
func (t *Terminal) SearchNext(direction int) {
	t.mu.Lock()
	if t.search != nil {
		if direction < 0 {
			t.searchMoves--
		} else {
			t.searchMoves++
		}
	}
	t.mu.Unlock()
	t.redraw()
}

func (t *Terminal) SearchState() SearchState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.advanceSearch()
}

// advanceSearch does bounded work on each frame, so a large scrollback never
// requires a blocking full-buffer scan when typing in the find bar. t.mu is held.
func (t *Terminal) advanceSearch() SearchState {
	if t.search == nil || t.term == nil {
		return SearchState{}
	}
	if err := t.search.Feed(); err != nil {
		return SearchState{Err: err}
	}
	deadline := time.Now().Add(2 * time.Millisecond)
	status := 0
	for {
		var err error
		status, err = t.search.Tick()
		if err != nil {
			return SearchState{Err: err}
		}
		if status == 2 || time.Now().After(deadline) {
			break
		}
		if status == 1 {
			if err := t.search.Feed(); err != nil {
				return SearchState{Err: err}
			}
		}
	}
	total, current := t.search.Count()
	if status == 2 && total > 0 && current == 0 && t.searchMoves == 0 {
		// Reflow or pruning can invalidate the selected match. Keep the
		// find bar and viewport on a valid result after it catches up.
		t.searchMoves = 1
	}
	if status == 2 && t.searchMoves != 0 {
		moves := t.searchMoves
		t.searchMoves = 0
		total, _ := t.search.Count()
		if total > 0 {
			_, current := t.search.Count()
			if current == 0 {
				// The first step establishes a selection; only later steps
				// form a cycle. Keep early Enter presses during a long scan.
				if moves > 0 {
					moves = (moves-1)%total + 1
				} else {
					moves = -((-moves-1)%total + 1)
				}
			} else {
				moves %= total
			}
			for moves != 0 {
				step := 1
				if moves < 0 {
					step = -1
				}
				if err := t.search.Select(step); err != nil {
					return SearchState{Err: err}
				}
				moves -= step
			}
			if err := t.search.Feed(); err != nil {
				return SearchState{Err: err}
			}
		}
	}
	total, current = t.search.Count()
	return SearchState{Total: total, Current: current, Busy: status != 2}
}
