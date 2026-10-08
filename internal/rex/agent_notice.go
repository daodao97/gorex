package rex

import (
	"crypto/sha256"
	"fmt"
	"gorex/internal/agents"
	"strconv"
)

// AgentNoticeID is shared by local reminders, APNs and foreground receipts.
// Older servers lack completion revisions, so the completion event timestamp
// identifies their turns. Terminal input after completion is never a new task.
func AgentNoticeID(desktop string, s SessionInfo) string {
	a := s.Agent
	revision := a.WaitRevision
	if a.State == agents.Completed || a.State == agents.Failed {
		revision = a.CompletionRevision
	}
	key := desktop + "\x00" + s.ID + "\x00" + a.ID + "\x00" + a.SessionID + "\x00" + a.State + "\x00" + strconv.FormatUint(revision, 10)
	if a.State != agents.Waiting && revision == 0 {
		key += "\x00" + strconv.FormatInt(a.Updated.UnixNano(), 10)
	}
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("gorex-agent-%x", sum[:16])
}

// AgentNoticeTransition recognizes actual lifecycle changes. It does not infer
// completion from silence, title text or terminal output, or report old results
// when a viewer first subscribes.
func AgentNoticeTransition(previous, current SessionInfo) bool {
	p, a := previous.Agent, current.Agent
	same := p.ID == a.ID && p.SessionID == a.SessionID
	switch a.State {
	case agents.Waiting:
		return !same || a.WaitRevision > p.WaitRevision || p.State != agents.Waiting
	case agents.Completed, agents.Failed:
		return !same || a.CompletionRevision > p.CompletionRevision || (a.CompletionRevision == 0 &&
			(p.State != agents.Completed && p.State != agents.Failed ||
				current.LastInput.After(p.Updated) && a.Updated.After(current.LastInput)))
	}
	return false
}

// LegacyCompletionState counts observed completions for version 4 servers.
// A fast turn can start and finish between polls, but it must have a fresh
// completion hook after new input. Draft edits alone do not complete anything.
// Keep the first completion timestamp across duplicate hooks so receipts and
// queued deliveries retain the same identity.
func LegacyCompletionState(previous, next SessionInfo) AgentState {
	s := next.Agent
	if s.CompletionRevision != 0 {
		return s
	}
	s.CompletionRevision = previous.Agent.CompletionRevision
	finished := s.State == agents.Completed || s.State == agents.Failed
	wasFinished := previous.Agent.State == agents.Completed || previous.Agent.State == agents.Failed
	same := s.SessionID == previous.Agent.SessionID && s.ID == previous.Agent.ID
	newInput := next.LastInput.After(previous.Agent.Updated) && s.Updated.After(next.LastInput)
	if finished && (!wasFinished || !same || newInput) {
		s.CompletionRevision++
	} else if finished && wasFinished && same {
		s.Updated = previous.Agent.Updated
	}
	return s
}

// AgentNoticeState remains true only while this exact reminder is relevant.
func AgentNoticeState(s SessionInfo) bool {
	return s.Agent.State == agents.Waiting || s.Agent.State == agents.Completed || s.Agent.State == agents.Failed
}
