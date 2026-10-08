//go:build darwin || linux

package rex

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Server owns the sessions.
type Server struct {
	mu         sync.Mutex
	agentMu    sync.Mutex // serialize shared-daemon session binding and reports
	agentToken string
	sessions   map[string]*session
	order      []string
	layout     json.RawMessage
	started    time.Time
	host       HostInfo
	hostDone   chan struct{}
	ln         net.Listener
	exe        string
	exeTime    time.Time
	controls   int
	idleFrom   time.Time
	quit       chan struct{}
	quitOnce   sync.Once
}

// idleTimeout is how long a server with no sessions and no app waits
// before it exits.
const idleTimeout = 10 * time.Minute

// Serve runs the server until it is shut down: it returns an error at
// once when another server already runs.
func Serve() error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("rex: a server already runs")
	}
	defer lock.Close()
	agentToken, err := loadAgentToken(dir)
	if err != nil {
		return err
	}
	deviceID, err := loadDeviceID(dir)
	if err != nil {
		return err
	}
	sock := SocketPath()
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	os.Chmod(sock, 0o600)
	s := &Server{
		agentToken: agentToken,
		sessions:   map[string]*session{}, started: time.Now(), ln: ln,
		hostDone: make(chan struct{}), idleFrom: time.Now(), quit: make(chan struct{}),
	}
	if b, err := os.ReadFile(filepath.Join(dir, "layout.json")); err == nil && json.Valid(b) {
		s.layout = b
	}
	s.exe, s.exeTime = Executable()
	go func() {
		s.host = hostInfo()
		if s.host.ID == "" {
			s.host.ID = "installation:" + deviceID
		}
		close(s.hostDone)
	}()
	go s.watchIdle()
	go func() {
		<-s.quit
		ln.Close()
	}()
	log.Printf("rex: serving on %s (pid %d)", sock, os.Getpid())
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.mu.Lock()
				all := make([]*session, 0, len(s.sessions))
				for _, ss := range s.sessions {
					all = append(all, ss)
				}
				s.mu.Unlock()
				for _, ss := range all {
					ss.kill()
				}
				os.Remove(sock)
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) watchIdle() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.mu.Lock()
		idle := len(s.sessions) == 0 && s.controls == 0 && time.Since(s.idleFrom) > idleTimeout
		s.mu.Unlock()
		if idle {
			log.Print("rex: idle, exiting")
			s.shutdown()
			return
		}
	}
}

func (s *Server) shutdown() { s.quitOnce.Do(func() { close(s.quit) }) }

func (s *Server) handle(conn net.Conn) {
	r := bufio.NewReaderSize(conn, 64<<10)
	first, err := r.ReadBytes('\n')
	if err != nil {
		conn.Close()
		return
	}
	var a Attach
	if json.Unmarshal(first, &a) == nil && a.Op == "attach" {
		s.mu.Lock()
		ss := s.sessions[a.SID]
		s.mu.Unlock()
		if ss == nil {
			conn.Write([]byte(`{"error":"no such session"}` + "\n"))
			conn.Close()
			return
		}
		if a.ScreenFrames {
			conn.Write([]byte(`{"ok":true,"screen_frames":true}` + "\n"))
		} else {
			conn.Write([]byte(`{"ok":true}` + "\n"))
		}
		// What the reader buffered past the first line is input.
		if n := r.Buffered(); n > 0 {
			b, _ := r.Peek(n)
			ss.input(b)
		}
		ss.attachScreen(conn, a.Cols, a.Rows, a.ScreenFrames)
		return
	}
	s.control(conn, r, first)
}

// control serves a control connection: a request per line.
func (s *Server) control(conn net.Conn, r *bufio.Reader, first []byte) {
	s.mu.Lock()
	s.controls++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.controls--
		s.idleFrom = time.Now()
		s.mu.Unlock()
		conn.Close()
	}()
	var wmu sync.Mutex
	reply := func(res Response) {
		b, _ := json.Marshal(res)
		wmu.Lock()
		conn.Write(append(b, '\n'))
		wmu.Unlock()
	}
	line := first
	for {
		var req Request
		if err := json.Unmarshal(line, &req); err == nil {
			// Kills wait for the session to end: answer them aside.
			if req.Op == "kill" {
				go func() { reply(s.request(req)) }()
			} else {
				reply(s.request(req))
			}
		}
		var err error
		line, err = r.ReadBytes('\n')
		if err != nil {
			return
		}
	}
}

func (s *Server) request(req Request) Response {
	data, err := s.do(req)
	res := Response{ID: req.ID}
	if err != nil {
		res.Error = err.Error()
	} else if data != nil {
		res.Data, _ = json.Marshal(data)
	}
	return res
}

