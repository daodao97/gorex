package main

import (
	"io"
	"sync"
)

// Keep the terminal reader alive across transports. Paused input is discarded,
// never queued for replay: a partially written shell command is not retryable.
type mobileTransport interface {
	io.ReadWriteCloser
	ReadScreen([]byte) (int, int, int, error)
	Resize(int, int) error
	HasScreenSize() bool
}

type mobileStream struct {
	mu         sync.Mutex
	cond       *sync.Cond
	current    mobileTransport
	cols, rows int
	reset      bool
	paused     bool
	closed     bool
	failed     func()
}

func newMobileStream(stream mobileTransport, failed func()) *mobileStream {
	_, initialSnapshot := stream.(interface {
		ReadSnapshot() ([]byte, int, int, error)
	})
	s := &mobileStream{current: stream, failed: failed, reset: initialSnapshot, paused: initialSnapshot}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *mobileStream) replace(next mobileTransport) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		if next != nil {
			next.Close()
		}
		return
	}
	old := s.current
	s.current = next
	s.reset = next != nil
	s.paused = true
	s.cond.Broadcast()
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

func (s *mobileStream) failure(stream mobileTransport) {
	s.mu.Lock()
	active := !s.closed && s.current == stream
	if active {
		s.current = nil
	}
	s.mu.Unlock()
	if active {
		stream.Close()
		if s.failed != nil {
			s.failed()
		}
	}
}

func (s *mobileStream) ReadScreen(p []byte) (int, int, int, error) {
	n, cols, rows, _, err := s.readScreen(p, false)
	return n, cols, rows, err
}

// Terminal readers can apply a complete replacement atomically. Ordinary
// readers retain the legacy byte-stream behavior.
func (s *mobileStream) ReadScreenUpdate(p []byte) (int, int, int, []byte, error) {
	return s.readScreen(p, true)
}

func (s *mobileStream) readScreen(p []byte, snapshots bool) (int, int, int, []byte, error) {
	if len(p) == 0 {
		return 0, 0, 0, nil, nil
	}
	for {
		s.mu.Lock()
		for s.current == nil && !s.closed {
			s.cond.Wait()
		}
		stream, closed := s.current, s.closed
		sourceCols, sourceRows := s.cols, s.rows
		reset := s.reset
		s.reset = false
		s.mu.Unlock()
		if closed {
			return 0, 0, 0, nil, io.EOF
		}
		if snapshots && reset {
			if source, ok := stream.(interface {
				ReadSnapshot() ([]byte, int, int, error)
			}); ok {
				data, cols, rows, err := source.ReadSnapshot()
				s.mu.Lock()
				active := s.current == stream && !s.closed
				if active && err == nil && len(data) > 0 {
					s.paused = false
				}
				s.mu.Unlock()
				if !active {
					continue
				}
				if err != nil {
					s.failure(stream)
					continue
				}
				if len(data) > 0 {
					return 0, cols, rows, data, nil
				}
			}
		}
		prefix := ""
		if reset {
			prefix = "\x18\x1b[?6l\x1b[r\x1b[H\x1b[2J\x1b[3J"
		}
		// Terminal reads use a large buffer; a reset and snapshot start
		// together so a failed attach cannot erase the retained frame.
		if len(prefix) >= len(p) {
			s.mu.Lock()
			s.reset = true
			s.mu.Unlock()
			return 0, 0, 0, nil, io.ErrShortBuffer
		}
		n, cols, rows, err := stream.ReadScreen(p[len(prefix):])
		s.mu.Lock()
		active := s.current == stream && !s.closed
		if active && n > 0 {
			s.paused = false
		}
		s.mu.Unlock()
		if !active {
			continue
		}
		if n > 0 && prefix != "" {
			copy(p, prefix)
			n += len(prefix)
		}
		if cols == 0 || rows == 0 {
			cols, rows = sourceCols, sourceRows
		}
		if err != nil {
			s.failure(stream)
		}
		if n > 0 || err == nil {
			return n, cols, rows, nil, nil
		}
	}
}

func (s *mobileStream) Read(p []byte) (int, error) {
	n, _, _, err := s.ReadScreen(p)
	return n, err
}

func (s *mobileStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	stream, closed, paused := s.current, s.closed, s.paused
	s.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if stream != nil && !paused {
		_, err := stream.Write(p)
		if err != nil {
			s.failure(stream)
		}
	}
	// The terminal input worker must survive the gap. Controls are disabled
	// during reconnection and input reaching a failed transport is not replayed.
	return len(p), nil
}

func (s *mobileStream) Resize(cols, rows int) error {
	s.mu.Lock()
	stream := s.current
	s.mu.Unlock()
	if stream != nil {
		return stream.Resize(cols, rows)
	}
	return nil
}

func (s *mobileStream) HasScreenSize() bool {
	s.mu.Lock()
	stream := s.current
	s.mu.Unlock()
	return stream != nil && stream.HasScreenSize()
}

func (s *mobileStream) hasTransport() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.current != nil
}

func (s *mobileStream) inputReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.current != nil && !s.paused
}

func (s *mobileStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	old := s.current
	s.current = nil
	s.cond.Broadcast()
	s.mu.Unlock()
	if old != nil {
		return old.Close()
	}
	return nil
}

func (s *mobileStream) geometry(cols, rows int) {
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
}
