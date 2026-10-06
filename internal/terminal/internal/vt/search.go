package vt

import (
	"runtime"
	"unsafe"
)

// These bindings match include/ghostty/vt/search.h in the pinned native build.
var fnSearchNew, fnSearchFree, fnSearchSet, fnSearchGet, fnSearchFeed, fnSearchTick, fnPointFromGridRef uintptr

func init() {
	symbols = append(symbols, []struct {
		name string
		addr *uintptr
	}{
		{"ghostty_search_new", &fnSearchNew}, {"ghostty_search_free", &fnSearchFree},
		{"ghostty_search_set", &fnSearchSet}, {"ghostty_search_get", &fnSearchGet},
		{"ghostty_search_feed", &fnSearchFeed}, {"ghostty_search_tick", &fnSearchTick},
		{"ghostty_terminal_point_from_grid_ref", &fnPointFromGridRef},
	}...)
	enums = append(enums, []struct {
		typ, name string
		value     int64
	}{
		{"GhosttySearchStatus", "COMPLETE", 2},
		{"GhosttySearchOption", "SELECT_PREV", 2},
		{"GhosttySearchData", "VIEWPORT_MATCHES", 6},
		{"GhosttyPointTag", "SCREEN", 2},
	}...)
}

type selectionBuffer struct{ ptr, cap, len uintptr }
type pointCoordinate struct {
	x uint16
	y uint32
}

type Search struct{ h uintptr }

func NewSearch(t *Terminal) (*Search, error) {
	s := &Search{}
	err := result(call(fnSearchNew, 0, uintptr(unsafe.Pointer(&s.h)), t.h))
	return s, err
}

func (s *Search) Free() { call(fnSearchFree, s.h); s.h = 0 }

func (s *Search) Needle(query string) error {
	b := []byte(query)
	v := cString{len: uintptr(len(b))}
	if len(b) > 0 {
		v.ptr = uintptr(unsafe.Pointer(&b[0]))
	}
	err := result(call(fnSearchSet, s.h, 0, uintptr(unsafe.Pointer(&v))))
	runtime.KeepAlive(b)
	return err
}

func (s *Search) Feed() error { return result(call(fnSearchFeed, s.h)) }
func (s *Search) Tick() (int, error) {
	var status int32
	err := result(call(fnSearchTick, s.h, uintptr(unsafe.Pointer(&status))))
	return int(status), err
}

func (s *Search) Select(direction int) error {
	option := uintptr(1) // toward older content
	if direction < 0 {
		option = 2
	}
	return result(call(fnSearchSet, s.h, option, 0))
}

func (s *Search) Count() (total, current int) {
	var n, index uintptr
	call(fnSearchGet, s.h, 2, uintptr(unsafe.Pointer(&n)))
	if ok(call(fnSearchGet, s.h, 3, uintptr(unsafe.Pointer(&index)))) {
		current = int(index) + 1
	}
	return int(n), current
}

func (s *Search) Selected() (Selection, bool) {
	v := Selection{size: unsafe.Sizeof(Selection{})}
	return v, ok(call(fnSearchGet, s.h, 4, uintptr(unsafe.Pointer(&v))))
}

// MatchRect is a run of cells on one viewport row. End is exclusive.
type MatchRect struct {
	Row, Start, End int
	Selected        bool
}

// Highlights converts snapshots to plain coordinates while the terminal is
// locked. Screen coordinates allow clipping a wrapped match at viewport edges.
func (s *Search) Highlights(t *Terminal, buf []Selection, rects []MatchRect) ([]Selection, []MatchRect) {
	b := selectionBuffer{}
	call(fnSearchGet, s.h, 6, uintptr(unsafe.Pointer(&b)))
	if cap(buf) < int(b.len) {
		buf = make([]Selection, b.len)
	} else {
		buf = buf[:b.len]
	}
	if len(buf) == 0 {
		return buf, rects[:0]
	}
	b = selectionBuffer{ptr: uintptr(unsafe.Pointer(&buf[0])), cap: uintptr(len(buf))}
	if !ok(call(fnSearchGet, s.h, 6, uintptr(unsafe.Pointer(&b)))) {
		return buf, rects[:0]
	}
	selected, hasSelected := s.Selected()
	viewport := t.Scrollbar()
	cols, rows := t.Size()
	rects = rects[:0]
	for _, match := range buf[:b.len] {
		var a, z pointCoordinate
		if !ok(call(fnPointFromGridRef, t.h, uintptr(unsafe.Pointer(&match.start)), 2, uintptr(unsafe.Pointer(&a)))) ||
			!ok(call(fnPointFromGridRef, t.h, uintptr(unsafe.Pointer(&match.end)), 2, uintptr(unsafe.Pointer(&z)))) {
			continue
		}
		first, last := int(a.y)-int(viewport.Offset), int(z.y)-int(viewport.Offset)
		active := hasSelected && match.start == selected.start && match.end == selected.end
		for y := max(first, 0); y <= min(last, rows-1); y++ {
			left, right := 0, cols
			if y == first {
				left = int(a.x)
			}
			if y == last {
				right = int(z.x) + 1
				// Native search endpoints identify a grapheme's leading cell;
				// include the trailing cell of a wide glyph in its highlight.
				var raw uint64
				if ok(call(fnGridRefCell, uintptr(unsafe.Pointer(&match.end)), uintptr(unsafe.Pointer(&raw)))) && Wide(cell.wide.of(raw)) == WideChar {
					right = min(right+1, cols)
				}
			}
			rects = append(rects, MatchRect{Row: y, Start: left, End: right, Selected: active})
		}
	}
	runtime.KeepAlive(buf)
	return buf, rects
}
