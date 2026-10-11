//go:build darwin || linux

package rex

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"retty/internal/agents"
)

// agentResume contains only lifecycle/launch metadata. Never save prompts,
// transcripts, arbitrary shell commands, credentials or temporary relay sockets.
type agentResume struct {
	Agent     string   `json:"agent"`
	SessionID string   `json:"sessionID"`
	Dir       string   `json:"dir"`
	Options   []string `json:"options,omitempty"`
	Env       []string `json:"env,omitempty"`
	Error     string   `json:"error,omitempty"`
	pid       int
	seen      time.Time
}

func recoveryEnv(kv string) bool {
	k, _, _ := strings.Cut(kv, "=")
	return k == "CODEX_HOME" || k == "CLAUDE_CONFIG_DIR"
}

func resumeConfigEnv(agent string, config *string, inherited []string) []string {
	key := "CLAUDE_CONFIG_DIR"
	if agent == "codex" {
		key = "CODEX_HOME"
	}
	if config != nil {
		return []string{key + "=" + *config}
	}
	// Compatibility with lifecycle events from older hook launchers.
	var env []string
	for _, kv := range inherited {
		if strings.HasPrefix(kv, key+"=") {
			env = []string{kv}
		}
	}
	return env
}

// Value options are deliberately allowlisted. Unknown options stop parsing so
// their values (which might contain a prompt) cannot be mistaken for options.
func resumeValueOptions(agent string) []string {
	if agent == "codex" {
		return []string{"-m", "--model", "-p", "--profile", "-s", "--sandbox", "-a", "--ask-for-approval"}
	}
	return []string{"--model", "--effort", "--permission-mode"}
}

func resumeOptions(agent string, argv []string) (out []string, supported bool) {
	for _, arg := range argv {
		if arg == "--no-session-persistence" || arg == "--ephemeral" {
			return nil, false
		}
	}
	// Skip only a recognized launcher entry, not later prompt text.
	start := 1
	if len(argv) > 1 && interpreters[filepath.Base(argv[0])] {
		start = 2
	}
	for i := start; i < len(argv); i++ {
		arg, value, equal := strings.Cut(argv[i], "=")
		if arg == "--no-session-persistence" || arg == "--ephemeral" || arg == "-p" && agent == "claude" || arg == "--print" {
			return nil, false
		}
		if slices.Contains(resumeValueOptions(agent), arg) {
			if !equal {
				i++
				if i >= len(argv) {
					return out, true
				}
				value = argv[i]
			}
			out = append(out, arg, value)
			continue
		}
		// These are launch transport/cwd selectors, not reusable configuration.
		if arg == "--remote" || arg == "-C" || arg == "--cd" || arg == "-r" || arg == "--resume" {
			if !equal && i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
			}
			continue
		}
		if agent == "codex" && (arg == "--no-daemon" || arg == "--full-auto") {
			out = append(out, arg)
			continue
		}
		if arg == "resume" && agent == "codex" {
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
			}
			continue
		}
		if arg == "--continue" || arg == "-c" && agent == "claude" || arg == "--last" || arg == "--all" {
			continue
		}
		break
	}
	return out, true
}

func (r agentResume) command() ([]string, error) {
	if r.Agent != "codex" && r.Agent != "claude" {
		return nil, errors.New("unsupported Agent")
	}
	if r.SessionID == "" || len(r.SessionID) > 128 || strings.HasPrefix(r.SessionID, "-") ||
		strings.IndexFunc(r.SessionID, func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_')
		}) >= 0 {
		return nil, errors.New("invalid Agent conversation ID")
	}
	if !filepath.IsAbs(r.Dir) {
		return nil, errors.New("Agent working directory must be absolute")
	}
	st, err := os.Stat(r.Dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("Agent working directory is not a directory")
	}
	// Validate persisted options as well; never allow extra positional input.
	for i := 0; i < len(r.Options); i++ {
		option := r.Options[i]
		if slices.Contains(resumeValueOptions(r.Agent), option) {
			i++
			if i >= len(r.Options) {
				return nil, errors.New("missing Agent option value")
			}
		} else if r.Agent != "codex" || option != "--no-daemon" && option != "--full-auto" {
			return nil, errors.New("unsupported Agent resume option")
		}
	}
	for _, kv := range r.Env {
		if !recoveryEnv(kv) {
			return nil, errors.New("unsupported Agent environment")
		}
	}
	cmd := []string{r.Agent}
	if r.Agent == "codex" {
		cmd = append(cmd, "resume")
	} else {
		cmd = append(cmd, "--resume")
	}
	cmd = append(cmd, r.SessionID)
	cmd = append(cmd, r.Options...)
	if r.Agent == "codex" {
		cmd = append(cmd, "-C", r.Dir)
	}
	return cmd, nil
}

