// Package screen keeps a session's Ghostty screen without a window, renderer,
// input goroutines or a graphics dependency. Viewers answer terminal queries;
// this authoritative copy only retains output, modes, titles and scrollback.
package screen

import (
	"sync"

	"retty/internal/terminal/internal/library"
	"retty/internal/terminal/internal/library/desktop"
	"retty/internal/terminal/internal/vt"
)

type Screen struct {
	mu         sync.Mutex
	term       *vt.Terminal
	cols, rows int
}

func Load() error {
	if vt.Loaded() {
		return nil
	}
	path, err := library.Find(desktop.Manifest)
	if err != nil {
		return err
	}
	return vt.Load(path)
}

func New(cols, rows, scrollback int, bell func()) (*Screen, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	s := &Screen{cols: max(cols, 1), rows: max(rows, 1)}
	t, err := vt.NewTerminal(s.cols, s.rows, vt.Effects{
		Bell: bell,
		Size: func() (int, int, int, int) { return s.cols, s.rows, 8, 16 },
		Dark: func() bool { return true },
	})
	if err != nil {
		return nil, err
	}
	t.SetTerminfoName("xterm-256color")
	t.SetModeDefault(vt.ModeGraphemes, true)
	t.SetScrollback(scrollback)
	t.SetDefaultCursor(vt.CursorBlock, true)
	s.term = t
	return s, nil
}

func (s *Screen) Feed(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term != nil {
		s.term.Write(data)
	}
}

func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = max(cols, 1), max(rows, 1)
	if s.term != nil {
		s.term.Resize(s.cols, s.rows, 8, 16)
	}
}

func (s *Screen) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return nil
	}
	return s.term.VT()
}

func (s *Screen) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return ""
	}
	return s.term.Title()
}

func (s *Screen) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return ""
	}
	return s.term.Text()
}

func (s *Screen) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term != nil {
		s.term.Free()
		s.term = nil
	}
	return nil
}
