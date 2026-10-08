//go:build darwin || linux

package rex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"gorex/internal/agents"
)

func codexSocket() (string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(home, ".codex")
	}
	return filepath.Join(home, "app-server-control", "app-server-control.sock"), nil
}

func dialCodex(ctx context.Context, socket string) (*websocket.Conn, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	c, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if c != nil {
		c.SetReadLimit(-1)
	}
	return c, err
}

// StartCodexBridge never restarts a daemon. The private endpoint is consumed
// by the launcher, while pane capabilities remain only in process memory.
func StartCodexBridge(launcher string, parent int, exe string) error {
	if parent <= 0 || osPaneContextMissing() {
		return errors.New("missing pane context")
	}
	socket, err := codexSocket()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	c, err := dialCodex(ctx, socket)
	cancel()
	if err != nil {
		// A normal CLI also starts the daemon when one does not exist.
		ctx, cancel = context.WithTimeout(context.Background(), 8*time.Second)
		start := exec.CommandContext(ctx, exe, "app-server", "daemon", "start")
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GOREX_") {
				start.Env = append(start.Env, kv)
			}
		}
		err = start.Run()
		cancel()
		if err != nil {
			return err
		}
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		c, err = dialCodex(ctx, socket)
		cancel()
		if err != nil {
			return err
		}
	}
	c.CloseNow()
	dir, err := os.MkdirTemp("", "gorex-codex-")
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	cmd := exec.Command(self, "-codex-proxy", dir, strconv.Itoa(parent), socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return err
	}
	go cmd.Wait()
	ready := filepath.Join(dir, "ready")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return os.WriteFile(filepath.Join(launcher, "endpoint"), []byte("unix://"+filepath.Join(dir, "bridge.sock")), 0o600)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cmd.Process.Kill()
	os.RemoveAll(dir)
	return errors.New("Codex notification relay did not start")
}

func osPaneContextMissing() bool {
	return os.Getenv("GOREX_SESSION") == "" || os.Getenv("GOREX_AGENT_TOKEN") == "" || os.Getenv("GOREX_AGENT_SOCKET") == ""
}

// RunCodexProxy relays frames unchanged. It observes only lifecycle metadata
// for threads opened by this CLI, never approvals, prompts or model output.
func RunCodexProxy(dir string, parent int, socket string) error {
	defer os.RemoveAll(dir)
	ln, err := net.Listen("unix", filepath.Join(dir, "bridge.sock"))
	if err != nil {
		return err
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !processAlive(parent) {
					cancel()
					ln.Close()
					return
				}
			}
		}
	}()
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		back, err := dialCodex(ctx, socket)
		if err != nil {
			http.Error(w, "Codex daemon unavailable", http.StatusServiceUnavailable)
			return
		}
		defer back.CloseNow()
		front, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer front.CloseNow()
		front.SetReadLimit(-1)
		connection, end := context.WithCancel(ctx)
		defer end()
		observer := newCodexObserver(connection)
		copy := func(from, to *websocket.Conn, client bool) {
			defer end()
			for {
				kind, raw, err := from.Read(connection)
				if err != nil {
					return
				}
				// Register requests before the daemon can respond; observe responses
				// before forwarding so subsequent CLI turns have an established owner.
				if kind == websocket.MessageText {
					observer.message(raw, client)
				}
				if to.Write(connection, kind, raw) != nil {
					return
				}
			}
		}
		go copy(front, back, true)
		copy(back, front, false)
	})}
	if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0o600); err != nil {
		return err
	}
	err = server.Serve(ln)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

type codexEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

type codexThread struct {
	ID     string      `json:"id"`
	CWD    string      `json:"cwd"`
	Parent string      `json:"parentThreadId"`
	Status codexStatus `json:"status"`
}

type codexStatus struct {
	Type  string   `json:"type"`
	Flags []string `json:"activeFlags"`
}

type codexTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type codexObserver struct {
	mu      sync.Mutex
	pending map[string]string
	thread  codexThread
	turn    string
	active  bool
	wait    string
	emit    func(agents.HookInput)
}

