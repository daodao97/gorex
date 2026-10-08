package rex

import (
	"crypto/sha256"
	"fmt"
	"gorex/internal/agents"
	"strconv"
)

// AgentNoticeID is shared by local reminders, APNs and foreground receipts.
// Older servers lack completion revisions, so actual input timestamps identify
// their turns. Updated is deliberately excluded: duplicate hooks may change it.
func AgentNoticeID(desktop string, s SessionInfo) string {
	a := s.Agent
	revision := a.WaitRevision
	if a.State == agents.Completed || a.State == agents.Failed {
		revision = a.CompletionRevision
	}
	key := desktop + "\x00" + s.ID + "\x00" + a.ID + "\x00" + a.SessionID + "\x00" + a.State + "\x00" + strconv.FormatUint(revision, 10)
	if a.State != agents.Waiting && revision == 0 {
		key += "\x00" + strconv.FormatInt(s.LastInput.UnixNano(), 10)
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
			(p.State != agents.Completed && p.State != agents.Failed || current.LastInput.After(previous.LastInput)))
	}
	return false
}

// AgentNoticeState remains true only while this exact reminder is relevant.
func AgentNoticeState(s SessionInfo) bool {
	return s.Agent.State == agents.Waiting || s.Agent.State == agents.Completed || s.Agent.State == agents.Failed
}
