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

func TestAgentStateLifecycleAndConcurrentQuestions(t *testing.T) {
	var tracker agentTracker
	clock := time.Now()
	apply := func(session, event, tool, id, notification string) {
		clock = clock.Add(time.Millisecond)
		tracker.apply(AgentEvent{Agent: "claude", At: clock, Input: agents.HookInput{SessionID: session, Event: event, Tool: tool, ToolID: id, Notification: notification}})
	}
	assert := func(state string, revision uint64) {
		t.Helper()
		if tracker.state.State != state || tracker.state.WaitRevision != revision {
			t.Fatalf("state %+v; expected %s/%d", tracker.state, state, revision)
		}
	}
	apply("first", "SessionStart", "", "", "")
	assert(agents.Ready, 0)
	apply("first", "UserPromptSubmit", "", "", "")
	assert(agents.Running, 0)
	apply("first", "PreToolUse", "AskUserQuestion", "q1", "")
	assert(agents.Waiting, 1)
	apply("first", "PreToolUse", "AskUserQuestion", "q2", "")
	apply("first", "PostToolUse", "Bash", "b1", "")
	assert(agents.Waiting, 1)
	apply("first", "PostToolUse", "AskUserQuestion", "q1", "")
	assert(agents.Waiting, 1)
	apply("first", "PostToolUse", "AskUserQuestion", "q2", "")
	assert(agents.Running, 1)
	apply("first", "PermissionRequest", "Bash", "", "")
	assert(agents.Waiting, 2)
	apply("first", "Notification", "", "", "permission_prompt")
	assert(agents.Waiting, 2)
	apply("first", "PostToolUse", "Bash", "b2", "")
	assert(agents.Running, 2)
	apply("first", "Stop", "", "", "")
	assert(agents.Completed, 2)
	apply("first", "Notification", "", "", "idle_prompt")
	assert(agents.Completed, 2)
	apply("second", "SessionStart", "", "", "")
	assert(agents.Ready, 2)
	apply("second", "UserPromptSubmit", "", "", "")
	apply("first", "SessionEnd", "", "", "")
	assert(agents.Running, 2)
	apply("first", "Stop", "", "", "")
	assert(agents.Running, 2)
	tracker.apply(AgentEvent{Agent: "claude", At: clock.Add(-time.Hour), Input: agents.HookInput{SessionID: "second", Event: "Stop"}})
	assert(agents.Running, 2)
	apply("second", "PreToolUse", "AskUserQuestion", "q3", "")
	assert(agents.Waiting, 3)
	apply("second", "SessionEnd", "", "", "")
	assert("", 3)
	apply("second", "Stop", "", "", "")
	assert("", 3)
	apply("first", "SessionStart", "", "", "")
	assert(agents.Ready, 3)
	apply("first", "PreToolUse", "AskUserQuestion", "resumed-question", "")
	assert(agents.Waiting, 4)
	apply("second", "Stop", "", "", "")
	assert(agents.Waiting, 4)
}

func TestAgentIgnoresPreviousCodexTurn(t *testing.T) {
	var tracker agentTracker
	now := time.Now()
	for i, input := range []agents.HookInput{
		{SessionID: "thread", Event: "UserPromptSubmit", TurnID: "old"},
		{SessionID: "thread", Event: "UserPromptSubmit", TurnID: "new"},
		{SessionID: "thread", Event: "Stop", TurnID: "old"},
	} {
		tracker.apply(AgentEvent{Agent: "codex", Input: input, At: now.Add(time.Duration(i) * time.Millisecond)})
	}
	if tracker.state.State != agents.Running || tracker.turnID != "new" {
		t.Fatal("late Stop completed the wrong turn")
	}
}

func TestAgentCompletionRevisionCountsTurnsNotDuplicateStop(t *testing.T) {
	var tracker agentTracker
	clock := time.Now()
	apply := func(agent, session, event, turn string) {
		clock = clock.Add(time.Millisecond)
		tracker.apply(AgentEvent{Agent: agent, At: clock, Input: agents.HookInput{SessionID: session, Event: event, TurnID: turn}})
	}
	apply("codex", "thread", "UserPromptSubmit", "first")
	apply("codex", "thread", "Stop", "first")
	apply("codex", "thread", "Stop", "first")
	if tracker.state.CompletionRevision != 1 {
		t.Fatal("duplicate Stop counted as another completion")
	}
	apply("codex", "thread", "UserPromptSubmit", "second")
	apply("codex", "thread", "Stop", "first")
	if tracker.state.CompletionRevision != 1 || tracker.state.State != agents.Running {
		t.Fatal("late Stop completed current turn")
	}
	apply("codex", "thread", "Stop", "second")
	if tracker.state.CompletionRevision != 2 {
		t.Fatal("next turn not counted")
	}
	apply("claude", "other", "SessionStart", "")
	apply("claude", "other", "UserPromptSubmit", "")
	apply("claude", "other", "StopFailure", "")
	apply("claude", "other", "StopFailure", "")
	if tracker.state.CompletionRevision != 3 || tracker.state.State != agents.Failed {
		t.Fatal("failure not counted once")
	}
	apply("claude", "other", "SessionEnd", "")
	if tracker.state.CompletionRevision != 3 {
		t.Fatal("ending session lost completion revision")
	}
}