type recoveryFile struct {
	Version  int                    `json:"version"`
	Sessions map[string]agentResume `json:"sessions"`
}

// All access is serialized by Server.agentMu, including process creation and
// explicit termination. The disk record is authoritative, not a stale layout.
func (s *Server) loadRecovery(dir string) error {
	s.recoveryPath = filepath.Join(dir, "agent-resume.json")
	s.recovery = map[string]agentResume{}
	b, err := os.ReadFile(s.recoveryPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var file recoveryFile
	if err := json.Unmarshal(b, &file); err != nil {
		return err
	}
	if file.Version != 1 {
		return errors.New("unsupported Agent recovery file version")
	}
	if file.Sessions != nil {
		s.recovery = file.Sessions
	}
	return nil
}

func (s *Server) saveRecovery() error {
	if s.recoveryPath == "" {
		return nil
	} // in-memory server fixtures
	b, err := json.Marshal(recoveryFile{Version: 1, Sessions: s.recovery})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.recoveryPath), ".agent-resume-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), s.recoveryPath); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(s.recoveryPath)); err == nil {
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}

func (s *Server) forgetRecovery(sid string) error {
	if _, ok := s.recovery[sid]; !ok {
		return nil
	}
	delete(s.recovery, sid)
	return s.saveRecovery()
}

func (s *Server) recordAgent(ss *session, event *AgentEvent) error {
	if event == nil {
		return nil
	}
	h := agents.NormalizeHook(event.Agent, event.Input)
	ss.mu.Lock()
	accepted := ss.agent.lastAt.Equal(event.At) && ss.agent.state.ID == event.Agent && ss.agent.state.SessionID == h.SessionID
	ss.mu.Unlock()
	if !accepted {
		return nil
	}
	if h.Event == "SessionEnd" {
		return s.forgetRecovery(ss.id)
	}
	if event.Agent != "codex" && event.Agent != "claude" {
		return s.forgetRecovery(ss.id)
	}
	if ss.p == nil || ss.p.cmd.Process == nil {
		return nil
	}
	fg := ss.p.foreground()
	if fg <= 0 {
		fg = ss.p.cmd.Process.Pid
	}
	p := inspect(fg)
	options, supported := resumeOptions(event.Agent, p.args)
	if !supported {
		return s.forgetRecovery(ss.id)
	}
	r := agentResume{Agent: event.Agent, SessionID: h.SessionID, Dir: h.CWD, Options: options, Env: resumeConfigEnv(event.Agent, h.ConfigDir, ss.p.cmd.Env), seen: time.Now()}
	if r.Dir == "" {
		r.Dir = p.dir
	}
	if a, ok := agents.Detect(p.programName(), p.args); ok && a.ID == event.Agent {
		r.pid = fg
	}
	if previous, ok := s.recovery[ss.id]; ok && previous.Agent == r.Agent && previous.SessionID == r.SessionID {
		// Metadata events may arrive while a tool owns the foreground group.
		if r.pid == 0 {
			r.pid, r.Options = previous.pid, previous.Options
			if h.ConfigDir == nil {
				r.Env = previous.Env
			}
		}
		if r.Dir == "" {
			r.Dir = previous.Dir
		}
	}
	if s.recovery == nil {
		s.recovery = map[string]agentResume{}
	}
	previous := s.recovery[ss.id]
	s.recovery[ss.id] = r
	if previous.Agent == r.Agent && previous.SessionID == r.SessionID && previous.Dir == r.Dir && previous.Error == "" && slices.Equal(previous.Options, r.Options) && slices.Equal(previous.Env, r.Env) {
		return nil
	}
	return s.saveRecovery()
}

