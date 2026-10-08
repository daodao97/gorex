package main

import (
	"gorex/internal/agents"
	"gorex/internal/rex"
)

type mobileAgentNotice struct{ ID, Desktop, Session, Title, Body string }

func (m *mobileApp) updateSessions(sessions []rex.SessionInfo, initial bool) {
	if m.agentPrevious == nil {
		m.agentPrevious = map[string]rex.SessionInfo{}
	}
	next := make(map[string]rex.SessionInfo, len(sessions))
	for i := range sessions {
		session := sessions[i]
		key := m.preferenceKey(session.ID)
		previous, seen := m.agentPrevious[key]
		if m.hello.Version < 5 {
			session.Agent = rex.LegacyCompletionState(previous, session)
		}
		sessions[i] = session
		next[key] = session
		state := sessionAgentState(session)
		// A one-shot command may finish and return to its shell between polls.
		if state.State == "" && (session.Agent.State == agents.Completed || session.Agent.State == agents.Failed) {
			state = session.Agent
		}
		if rex.AgentNoticeState(session) {
			id := m.taskNoticeID(session)
			if initial || m.pushDisabled || !m.background {
				m.rememberNotice(id)
			}
		}
		if initial || !seen || m.pushDisabled {
			continue
		}
		same := state.ID == previous.Agent.ID && state.SessionID == previous.Agent.SessionID
		waiting := state.State == agents.Waiting && (!same || state.WaitRevision > previous.Agent.WaitRevision || previous.Agent.State != agents.Waiting)
		finished := (state.State == agents.Completed || state.State == agents.Failed) && (!same || state.CompletionRevision > previous.Agent.CompletionRevision)
		if !waiting && !finished {
			continue
		}
		// Foreground events update session status quietly on every mobile page.
		if !m.background {
			continue
		}
		id := m.taskNoticeID(session)
		if !m.rememberNotice(id) {
			continue
		}
		notice := mobileAgentNotice{ID: id, Desktop: m.desktopKey(), Session: session.ID, Title: programOf(state.ID).Name + " · " + agentStateLabel(state), Body: m.sessionTitle(session)}
		if m.agentNotify != nil {
			m.agentNotify(notice)
		}
	}
	m.refreshPushSnapshot()
	m.syncPushRegistration()
	m.agentPrevious = next
	m.sessions = sessions
	m.refreshRecentSessions()
}

func (m *mobileApp) openNotifiedSession(desktop, sid string) {
	if desktop == "" || sid == "" {
		return
	}
	if m.background && desktop == m.desktopKey() && m.client != nil {
		m.home, m.resumeSID = false, sid
		return
	}
	if desktop == m.desktopKey() && m.connectionUsable() {
		m.home = false
		for _, s := range m.sessions {
			if s.ID == sid {
				if m.selected.ID != sid || m.term == nil {
					m.openSession(s)
				}
				m.creating = false
				m.invalidate()
				return
			}
		}
		m.error = "提醒对应的会话已结束"
		m.invalidate()
		return
	}
	for _, entry := range m.history {
		if entry.ID == desktop || entry.Link == desktop {
			m.connect(entry.Link)
			m.resumeSID = sid
			return
		}
	}
	// A notification can arrive before the asynchronous Keychain load.
	m.pendingDesktop, m.pendingSession = desktop, sid
}
