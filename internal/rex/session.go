//go:build darwin || linux

package rex

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"retty/internal/agents"
)

// scrollback is about how many bytes of output a session keeps above its
// screen, for the windows that attach to it.
const scrollback = 8 << 20

// session is a pseudo-terminal of the server and its shell.
type session struct {
	id            string
	shell         string
	created       time.Time
	p             *pty
	agentToken    string
	agent         agentTracker
	programStatus programStatusTracker

	mu sync.Mutex
	// vt is the session's screen, kept by the terminal emulator of the
	// app's terminals without a view: a window attaching gets a snapshot
	// of it at its size, as the session shows it now.
	vt         sessionScreen
	vtIn       io.Closer
	output     uint64
	bells      atomic.Int64
	clients    map[*attached]struct{}
	lastOutput time.Time
	lastInput  time.Time
	resync     *time.Timer
	resizeAt   time.Time
	prompt     promptTracker
	exited     bool
	code       int
	cols, rows int
	// sizeLock is the device that took the size with lockSize; while it
	// is set, only that lock's own resizes reach the PTY.
	sizeLock, sizeLockDevice string
	sizeLockConn             net.Conn
	sizeLockUntil            time.Time
	sizeLockTimer            *time.Timer
	done                     chan struct{}
}

// attached is a connection attached to a session, whose output a
// goroutine writes so that a slow client never holds up the others.
type attached struct {
	conn         net.Conn
	out          chan []byte
	once         sync.Once
	screenFrames bool
}

func (a *attached) send(p []byte) bool { return a.sendScreen(p, 0, 0) }

func (a *attached) sendScreen(p []byte, cols, rows int) bool {
	if a.screenFrames {
		frame := make([]byte, 8+len(p))
		binary.BigEndian.PutUint32(frame[:4], uint32(len(p)))
		binary.BigEndian.PutUint16(frame[4:6], uint16(cols))
		binary.BigEndian.PutUint16(frame[6:8], uint16(rows))
		copy(frame[8:], p)
		p = frame
	}
	select {
	case a.out <- p:
		return true
	default:
		return false // too far behind: drop it, it attaches again
	}
}

func (a *attached) finish() { a.once.Do(func() { close(a.out) }) }

func (a *attached) writer() {
	for p := range a.out {
		if _, err := a.conn.Write(p); err != nil {
			break
		}
	}
	a.conn.Close()
	for range a.out {
	}
}

func newSession(id string, o CreateOptions) (*session, error) {
	return newSessionForServer(id, o, "")
}

func newSessionForServer(id string, o CreateOptions, serverToken string) (*session, error) {
	path, argv, name, err := shellCommand(o.Command)
	if err != nil {
		return nil, err
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	token := newID() + newID()
	env := sessionEnv(id, o.Env)
	exe, _ := os.Executable()
	env = append(env, "RETTY_SESSION="+id, "RETTY_AGENT_TOKEN="+token, "RETTY_AGENT_SOCKET="+SocketPath(), "RETTY_HOOK="+exe)
	env = append(env, "RETTY_AGENT_SERVER_TOKEN="+serverToken)
	env, codexDir, err := codexEnv(env, agents.InspectHooks("codex").Installed)
	if err != nil {
		return nil, err
	}
	if name == "codex" {
		env = append(env, "RETTY_CODEX_EXE="+path)
		path = filepath.Join(codexDir, "codex")
	}
	env, shellDir, err := zshEnv(path, env)
	if err != nil {
		os.RemoveAll(codexDir)
		return nil, err
	}
	cleanupShell := func() {
		os.RemoveAll(codexDir)
		if shellDir != "" {
			os.RemoveAll(shellDir)
		}
	}
	p, err := startPTY(path, argv, dir, env, cols, rows)
	if err != nil {
		cleanupShell()
		return nil, err
	}
	s := &session{
		id: id, shell: name, created: time.Now(), p: p, agentToken: token,
		clients: map[*attached]struct{}{},
		cols:    cols, rows: rows, done: make(chan struct{}),
	}
	// The emulator's Conn reads nothing, and what it would answer programs
	// is dropped: the windows' terminals answer them.
	vt, input, err := newSessionScreen(cols, rows, func() { s.bells.Add(1) })
	if err != nil {
		p.hangup()
		cleanupShell()
		return nil, err
	}
	vt.Resize(cols, rows)
	if name == "zsh" {
		vt.Feed([]byte(promptRedraw))
	}
	s.vt, s.vtIn = vt, input
	read := make(chan struct{})
	go func() {
		s.read()
		close(read)
	}()
	go func() {
		code := p.wait()
		cleanupShell()
		// What it printed last is still to read; then hang up whatever
		// else holds the terminal, as terminal apps do.
		select {
		case <-read:
		case <-time.After(150 * time.Millisecond):
			p.master.Close()
			<-read
		}
		s.mu.Lock()
		s.exited, s.code = true, code
		s.programStatus.processExit(time.Now())
		s.clearSizeLockLocked()
		for a := range s.clients {
			a.finish()
		}
		clear(s.clients)
		s.mu.Unlock()
		close(s.done)
		s.vtIn.Close()
		s.vt.Close()
	}()
	return s, nil
}

// sessionEnv returns the environment of a session's shell: the server's,
// without what other terminals set, and what terminal apps set.
func sessionEnv(id string, extra []string) []string {
	var env []string
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "TERM", k == "COLORTERM", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION",
			k == "TERM_SESSION_ID", k == "TMUX", k == "TMUX_PANE", k == "STY", k == "WINDOWID",
			k == "COLUMNS", k == "LINES", k == "SHLVL", k == "OLDPWD", k == "PWD", k == "_",
			strings.HasPrefix(k, "ITERM_"), strings.HasPrefix(k, "GHOSTTY_"), strings.HasPrefix(k, "KITTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "WEZTERM_"), strings.HasPrefix(k, "ALACRITTY_"),
			strings.HasPrefix(k, "RETTY_"), strings.HasPrefix(k, "MYGO_"):
			continue
		case k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE":
			hasLang = true
		}
		env = append(env, kv)
	}
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=Retty",
		"TERM_PROGRAM_VERSION=0.1.0",
		"RETTY_SESSION="+id,
	)
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env, extra...)
}

