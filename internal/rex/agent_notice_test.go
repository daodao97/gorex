package rex

import (
	"testing"
	"time"
)

func TestAgentNoticeIdentityIsStableAndSeparatesTasksAndDevices(t *testing.T) {
	now := time.Now()
	s := SessionInfo{ID: "pane", LastInput: now, Agent: AgentState{ID: "codex", SessionID: "thread", State: "completed", CompletionRevision: 2, Updated: now}}
	id := AgentNoticeID("desktop", s)
	s.Agent.Updated = now.Add(time.Second)
	if AgentNoticeID("desktop", s) != id {
		t.Fatal("duplicate stop changed notice identity")
	}
	if AgentNoticeID("other-desktop", s) == id {
		t.Fatal("desktop collision")
	}
	s.Agent.CompletionRevision++
	if AgentNoticeID("desktop", s) == id {
		t.Fatal("distinct task reused identity")
	}
	s.Agent.CompletionRevision = 0
	id = AgentNoticeID("desktop", s)
	s.LastInput = now.Add(time.Second)
	if AgentNoticeID("desktop", s) == id {
		t.Fatal("legacy new input reused identity")
	}
}
