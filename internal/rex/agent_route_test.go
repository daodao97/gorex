package rex

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorex/internal/agents"
)

func TestCodexConversationRouting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		panes []codexCandidate
		input agents.HookInput
		want  string
	}{
		{"known owner overrides cwd", []codexCandidate{{"one", "/a", "old", false}, {"two", "/b", "new", false}}, agents.HookInput{SessionID: "new", CWD: "/a", Event: "PermissionRequest"}, "two"},
		{"new conversation by cwd", []codexCandidate{{"one", "/a", "", false}, {"two", "/b", "", false}}, agents.HookInput{SessionID: "new", CWD: "/b", Event: "SessionStart"}, "two"},
		{"same cwd fresh pane", []codexCandidate{{"one", "/a", "old", false}, {"two", "/a", "", false}}, agents.HookInput{SessionID: "new", CWD: "/a", Event: "SessionStart"}, "two"},
		{"clear initialized pane", []codexCandidate{{"one", "/a", "old", false}, {"two", "/a", "", false}}, agents.HookInput{SessionID: "new", CWD: "/a", Event: "SessionStart", Source: "clear"}, "one"},
		{"codex C fallback", []codexCandidate{{"one", "/a", "old", false}, {"two", "/b", "", false}}, agents.HookInput{SessionID: "new", CWD: "/elsewhere", Event: "SessionStart"}, "two"},
		{"ambiguous starts", []codexCandidate{{"one", "/a", "", false}, {"two", "/a", "", false}}, agents.HookInput{SessionID: "new", CWD: "/a", Event: "SessionStart"}, ""},
		{"late stop cannot claim fresh pane", []codexCandidate{{"two", "/a", "", false}}, agents.HookInput{SessionID: "closed", CWD: "/a", Event: "Stop"}, ""},
		{"retired user turn cannot claim fresh pane", []codexCandidate{{"one", "/a", "new", true}, {"two", "/a", "", false}}, agents.HookInput{SessionID: "old", CWD: "/a", Event: "UserPromptSubmit"}, ""},
		{"duplicate owners ambiguous", []codexCandidate{{"one", "/a", "same", false}, {"two", "/b", "same", false}}, agents.HookInput{SessionID: "same", Event: "Stop"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexTarget(tc.panes, tc.input); got != tc.want {
				t.Fatalf("target %q, want %q", got, tc.want)
			}
		})
	}
	dir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if !sameAgentDir(dir, alias) || sameAgentDir("", "") {
		t.Fatal("cwd canonicalization failed")
	}
}

func TestSharedAgentCapabilitySurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := loadAgentToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAgentToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "agent-token"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 || info.Mode().Perm() != 0o600 {
		t.Fatal("shared daemon capability changed or was not private")
	}
}

// Real foreground PTYs plus the hook socket reproduce a shared app-server:
// every report carries the first pane's inherited environment, even after
// that pane has been closed. No model call or global configuration is used.
func TestSharedCodexDaemonRoutesToCorrectLivePane(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gorex-shared-hook-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("GOREX_DIR", dir)
	token, err := loadAgentToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	server := &Server{sessions: map[string]*session{}, agentToken: token}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(bin, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile IFS= read -r line; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		cwd := filepath.Join(dir, id)
		if err := os.Mkdir(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		ss, err := newSessionForServer(id, CreateOptions{Dir: cwd, Command: []string{"/bin/sh"}}, token)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(ss.kill)
		server.sessions[id] = ss
		ss.input([]byte("'" + script + "'\r"))
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			info := ss.info()
			if info.Program == "codex" && !info.Idle {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if info := ss.info(); info.Program != "codex" || info.Idle {
			t.Fatal("Codex PTY fixture did not enter foreground")
		}
	}
	// An ordinary shell in the same cwd cannot steal Codex hook events.
	shell, err := newSessionForServer("shell", CreateOptions{Dir: filepath.Join(dir, "second"), Command: []string{"/bin/sh"}}, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.kill)
	server.sessions[shell.id] = shell
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				var req Request
				json.NewDecoder(conn).Decode(&req)
				json.NewEncoder(conn).Encode(server.request(req))
			}()
		}
	}()
	first, second := server.sessions["first"], server.sessions["second"]
	t.Setenv("GOREX_SESSION", first.id)
	t.Setenv("GOREX_AGENT_TOKEN", first.agentToken)
	t.Setenv("GOREX_AGENT_SERVER_TOKEN", token)
	t.Setenv("GOREX_AGENT_SOCKET", path)
	send := func(input agents.HookInput) {
		t.Helper()
		data, _ := json.Marshal(input)
		RunAgentHook("codex", strings.NewReader(string(data)))
	}
	send(agents.HookInput{SessionID: "conversation-one", Event: "SessionStart", CWD: filepath.Join(dir, "first")})
	send(agents.HookInput{SessionID: "conversation-two", Event: "SessionStart", CWD: filepath.Join(dir, "second")})
	first.mu.Lock()
	gotFirst := first.agent.state
	first.mu.Unlock()
	second.mu.Lock()
	gotSecond := second.agent.state
	second.mu.Unlock()
	if gotFirst.SessionID != "conversation-one" || gotSecond.SessionID != "conversation-two" {
		t.Fatal("shared daemon used inherited pane instead of conversation/cwd")
	}
	first.kill()
	server.mu.Lock()
	delete(server.sessions, first.id)
	server.mu.Unlock()
	// Deliberately use the wrong cwd: established conversation ownership wins.
	send(agents.HookInput{SessionID: "conversation-two", Event: "PermissionRequest", CWD: filepath.Join(dir, "first"), Tool: "Bash"})
	second.mu.Lock()
	waiting := second.agent.state
	second.mu.Unlock()
	if waiting.State != agents.Waiting || waiting.WaitRevision != 1 {
		t.Fatal("closing original daemon pane broke routing")
	}
	e := AgentEvent{Agent: "codex", At: time.Now(), Input: agents.HookInput{SessionID: "conversation-two", Event: "Stop"}}
	if err := ReportAgent(path, first.id, "invalid-token", e); err == nil {
		t.Fatal("invalid shared token accepted")
	}
	send(e.Input)
	second.mu.Lock()
	completed := second.agent.state
	second.mu.Unlock()
	if completed.State != agents.Completed {
		t.Fatal("known conversation not completed")
	}
	shell.mu.Lock()
	shellState := shell.agent.state
	shell.mu.Unlock()
	if shellState.State != "" {
		t.Fatal("ordinary shell received agent state")
	}
}