func (s *session) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := s.p.master.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.programStatus.serial = max(s.programStatus.serial, s.agent.state.WaitRevision, s.agent.state.CompletionRevision)
			replies := s.programStatus.feed(data, time.Now())
			s.mu.Unlock()
			// Answer before delivering the following DA query to viewers. This
			// works with no viewer attached and never counts as a user edit.
			for range replies {
				s.p.master.Write([]byte(programStatusReply))
			}
			s.mu.Lock()
			s.prompt.feed(data)
			s.vt.Feed(data)
			s.output += uint64(n)
			s.lastOutput = time.Now()
			for a := range s.clients {
				if !a.sendScreen(data, s.cols, s.rows) {
					a.finish()
					delete(s.clients, a)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// attach attaches a connection: it gets a snapshot of the session's
// screen and scrollback at its size, then what the session prints, and
// what it sends is typed.
func (s *session) attach(conn net.Conn, cols, rows int) { s.attachScreen(conn, cols, rows, false) }

func (s *session) attachScreen(conn net.Conn, cols, rows int, frames bool) {
	a := &attached{conn: conn, out: make(chan []byte, 2048), screenFrames: frames}
	s.mu.Lock()
	if cols > 0 && rows > 0 && s.sizeLock == "" {
		// Resize both the screen and PTY before queuing the snapshot.
		s.resizeLocked(cols, rows)
	}
	snap := s.vt.Snapshot()
	if s.shell == "zsh" {
		snap = append([]byte(promptRedraw), snap...)
	}
	if title := s.vt.Title(); title != "" {
		snap = append([]byte("\x1b]2;"+title+"\x07"), snap...)
	}
	a.sendScreen(snap, s.cols, s.rows)
	exited := s.exited
	if exited {
		a.finish()
	} else {
		s.clients[a] = struct{}{}
	}
	s.mu.Unlock()
	go a.writer()
	if exited {
		return
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.input(buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	if _, ok := s.clients[a]; ok {
		delete(s.clients, a)
		a.finish()
	}
	s.mu.Unlock()
}

func (s *session) input(p []byte) {
	s.mu.Lock()
	s.lastInput = time.Now()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.master.Write(p)
	}
}

// resize sets the size of the terminal and reports whether it changed.
// A window's resize is ignored while a phone holds the size lock: one PTY
// has one size, and the desktop must not take it back by redrawing.
func (s *session) resize(cols, rows int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSizeLockLocked(time.Now())
	if s.sizeLock != "" {
		return false
	}
	return s.resizeLocked(cols, rows)
}

// lockSize gives the terminal's size to this attachment. Leased owners must
// renew while active; a disconnected attachment releases even a legacy lock.
func (s *session) lockSize(owner, device string, cols, rows int, conn net.Conn, leased bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearSizeLockLocked()
	s.sizeLock, s.sizeLockDevice = owner, device
	s.sizeLockConn = conn
	if leased {
		s.renewSizeLockLocked()
	}
	if cols > 0 && rows > 0 {
		s.resizeLocked(cols, rows)
	}
}

// lockedResize resizes for the device holding the lock, and reports
// whether it still does.
func (s *session) lockedResize(owner string, cols, rows int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSizeLockLocked(time.Now())
	if s.sizeLock != owner {
		return false
	}
	s.resizeLocked(cols, rows)
	return true
}

// unlockSize releases the lock and gives the size back to the windows,
// at the size given (that of the window unlocking), if any.
func (s *session) unlockSize(cols, rows int) {
	s.unlockSizeOwned("", cols, rows)
}

// An inactive attachment must not release a newer device's lock.
func (s *session) unlockSizeOwned(owner string, cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != "" && s.sizeLock != owner {
		return
	}
	s.clearSizeLockLocked()
	if cols > 0 && rows > 0 {
		s.resizeLocked(cols, rows)
	}
}

// releaseSizeAttachment checks the connection too: an old attachment closing
// must not release a newer attachment, even if it reused the same owner ID.
func (s *session) releaseSizeAttachment(owner string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != "" && s.sizeLock == owner && s.sizeLockConn == conn {
		s.clearSizeLockLocked()
	}
}

func (s *session) renewSizeLock(owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSizeLockLocked(time.Now())
	if owner == "" || s.sizeLock != owner || s.sizeLockUntil.IsZero() {
		return false
	}
	s.renewSizeLockLocked()
	return true
}

func (s *session) renewSizeLockLocked() {
	s.sizeLockUntil = time.Now().Add(sizeLockLease)
	if s.sizeLockTimer == nil {
		s.sizeLockTimer = time.AfterFunc(sizeLockLease, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.expireSizeLockLocked(time.Now())
		})
	} else {
		s.sizeLockTimer.Reset(sizeLockLease)
	}
}

func (s *session) expireSizeLockLocked(now time.Time) {
	// An old timer callback may already be waiting for mu when a renewal or
	// replacement arrives. Its deadline must not clear the renewed owner.
	if !s.sizeLockUntil.IsZero() && !now.Before(s.sizeLockUntil) {
		s.clearSizeLockLocked()
	}
}

func (s *session) clearSizeLockLocked() {
	if s.sizeLockTimer != nil {
		s.sizeLockTimer.Stop()
		s.sizeLockTimer = nil
	}
	s.sizeLock, s.sizeLockDevice = "", ""
	s.sizeLockConn, s.sizeLockUntil = nil, time.Time{}
}

func (s *session) resizeLocked(cols, rows int) bool {
	cols, rows = clamp(cols), clamp(rows)
	oldCols := s.cols
	changed := cols != s.cols || rows != s.rows
	s.cols, s.rows = cols, rows
	exited := s.exited
	if changed {
		s.resizeAt = time.Now()
		// Clear a marked prompt at its old width before reflow. A right
		// prompt's padding would otherwise wrap into extra prompt rows.
		if s.prompt.active && cols != oldCols {
			s.vt.Resize(oldCols, rows+1)
		}
		s.vt.Resize(cols, rows)
		// Once the program drew its screen at the new size, the windows
		// get it again.
		if s.resync == nil {
			s.resync = time.AfterFunc(resyncDelay, s.resyncScreen)
		} else {
			s.resync.Reset(resyncDelay)
		}
	}
	if changed && !exited {
		setSize(s.p.master, cols, rows)
	}
	return changed
}

// resyncDelay is how long after the last resize the screen of a
// full-screen program is sent again.
const resyncDelay = 150 * time.Millisecond

// libghostty's C API defaults to preserving prompts on resize. Opt in
// for zsh, which redraws its full prompt on SIGWINCH.
const promptRedraw = "\x1b]133;A;redraw=1\x07\x1b]133;C\x07"

// resyncScreen sends the windows the session's screen after resizing.
// A window resizes its terminal as it draws, and the
// session as the request reaches it, while the program's output is on its
// way: what the program drew for one size may land in a screen of
// another, which full-screen programs, redrawing only what changed, do
// not mend. A shell's right prompt can likewise wrap differently while
// the window reflows through an animation's intermediate sizes.
func (s *session) resyncScreen() { s.resend(false) }

// resend sends the windows the session's screen; force replaces a
// primary screen too, for a window that changed the size it decodes at.
func (s *session) resend(force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || len(s.clients) == 0 {
		return
	}
	if !s.resizeAt.IsZero() {
		if remaining := resyncDelay - time.Since(s.resizeAt); remaining > 0 {
			s.resync.Reset(remaining)
			return
		}
	}
	snap := s.vt.Snapshot()
	var msg []byte
	if bytes.Contains(snap, []byte("\x1b[?1049h")) {
		// Clear the alternate screen the window shows, then draw it anew.
		msg = append([]byte("\x1b[?1049h\x1b[H\x1b[2J"), snap...)
	} else if s.shell == "zsh" {
		// Replace the primary screen and history, rather than appending
		// a snapshot to the window's independently reflowed scrollback.
		msg = append([]byte("\x1b[?6l\x1b[r\x1b[H\x1b[2J\x1b[3J"+promptRedraw), snap...)
	} else if force {
		msg = append([]byte("\x1b[?6l\x1b[r\x1b[H\x1b[2J\x1b[3J"), snap...)
	} else {
		return
	}
	// A socket can split this replacement between its clear-screen prefix
	// and the snapshot. Keep the previous frame visible until all of it
	// arrives, rather than briefly drawing an empty or partial screen.
	msg = append(append([]byte("\x1b[?2026h"), msg...), []byte("\x1b[?2026l")...)
	for a := range s.clients {
		if !a.sendScreen(msg, s.cols, s.rows) {
			a.finish()
			delete(s.clients, a)
		}
	}
}

// clear keeps the authoritative screen in step with Command-K.
func (s *session) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := []byte("\x1b[H\x1b[2J\x1b[3J")
	s.vt.Feed(data)
	for a := range s.clients {
		if !a.sendScreen(data, s.cols, s.rows) {
			a.finish()
			delete(s.clients, a)
		}
	}
}

// kill hangs up the session and waits a little for it to end.
func (s *session) kill() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		s.p.hangup()
	}
	select {
	case <-s.done:
	case <-time.After(time.Second):
		if pr := s.p.cmd.Process; pr != nil {
			syscall.Kill(-pr.Pid, syscall.SIGKILL)
		}
	}
}

