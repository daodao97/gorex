package main

import (
	"github.com/egoist/mygo/ui"
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
			session.Agent = legacyCompletionState(previous, session)
		}
		sessions[i] = session
		next[key] = session
		state := sessionAgentState(session)
		// A one-shot command may finish and return to its shell between polls.
		if state.State == "" && (session.Agent.State == agents.Completed || session.Agent.State == agents.Failed) {
			state = session.Agent
		}
		if m.notice != nil && m.notice.Session == session.ID && state.State != agents.Waiting && state.State != agents.Completed && state.State != agents.Failed {
			m.notice = nil
		}
		if rex.AgentNoticeState(session) {
			id := m.taskNoticeID(session)
			if initial || m.pushDisabled || m.term != nil && m.selected.ID == session.ID && !m.background {
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
		// Reading the target terminal is already the most direct reminder.
		if m.term != nil && m.selected.ID == session.ID && !m.background {
			continue
		}
		id := m.taskNoticeID(session)
		if !m.rememberNotice(id) {
			continue
		}
		notice := mobileAgentNotice{ID: id, Desktop: m.desktopKey(), Session: session.ID, Title: programOf(state.ID).Name + " · " + agentStateLabel(state), Body: m.sessionTitle(session)}
		m.notice = &notice
		if m.agentNotify != nil {
			m.agentNotify(notice)
		}
	}
	m.refreshPushSnapshot()
	m.syncPushRegistration()
	m.agentPrevious = next
	m.sessions = sessions
	if m.notice != nil {
		if _, exists := next[m.preferenceKey(m.notice.Session)]; !exists {
			m.notice = nil
		}
	}
}

func (m *mobileApp) noticeView(c *ui.Context) {
	if m.notice == nil || m.editingOpen {
		return
	}
	notice := *m.notice
	ui.Overlay(c, func() {
		ui.Row(c).Absolute().Left(16).Right(16).Bottom(16).Radius(14).Padding(4, 8).Gap(4).Background(c.Theme().Surface).Children(func() {
			if ui.ButtonBase(c).Label("任务提醒 " + notice.Title).Height(60).Grow(1).Padding(8).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, notice.Title).FontSize(14).Bold().SingleLine().Ellipsis("…")
					ui.Text(c, notice.Body).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…")
				})
			}).Clicked() {
				m.openNotifiedSession(notice.Desktop, notice.Session)
			}
			if mobileTextAction(c, "关闭任务提醒", "×").Clicked() {
				m.notice = nil
			}
		})
	})
}

func (m *mobileApp) openNotifiedSession(desktop, sid string) {
	if desktop == "" || sid == "" {
		return
	}
	if desktop == m.desktopKey() && m.client != nil {
		for _, s := range m.sessions {
			if s.ID == sid {
				m.notice = nil
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