func TestSessionInjectsDistinctAgentCredentials(t *testing.T) {
	t.Setenv("GOREX_DIR", t.TempDir())
	t.Setenv("GOREX_AGENT_TOKEN", "parent-token")
	dir := t.TempDir()
	var tokens []string
	for _, id := range []string{"one", "two"} {
		capture := filepath.Join(dir, id)
		ss, err := newSession(id, CreateOptions{Dir: dir, Env: []string{"GOREX_AGENT_TOKEN=extra-token"},
			Command: []string{"/bin/sh", "-c", `umask 077; printf '%s\n' "$GOREX_SESSION" "$GOREX_AGENT_TOKEN" "$GOREX_AGENT_SOCKET" "$GOREX_HOOK" > "$1"; sleep 10`, "hook-test", capture}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(ss.kill)
		deadline := time.Now().Add(3 * time.Second)
		var data []byte
		for time.Now().Before(deadline) {
			data, _ = os.ReadFile(capture)
			if len(strings.Split(strings.TrimSpace(string(data)), "\n")) == 4 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		fields := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(fields) != 4 || fields[0] != id || fields[1] != ss.agentToken || fields[2] != SocketPath() || fields[3] == "" {
			t.Fatal("session hook environment not injected correctly")
		}
		tokens = append(tokens, fields[1])
	}
	if tokens[0] == tokens[1] || tokens[0] == "parent-token" || tokens[0] == "extra-token" {
		t.Fatal("agent credentials reused or overwritten")
	}
}

func TestAgentHookSocketAuthenticationAndRouting(t *testing.T) {
	t.Setenv("GOREX_AGENT_SERVER_TOKEN", "")
	dir, err := os.MkdirTemp("/tmp", "gorex-agent-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "events.sock")
	if len(path) >= 100 {
		t.Skip("test socket path too long")
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ss := &session{id: "correct-pane", agentToken: "secret"}
	server := &Server{sessions: map[string]*session{ss.id: ss}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				var request Request
				json.NewDecoder(conn).Decode(&request)
				json.NewEncoder(conn).Encode(server.request(request))
			}()
		}
	}()
	e := AgentEvent{Agent: "codex", At: time.Now(), Input: agents.HookInput{SessionID: "cli-thread", Event: "PermissionRequest", Tool: "Bash"}}
	for _, tc := range []struct{ sid, token string }{{ss.id, ""}, {ss.id, "wrong"}, {"other-pane", "secret"}} {
		if err := ReportAgent(path, tc.sid, tc.token, e); err == nil {
			t.Fatalf("unauthorized report accepted: %+v", tc)
		}
	}
	t.Setenv("GOREX_SESSION", ss.id)
	t.Setenv("GOREX_AGENT_TOKEN", ss.agentToken)
	t.Setenv("GOREX_AGENT_SOCKET", path)
	RunAgentHook("codex", strings.NewReader(`{"session_id":"cli-thread","hook_event_name":"PermissionRequest","tool_name":"Bash"}`))
	ss.mu.Lock()
	got := ss.agent.state
	ss.mu.Unlock()
	if got.State != agents.Waiting || got.Reason != "permission" || got.WaitRevision != 1 {
		t.Fatalf("hook not routed: %+v", got)
	}
	RunAgentHook("codex", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	RunAgentHook("codex", strings.NewReader("bad json"))
	t.Setenv("GOREX_AGENT_TOKEN", "")
	RunAgentHook("codex", strings.NewReader(`{"session_id":"cli-thread","hook_event_name":"Stop"}`))
	ss.mu.Lock()
	after := ss.agent.state
	ss.mu.Unlock()
	if got != after {
		t.Fatal("invalid hook changed state")
	}
}

func TestGeminiNormalizedLifecycleClearsPendingPermission(t *testing.T) {
	var tracker agentTracker
	now := time.Now()
	for i, input := range []agents.HookInput{
		{SessionID: "gemini", Event: "BeforeAgent"},
		{SessionID: "gemini", Event: "Notification", Notification: "ToolPermission"},
		{SessionID: "gemini", Event: "AfterTool", Tool: "run_shell_command"},
		{SessionID: "gemini", Event: "AfterAgent"},
	} {
		tracker.apply(AgentEvent{Agent: "gemini", Input: input, At: now.Add(time.Duration(i) * time.Millisecond)})
		if i == 1 && tracker.state.State != agents.Waiting {
			t.Fatal(tracker.state)
		}
		if i == 2 && tracker.state.State != agents.Running {
			t.Fatal("permission not cleared", tracker.state)
		}
	}
	if tracker.state.State != agents.Completed || tracker.state.WaitRevision != 1 || tracker.state.CompletionRevision != 1 {
		t.Fatal(tracker.state)
	}
}