// info describes the session and what runs in its foreground.
func (s *session) info() SessionInfo {
	s.mu.Lock()
	s.expireSizeLockLocked(time.Now())
	in := SessionInfo{
		ID: s.id, Shell: s.shell, Created: s.created,
		Title: s.vt.Title(), LastOutput: s.lastOutput, LastInput: s.lastInput,
		Output: s.output, Bells: int(s.bells.Load()), Exited: s.exited, ExitCode: s.code,
		Attached: len(s.clients), Cols: s.cols, Rows: s.rows,
		SizeLock: s.sizeLock, SizeLockDevice: s.sizeLockDevice, Agent: s.agent.state,
		ProgramStatuses: s.programStatus.snapshot(),
	}
	if len(in.ProgramStatuses) > 0 {
		in.Agent = s.programStatus.agentState("")
	} else if !s.programStatus.lastReport.IsZero() {
		// Cleared protocol records must not resurrect an older hook badge.
		in.Agent = AgentState{}
	}
	s.mu.Unlock()
	if s.p.cmd.Process != nil {
		in.PID = s.p.cmd.Process.Pid
	}
	if in.Exited {
		return in
	}
	fg := s.p.foreground()
	if fg <= 0 {
		fg = in.PID
	}
	in.Idle = fg == in.PID
	// A crashed/killed agent cannot emit SessionEnd. Do not leave its
	// last waiting state on a shell prompt. Allow startup hooks to settle.
	if in.Idle && in.Agent.Source != ProgramStatusSource && time.Since(in.Agent.Updated) > 2*time.Second {
		in.Agent = AgentState{}
	}
	p := inspect(fg)
	in.Program, in.Args, in.Dir = p.programName(), p.args, p.dir
	if in.Program == "" {
		in.Program = s.shell
	}
	if in.Dir == "" && !in.Idle {
		in.Dir = inspect(in.PID).dir
	}
	return in
}

// discard is the Conn of a session's emulator: it reads from a pipe that
// closes with the session, and drops what it writes.
type discard struct{ r *io.PipeReader }

func (d discard) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d discard) Write(p []byte) (int, error) { return len(p), nil }
func (d discard) Close() error                { return d.r.Close() }
