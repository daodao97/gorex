package rex

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"gorex/internal/agents"
)

// AgentState is owned by the session server, so reconnecting a window does
// not lose a pending question. WaitRevision counts distinct waiting periods.
type AgentState struct {
	ID                 string    `json:"id,omitempty"`
	SessionID          string    `json:"sessionID,omitempty"`
	State              string    `json:"state,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	Updated            time.Time `json:"updated,omitzero"`
	WaitRevision       uint64    `json:"waitRevision,omitempty"`
	CompletionRevision uint64    `json:"completionRevision,omitempty"`
}

type AgentEvent struct {
	Agent string           `json:"agent"`
	Input agents.HookInput `json:"input"`
	At    time.Time        `json:"at"`
}

type agentTracker struct {
	state   AgentState
	lastAt  time.Time
	retired map[string]bool
	waiting map[string]string // tool -> reason; parallel tools must not clear another question
	turnID  string
}

func (s *session) reportAgent(token string, event *AgentEvent) error {
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.agentToken)) != 1 {
		return errors.New("invalid agent token")
	}
	if err := validateAgentEvent(event); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return errors.New("session exited")
	}
	s.agent.apply(*event)
	return nil
}

func validateAgentEvent(event *AgentEvent) error {
	if event == nil {
		return errors.New("missing agent event")
	}
	b, _ := json.Marshal(event.Input)
	if _, valid := agents.ParseHook(event.Agent, b); !valid || event.At.IsZero() || event.At.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid agent event")
	}
	return nil
}

func (t *agentTracker) apply(e AgentEvent) {
	h := agents.NormalizeHook(e.Agent, e.Input)
	previousTurn := t.turnID
	state, reason := agents.HookStatus(e.Agent, h)
	if state == "" || e.At.Before(t.lastAt) {
		return
	}
	if t.retired[h.SessionID] {
		// Resuming an older conversation is a legitimate new start. Late
		// tool/Stop/SessionEnd events still cannot take the pane back.
		if h.Event != "SessionStart" {
			return
		}
		delete(t.retired, h.SessionID)
	}
	if t.state.SessionID != "" && t.state.SessionID != h.SessionID {
		// Only a session start or a new user turn can claim this pane.
		// Late SessionEnd/Stop events from an old conversation are ignored.
		if h.Event != "SessionStart" && h.Event != "UserPromptSubmit" {
			return
		}
		if t.retired == nil {
			t.retired = map[string]bool{}
		}
		if len(t.retired) >= 64 {
			clear(t.retired)
		}
		t.retired[t.state.SessionID] = true
		t.waiting = nil
		t.turnID = ""
	}
	if h.Event == "SessionStart" {
		t.turnID = ""
	}
	if h.TurnID != "" {
		if h.Event != "UserPromptSubmit" && t.turnID != "" && h.TurnID != t.turnID {
			return
		}
		t.turnID = h.TurnID
	}
	// An idle reminder after a completed response is not an unanswered
	// question. Keep the completed/ready state instead of notifying again.
	if h.Event == "Notification" && h.Notification == "idle_prompt" && (t.state.State == agents.Completed || t.state.State == agents.Ready) {
		return
	}
	if t.state.State == "" && t.state.SessionID == h.SessionID && h.Event != "SessionStart" && h.Event != "UserPromptSubmit" {
		return
	}
	t.lastAt = e.At
	key := h.Tool
	if h.ToolID != "" {
		key += ":" + h.ToolID
	}
	if key == "" {
		key = reason
	}
	switch {
	case h.Event == "SessionStart" || h.Event == "UserPromptSubmit" || state == agents.Completed || state == agents.Ready || state == agents.Failed || state == agents.Ended:
		t.waiting = nil
	case state == agents.Waiting:
		if t.waiting == nil {
			t.waiting = map[string]string{}
		}
		// Notification repeats a permission already reported by its tool.
		duplicate := false
		if h.Event == "Notification" {
			for _, r := range t.waiting {
				if r == reason {
					duplicate = true
				}
			}
		}
		if !duplicate {
			t.waiting[key] = reason
		}
	case h.Event == "PostToolUse" || h.Event == "PostToolUseFailure" || h.Event == "ElicitationResult":
		delete(t.waiting, key)
		// Codex PermissionRequest does not include tool_use_id.
		delete(t.waiting, h.Tool)
		// Legacy notifications carry no tool ID; any subsequent completed
		// tool or new user turn resolves those notifications.
		delete(t.waiting, "permission")
		delete(t.waiting, "input")
		if h.Event == "ElicitationResult" {
			delete(t.waiting, "question")
		}
	}
	if len(t.waiting) > 0 && state != agents.Ended {
		state, reason = agents.Waiting, "input"
		for _, r := range t.waiting {
			if r == "permission" {
				reason = r
				break
			}
			if r == "question" {
				reason = r
			}
		}
	}
	revision := t.state.WaitRevision
	if state == agents.Waiting && (t.state.State != agents.Waiting || t.state.SessionID != h.SessionID) {
		revision++
	}
	completion := t.state.CompletionRevision
	finished := state == agents.Completed || state == agents.Failed
	wasFinished := t.state.State == agents.Completed || t.state.State == agents.Failed
	if finished && (!wasFinished || t.state.SessionID != h.SessionID || (h.TurnID != "" && h.TurnID != previousTurn)) {
		completion++
	}
	t.state = AgentState{ID: e.Agent, SessionID: h.SessionID, State: state, Reason: reason, Updated: e.At, WaitRevision: revision, CompletionRevision: completion}
	if state == agents.Ended {
		t.state.State = ""
	}
}

// RunAgentHook never writes model-visible output, starts a server or changes
// an approval decision. A failed report cannot hold up the agent.
func RunAgentHook(agent string, stdin io.Reader) {
	sid, token, socket := os.Getenv("GOREX_SESSION"), os.Getenv("GOREX_AGENT_TOKEN"), os.Getenv("GOREX_AGENT_SOCKET")
	if agent == "codex" && os.Getenv("GOREX_AGENT_SERVER_TOKEN") != "" {
		token = os.Getenv("GOREX_AGENT_SERVER_TOKEN")
	}
	if sid == "" || token == "" || socket == "" {
		return
	}
	type inputResult struct {
		data []byte
		err  error
	}
	input := make(chan inputResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(stdin, (1<<20)+1))
		input <- inputResult{data, err}
	}()
	var data []byte
	var err error
	select {
	case result := <-input:
		data, err = result.data, result.err
	case <-time.After(500 * time.Millisecond):
		return
	}
	if err != nil || len(data) > 1<<20 {
		return
	}
	h, valid := agents.ParseHook(agent, data)
	if !valid {
		return
	}
	event := AgentEvent{Agent: agent, Input: h, At: time.Now()}
	if ReportAgent(socket, sid, token, event) == nil {
		_ = agents.SaveHookTask(Dir(), agent, h, data, event.At)
	}
}

func ReportAgent(socket, sid, token string, event AgentEvent) error {
	conn, err := net.DialTimeout("unix", socket, 300*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(750 * time.Millisecond))
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Op: "agentEvent", SID: sid, Token: token, AgentEvent: &event}); err != nil {
		return err
	}
	var res Response
	if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&res); err != nil {
		return err
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	return nil
}