func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss := s.sessions[id]; ss != nil {
		return ss, nil
	}
	return nil, fmt.Errorf("no session %q", id)
}

func (s *Server) do(req Request) (any, error) {
	switch req.Op {
	case "hello":
		select {
		case <-s.hostDone:
		case <-time.After(3 * time.Second):
		}
		return Hello{Version: ProtocolVersion, PID: os.Getpid(), Started: s.started, Host: s.host, Exe: s.exe, ExeTime: s.exeTime}, nil
	case "list":
		s.mu.Lock()
		all := make([]*session, 0, len(s.order))
		for _, id := range s.order {
			all = append(all, s.sessions[id])
		}
		s.mu.Unlock()
		infos := make([]SessionInfo, len(all))
		for i, ss := range all {
			infos[i] = ss.info()
		}
		return infos, nil
	case "create":
		o := CreateOptions{}
		if req.Create != nil {
			o = *req.Create
		}
		id := newID()
		ss, err := newSessionForServer(id, o, s.agentToken)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.sessions[id] = ss
		s.order = append(s.order, id)
		s.mu.Unlock()
		return ss.info(), nil
	case "kill":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, nil // already gone
		}
		ss.kill()
		s.mu.Lock()
		delete(s.sessions, req.SID)
		s.order = slices.DeleteFunc(s.order, func(id string) bool { return id == req.SID })
		if len(s.sessions) == 0 {
			s.idleFrom = time.Now()
		}
		s.mu.Unlock()
		return nil, nil
	case "resize":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.resize(req.Cols, req.Rows)
		return nil, nil
	case "clear":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.clear()
		return nil, nil
	case "agentEvent":
		return nil, s.reportAgent(req)
	case "getLayout":
		s.mu.Lock()
		l := s.layout
		s.mu.Unlock()
		if l == nil {
			return nil, nil
		}
		return l, nil
	case "setLayout":
		if !json.Valid(req.Layout) {
			return nil, errors.New("layout is not JSON")
		}
		l := bytes.Clone(req.Layout)
		s.mu.Lock()
		s.layout = l
		s.mu.Unlock()
		path := filepath.Join(Dir(), "layout.json")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, l, 0o600); err == nil {
			os.Rename(tmp, path)
		}
		return nil, nil
	case "shutdown":
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.shutdown()
		}()
		return nil, nil
	}
	return nil, fmt.Errorf("unknown op %q", req.Op)
}

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// hostInfo describes this machine; it may take a second.
func hostInfo() HostInfo {
	h := HostInfo{}
	if u, err := user.Current(); err == nil {
		h.User = u.Username
		h.Home = u.HomeDir
	}
	out := func(name string, args ...string) string {
		b, err := exec.Command(name, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if runtime.GOOS == "darwin" {
		for _, line := range strings.Split(out("ioreg", "-rd1", "-c", "IOPlatformExpertDevice"), "\n") {
			if _, value, ok := strings.Cut(line, `"IOPlatformUUID" = `); ok {
				h.ID = deviceFingerprint(strings.Trim(value, `"`))
				break
			}
		}
		h.Name = out("scutil", "--get", "ComputerName")
		h.OS = "macOS " + out("sw_vers", "-productVersion")
		h.Chip = sysctlString("machdep.cpu.brand_string")
		h.Memory = sysctlUint64("hw.memsize")
		var hw struct {
			Data []struct {
				MachineName string `json:"machine_name"`
				ChipType    string `json:"chip_type"`
			} `json:"SPHardwareDataType"`
		}
		if json.Unmarshal([]byte(out("system_profiler", "SPHardwareDataType", "-json", "-detailLevel", "mini")), &hw) == nil && len(hw.Data) > 0 {
			h.Model = hw.Data[0].MachineName
			if hw.Data[0].ChipType != "" {
				h.Chip = hw.Data[0].ChipType
			}
		}
		if h.Model == "" {
			h.Model = sysctlString("hw.model")
		}
	} else {
		if data, err := os.ReadFile("/etc/machine-id"); err == nil {
			h.ID = deviceFingerprint(string(data))
		}
		h.Name, _ = os.Hostname()
		h.OS = runtime.GOOS
		if b, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
					h.OS, _ = strconv.Unquote(v)
				}
			}
		}
		h.Model = "Linux"
	}
	if h.Name == "" {
		h.Name, _ = os.Hostname()
		h.Name = strings.TrimSuffix(h.Name, ".local")
	}
	return h
}

// Spawn starts a server in the background, as a process of its own that
// outlives the app: the executable run with -server.
func Spawn() error {
	return StartBackground([]string{"-server"}, Dir(), "server.log")
}
