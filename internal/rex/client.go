package rex

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Client is a control connection to the server.
type Client struct {
	conn net.Conn
	dial func(context.Context) (net.Conn, error)

	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan Response
	err     error
	closed  chan struct{}
}

// Connect connects to the server, starting one in the background when
// none runs.
func Connect() (*Client, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		if err := Spawn(); err != nil {
			return nil, fmt.Errorf("rex: starting the server: %w", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			time.Sleep(30 * time.Millisecond)
			conn, err = net.Dial("unix", SocketPath())
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("rex: connecting to the server: %w", err)
			}
		}
	}
	return NewClient(conn, localDial), nil
}

func localDial(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", SocketPath())
}

// ConnectDial uses the same transport for control requests and session streams.
func ConnectDial(ctx context.Context, dial func(context.Context) (net.Conn, error)) (*Client, error) {
	conn, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(conn, dial), nil
}

// NewClient takes ownership of conn; dial opens independent session streams.
func NewClient(conn net.Conn, dial func(context.Context) (net.Conn, error)) *Client {
	c := &Client{conn: conn, dial: dial, pending: map[int64]chan Response{}, closed: make(chan struct{})}
	go c.read()
	return c
}

// Executable returns the path of the running executable and when it was
// modified last.
func Executable() (string, time.Time) {
	exe, err := os.Executable()
	if err != nil {
		return "", time.Time{}
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	st, err := os.Stat(exe)
	if err != nil {
		return exe, time.Time{}
	}
	return exe, st.ModTime()
}

// Restart shuts the server down, ending its sessions, waits for it to
// exit, and connects to a new one.
func (c *Client) Restart(pid int) (*Client, error) {
	c.Shutdown()
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for pid > 0 && processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	return Connect()
}

// Closed is closed once the connection to the server is lost.
func (c *Client) Closed() <-chan struct{} { return c.closed }

func (c *Client) read() {
	r := bufio.NewReaderSize(c.conn, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.pending {
				ch <- Response{ID: id, Error: "connection to the server lost"}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			close(c.closed)
			return
		}
		var res Response
		if json.Unmarshal(line, &res) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[res.ID]
		delete(c.pending, res.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	}
}

// call sends a request and decodes the response's data into out.
func (c *Client) call(req Request, out any) error {
	ch := make(chan Response, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return errors.New("rex: connection to the server lost")
	}
	c.nextID++
	req.ID = c.nextID
	c.pending[req.ID] = ch
	c.mu.Unlock()
	b, _ := json.Marshal(req)
	c.wmu.Lock()
	_, err := c.conn.Write(append(b, '\n'))
	c.wmu.Unlock()
	if err != nil {
		return err
	}
	var res Response
	select {
	case res = <-ch:
	case <-time.After(15 * time.Second):
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
		return fmt.Errorf("rex: %s timed out", req.Op)
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	if out != nil && len(res.Data) > 0 {
		return json.Unmarshal(res.Data, out)
	}
	return nil
}

func (c *Client) Hello() (h Hello, err error) {
	err = c.call(Request{Op: "hello"}, &h)
	if err == nil && h.Version == 4 && h.Host.ID == "" {
		h.Host.ID = legacyHostID(h.Host)
	}
	return
}

func (c *Client) HelloFrom(device DeviceInfo) (h Hello, err error) {
	err = c.call(Request{Op: "hello", Device: &device}, &h)
	if err == nil && h.Version == 4 && h.Host.ID == "" {
		h.Host.ID = legacyHostID(h.Host)
	}
	return
}

func (c *Client) List() (s []SessionInfo, err error) {
	err = c.call(Request{Op: "list"}, &s)
	return
}

func (c *Client) ListFrom(device DeviceInfo) (s []SessionInfo, err error) {
	err = c.call(Request{Op: "list", Device: &device}, &s)
	return
}

func (c *Client) Create(o CreateOptions) (s SessionInfo, err error) {
	err = c.call(Request{Op: "create", Create: &o}, &s)
	return
}

func (c *Client) Kill(sid string) error { return c.call(Request{Op: "kill", SID: sid}, nil) }

func (c *Client) Resize(sid string, cols, rows int) error {
	return c.call(Request{Op: "resize", SID: sid, Cols: cols, Rows: rows}, nil)
}

func (c *Client) Clear(sid string) error {
	return c.call(Request{Op: "clear", SID: sid}, nil)
}

func (c *Client) Layout() (json.RawMessage, error) {
	var l json.RawMessage
	err := c.call(Request{Op: "getLayout"}, &l)
	return l, err
}

func (c *Client) SetLayout(l json.RawMessage) error {
	return c.call(Request{Op: "setLayout", Layout: l}, nil)
}

func (c *Client) Shutdown() error { return c.call(Request{Op: "shutdown"}, nil) }

func (c *Client) Close() error { return c.conn.Close() }

// Stream is a session attached to, for a terminal's Conn: it attaches
// once it knows the size of the screen, from the first Resize, or after
// a moment at the size it had last.
type Stream struct {
	// preserveSize keeps this viewer's local layout from resizing the PTY.
	preserveSize                    bool
	screenFrames                    bool
	frameLeft, frameCols, frameRows int
	c                               *Client
	sid                             string
	attachMu                        sync.Mutex // one attach at a time
	syncMu                          sync.Mutex // preserve the order of resize requests
	mu                              sync.Mutex
	cond                            *sync.Cond
	conn                            net.Conn
	err                             error
	closed                          bool
	// cols and rows are the terminal's size; sentCols and sentRows the
	// size the server was told last.
	cols, rows         int
	sentCols, sentRows int
	resizeTimer        *time.Timer
	resizeAt           time.Time

	// OnData, when set, is called with the size of what the session
	// prints, from the reading goroutine.
	OnData func(n int)
}

// Stream returns a stream of the session sid, which attaches lazily.
func (c *Client) Stream(sid string, cols, rows int) *Stream {
	return c.stream(sid, cols, rows, false)
}

// ViewStream attaches an independent viewer without changing the session's
// PTY size. ReadScreen supplies the source grid with each output chunk on
// supporting servers; the viewer scales that grid instead of reflowing ANSI.
func (c *Client) ViewStream(sid string) *Stream {
	return c.stream(sid, 0, 0, true)
}

func (c *Client) stream(sid string, cols, rows int, preserveSize bool) *Stream {
	s := &Stream{c: c, sid: sid, cols: cols, rows: rows, preserveSize: preserveSize}
	s.cond = sync.NewCond(&s.mu)
	time.AfterFunc(400*time.Millisecond, func() {
		s.attach()
		s.sync()
	})
	return s
}

// attach attaches to the session at the terminal's size, once.
func (s *Stream) attach() {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	if s.conn != nil || s.err != nil || s.closed {
		s.mu.Unlock()
		return
	}
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dial := s.c.dial
	if dial == nil {
		dial = localDial
	}
	conn, err := dial(ctx)
	var frames bool
	if err == nil {
		conn.SetDeadline(time.Now().Add(15 * time.Second))
		b, _ := json.Marshal(Attach{Op: "attach", SID: s.sid, Cols: cols, Rows: rows, ScreenFrames: s.preserveSize})
		if _, err = conn.Write(append(b, '\n')); err == nil {
			frames, err = readAttachOK(conn)
		}
		conn.SetDeadline(time.Time{})
		if err != nil {
			conn.Close()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if err == nil {
			conn.Close()
		}
		return
	}
	if err != nil {
		s.err = err
	} else {
		s.conn, s.sentCols, s.sentRows, s.screenFrames = conn, cols, rows, frames
	}
	s.cond.Broadcast()
}

// sync tells the server the terminal's size, once attached, when it was
// told another.
func (s *Stream) sync() error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	if s.closed || s.preserveSize || s.conn == nil || s.cols == s.sentCols && s.rows == s.sentRows {
		s.mu.Unlock()
		return nil
	}
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if err := s.c.Resize(s.sid, cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	s.sentCols, s.sentRows = cols, rows
	s.mu.Unlock()
	return nil
}

// readOK reads the server's answer to an attach, a byte at a time so as
// to read nothing past it.
func readAttachOK(conn net.Conn) (bool, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := conn.Read(b); err != nil {
			return false, err
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
		if len(line) > 4096 {
			return false, errors.New("rex: bad answer")
		}
	}
	var res struct {
		ScreenFrames bool   `json:"screen_frames"`
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(line, &res); err != nil {
		return false, err
	}
	if !res.OK {
		return false, errors.New(res.Error)
	}
	return res.ScreenFrames, nil
}

func (s *Stream) wait() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.conn == nil && s.err == nil && !s.closed {
		s.cond.Wait()
	}
	if s.closed {
		return nil, io.EOF
	}
	return s.conn, s.err
}

// HasScreenSize reports whether this server supplies output geometry.
func (s *Stream) HasScreenSize() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screenFrames
}

func (s *Stream) Read(p []byte) (int, error) {
	n, _, _, err := s.ReadScreen(p)
	return n, err
}

// ReadSnapshot consumes the first complete screen frame on a fresh viewer.
// The caller is the stream's sole reader. Legacy servers return nil without
// consuming bytes and continue through ReadScreen. A failed partial transfer
// must never replace the viewer's last complete screen.
func (s *Stream) ReadSnapshot() ([]byte, int, int, error) {
	return s.readSnapshot(15 * time.Second)
}

func (s *Stream) readSnapshot(timeout time.Duration) ([]byte, int, int, error) {
	conn, err := s.wait()
	if err != nil {
		return nil, 0, 0, err
	}
	if !s.HasScreenSize() {
		return nil, 0, 0, nil
	}
	// A healthy control socket does not guarantee the viewer delivers its
	// snapshot. Bound the whole frame, then clear the deadline for idle live
	// sessions whose output may legitimately remain quiet indefinitely.
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, 0, 0, err
	}
	defer conn.SetReadDeadline(time.Time{})
	buf := make([]byte, 64<<10)
	n, cols, rows, err := s.ReadScreen(buf)
	if err != nil {
		return nil, 0, 0, err
	}
	data := make([]byte, n+s.frameLeft)
	copy(data, buf[:n])
	if _, err := io.ReadFull(conn, data[n:]); err != nil {
		return nil, 0, 0, err
	}
	s.frameLeft = 0
	return data, cols, rows, nil
}

// ReadScreen reads ANSI at its source size. Each frame carries geometry
// before its payload, so an in-flight resize cannot decode at another size.
// A legacy server returns zero geometry; callers use the session list size.
// Like Read, it must be called by a single reader.
func (s *Stream) ReadScreen(p []byte) (int, int, int, error) {
	if len(p) == 0 {
		return 0, 0, 0, nil
	}
	conn, err := s.wait()
	if err != nil {
		return 0, 0, 0, err
	}
	cols, rows := 0, 0
	if s.HasScreenSize() {
		if s.frameLeft == 0 {
			var header [8]byte
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				return 0, 0, 0, err
			}
			length := binary.BigEndian.Uint32(header[:4])
			cols, rows = int(binary.BigEndian.Uint16(header[4:6])), int(binary.BigEndian.Uint16(header[6:8]))
			if length == 0 || length > 64<<20 || cols == 0 || rows == 0 {
				return 0, 0, 0, errors.New("rex: invalid screen frame")
			}
			s.frameLeft, s.frameCols, s.frameRows = int(length), cols, rows
		}
		cols, rows = s.frameCols, s.frameRows
		p = p[:min(len(p), s.frameLeft)]
	}
	n, err := conn.Read(p)
	if s.HasScreenSize() {
		s.frameLeft -= n
	}
	if n > 0 && s.OnData != nil {
		s.OnData(n)
	}
	return n, cols, rows, err
}

func (s *Stream) Write(p []byte) (int, error) {
	conn, err := s.wait()
	if err != nil {
		return 0, err
	}
	return conn.Write(p)
}

// resizeDelay keeps window animations from making the shell redraw its
// prompt at every intermediate size, while the window is already drawing
// at the next one.
const resizeDelay = 75 * time.Millisecond

// Resize attaches at the first size and coalesces subsequent changes.
func (s *Stream) Resize(cols, rows int) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	if s.preserveSize {
		s.mu.Unlock()
		s.attach()
		return nil
	}
	cols, rows = max(cols, 1), max(rows, 1)
	attached := s.conn != nil
	if attached && s.cols == cols && s.rows == rows {
		s.mu.Unlock()
		return nil
	}
	s.cols, s.rows = cols, rows
	s.resizeAt = time.Now()
	if attached {
		if s.resizeTimer == nil {
			s.resizeTimer = time.AfterFunc(resizeDelay, s.resizeSettled)
		} else {
			s.resizeTimer.Reset(resizeDelay)
		}
	}
	s.mu.Unlock()
	if attached {
		return nil
	}
	s.attach()
	return s.sync()
}

func (s *Stream) resizeSettled() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if remaining := resizeDelay - time.Since(s.resizeAt); remaining > 0 {
		s.resizeTimer.Reset(remaining)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.sync()
}

// Close detaches; the session goes on.
func (s *Stream) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.resizeTimer != nil {
		s.resizeTimer.Stop()
	}
	conn := s.conn
	s.cond.Broadcast()
	s.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// Size returns the size the session was last given.
func (s *Stream) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}