func (s *Server) restoreAgent(req Request) (RestoreResult, error) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.ending {
		return RestoreResult{}, errors.New("session server is ending")
	}
	if req.SID == "" {
		return RestoreResult{}, errors.New("missing pane session ID")
	}
	ss, _ := s.session(req.SID)
	if ss != nil {
		info := ss.info()
		if !info.Exited {
			return RestoreResult{Session: &info}, nil
		}
		s.updateRecoveryExit(ss)
	}
	r, ok := s.recovery[req.SID]
	if !ok {
		return RestoreResult{}, nil
	}
	result := RestoreResult{Agent: r.Agent}
	if r.Error != "" && !req.Retry {
		result.Error = r.Error
		if ss != nil {
			info := ss.info()
			result.Session = &info
		} // replay failed CLI output
		return result, nil
	}
	cmd, err := r.command()
	if err == nil {
		ss, err = newSessionForServer(req.SID, CreateOptions{Command: cmd, Dir: r.Dir, Env: r.Env, Cols: req.Cols, Rows: req.Rows, resumed: true}, s.agentToken)
	}
	if err != nil {
		r.Error = err.Error()
		s.recovery[req.SID] = r
		if saveErr := s.saveRecovery(); saveErr != nil {
			return result, saveErr
		}
		result.Error = r.Error
		return result, nil
	}
	r.Error, r.pid, r.seen = "", ss.p.cmd.Process.Pid, time.Now()
	ss.mu.Lock()
	ss.agent.state = AgentState{ID: r.Agent, SessionID: r.SessionID, State: agents.Ready, Updated: time.Now()}
	ss.mu.Unlock()
	s.recovery[req.SID] = r
	s.mu.Lock()
	if s.sessions[req.SID] == nil {
		s.order = append(s.order, req.SID)
	}
	s.sessions[req.SID] = ss
	s.mu.Unlock()
	if err := s.saveRecovery(); err != nil {
		log.Printf("rex: save Agent recovery: %v", err)
	}
	info := ss.info()
	result.Session = &info
	return result, nil
}

func (s *Server) updateRecoveryExit(ss *session) {
	r, ok := s.recovery[ss.id]
	if !ok {
		return
	}
	ss.mu.Lock()
	exited, code, resumed := ss.exited, ss.code, ss.resumed
	ss.mu.Unlock()
	if !exited {
		return
	}
	if resumed && code != 0 {
		message := fmt.Sprintf("%s 恢复进程已退出（%d）", r.Agent, code)
		if r.Error == message {
			return
		}
		r.Error, r.pid = message, 0
		s.recovery[ss.id] = r
	} else {
		delete(s.recovery, ss.id)
	}
	if err := s.saveRecovery(); err != nil {
		log.Printf("rex: save Agent exit: %v", err)
	}
}

func (s *Server) watchRecovery() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.checkRecovery()
	}
}

func (s *Server) checkRecovery() {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	for sid, r := range s.recovery {
		ss, _ := s.session(sid)
		if ss == nil {
			continue
		} // previous boot; await a GUI restore request
		ss.mu.Lock()
		exited := ss.exited
		ss.mu.Unlock()
		if exited {
			s.updateRecoveryExit(ss)
			continue
		}
		if r.pid == 0 {
			fg := ss.p.foreground()
			p := inspect(fg)
			if a, ok := agents.Detect(p.programName(), p.args); ok && a.ID == r.Agent {
				r.pid = fg
				s.recovery[sid] = r
				continue
			}
			if time.Since(r.seen) < 5*time.Second {
				continue
			}
		} else {
			p := inspect(r.pid)
			if a, ok := agents.Detect(p.programName(), p.args); ok && a.ID == r.Agent {
				continue
			}
			if time.Since(r.seen) < 5*time.Second {
				continue
			}
		}
		// An Agent killed from outside cannot emit SessionEnd. A stopped
		// or backgrounded Agent remains recorded while its process lives.
		if err := s.forgetRecovery(sid); err != nil {
			log.Printf("rex: forget Agent recovery: %v", err)
		}
	}
}