func newCodexObserver(ctx context.Context) *codexObserver {
	events := make(chan AgentEvent, 256)
	sid, token, socket := os.Getenv("GOREX_SESSION"), os.Getenv("GOREX_AGENT_TOKEN"), os.Getenv("GOREX_AGENT_SOCKET")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				_ = ReportAgent(socket, sid, token, event)
			}
		}
	}()
	return &codexObserver{pending: map[string]string{}, emit: func(h agents.HookInput) {
		select {
		case events <- AgentEvent{Agent: "codex", Input: h, At: time.Now()}:
		default: // Notification reporting cannot hold up terminal operations.
		}
	}}
}

func (o *codexObserver) event(name, tool string) {
	if o.thread.ID == "" {
		return
	}
	o.emit(agents.HookInput{Event: name, SessionID: o.thread.ID, CWD: o.thread.CWD, TurnID: o.turn, Tool: tool})
}

func (o *codexObserver) message(raw []byte, client bool) {
	var message codexEnvelope
	if json.Unmarshal(raw, &message) != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if client {
		switch message.Method {
		case "thread/start", "thread/resume", "thread/fork":
			if len(message.ID) > 0 && len(o.pending) < 64 {
				o.pending[string(message.ID)] = message.Method
			}
		}
		return
	}
	if method, ok := o.pending[string(message.ID)]; message.Method == "" && len(message.ID) > 0 && ok {
		delete(o.pending, string(message.ID))
		var result struct {
			Thread codexThread `json:"thread"`
		}
		if json.Unmarshal(message.Result, &result) == nil && result.Thread.ID != "" && result.Thread.Parent == "" {
			o.thread, o.turn, o.active, o.wait = result.Thread, "", false, ""
			h := agents.HookInput{Event: "SessionStart", SessionID: o.thread.ID, CWD: o.thread.CWD, Source: "startup"}
			if method == "thread/resume" {
				h.Source = "resume"
			}
			o.emit(h)
			if result.Thread.Status.Type == "active" {
				o.active = true
				o.event("UserPromptSubmit", "")
				o.waiting(result.Thread.Status)
			}
		}
		return
	}
	var params struct {
		ThreadID string      `json:"threadId"`
		Turn     codexTurn   `json:"turn"`
		Status   codexStatus `json:"status"`
	}
	if json.Unmarshal(message.Params, &params) != nil || params.ThreadID == "" || params.ThreadID != o.thread.ID {
		return
	}
	switch message.Method {
	case "turn/started":
		if params.Turn.ID == "" || (o.active && o.turn == params.Turn.ID) {
			return
		}
		o.turn, o.active, o.wait = params.Turn.ID, true, ""
		o.event("UserPromptSubmit", "")
	case "turn/completed":
		if params.Turn.ID == "" || (o.turn != "" && params.Turn.ID != o.turn) || !o.active {
			return
		}
		o.turn = params.Turn.ID
		o.active, o.wait = false, ""
		switch params.Turn.Status {
		case "completed":
			o.event("Stop", "")
		case "interrupted":
			o.event("Interrupt", "")
		case "failed":
			o.event("StopFailure", "")
		}
	case "thread/status/changed":
		o.waiting(params.Status)
	}
}

func (o *codexObserver) waiting(status codexStatus) {
	if !o.active || status.Type != "active" {
		return
	}
	wait := ""
	for _, flag := range status.Flags {
		if flag == "waitingOnApproval" {
			wait = "permission"
			break
		}
		if flag == "waitingOnUserInput" {
			wait = "question"
		}
	}
	if wait == o.wait {
		return
	}
	// Clear the bridge's previous waiting key before changing its reason.
	if o.wait != "" {
		o.event("PostToolUse", "codex_bridge")
		o.event("PostToolUse", "request_user_input")
	}
	o.wait = wait
	if wait == "permission" {
		o.event("PermissionRequest", "codex_bridge")
	}
	if wait == "question" {
		o.event("PreToolUse", "request_user_input")
	}
}

// ParseBridgePID is used only by the early CLI entry point.
func ParseBridgePID(value string) (int, error) {
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid Codex launcher PID")
	}
	return pid, nil
}
