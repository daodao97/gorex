package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/agents"
	"gorex/internal/rex"
)

// An old hook must not put an agent badge on the shell or another program
// after the agent exited. Unintegrated agents keep the ordinary activity dot.
func paneAgentState(p *Pane) rex.AgentState {
	return sessionAgentState(p.info)
}

func sessionAgentState(info rex.SessionInfo) rex.AgentState {
	s := info.Agent
	if s.State == "" || info.Idle || info.Exited {
		return rex.AgentState{}
	}
	if agent, ok := agents.Detect(info.Program, info.Args); !ok || agent.ID != s.ID {
		return rex.AgentState{}
	}
	return s
}

func agentStateLabel(s rex.AgentState) string {
	switch s.State {
	case agents.Ready:
		return "就绪"
	case agents.Running:
		return "执行中"
	case agents.Waiting:
		switch s.Reason {
		case "permission":
			return "等待授权"
		case "question":
			return "等待回答"
		default:
			return "等待输入"
		}
	case agents.Completed:
		return "已完成"
	case agents.Failed:
		return "执行失败"
	}
	return ""
}

func tabAgentState(t *Tab) (*Pane, rex.AgentState) {
	priority := map[string]int{agents.Waiting: 5, agents.Failed: 4, agents.Running: 3, agents.Completed: 2, agents.Ready: 1}
	var chosen *Pane
	var state rex.AgentState
	for _, p := range t.panes() {
		s := paneAgentState(p)
		if priority[s.State] > priority[state.State] || (priority[s.State] > 0 && priority[s.State] == priority[state.State] && p == t.Focus) {
			chosen, state = p, s
		}
	}
	return chosen, state
}

func (a *App) agentIndicator(c *ui.Context, k *colors, p *Pane, s rex.AgentState) {
	if p == nil || s.State == "" {
		return
	}
	label := "Agent " + s.State
	if s.State == agents.Waiting {
		label = "Agent waiting for input"
	}
	name := programOf(s.ID).Name
	tip := name + " · " + agentStateLabel(s)
	e := ui.Box(c).Key("agent-state-"+p.SID).Size(13, 16).Shrink(0).Center().
		Role(ui.RoleButton).Label(label).Tooltip(tip + " · 点击定位窗格").Cursor(ui.CursorPointer)
	e.Children(func() {
		switch s.State {
		case agents.Waiting:
			ui.Text(c, "Ⅱ").FontSize(11).FontWeight(700).TextColor(k.attention)
		case agents.Running:
			ui.Box(c).Size(6, 6).Radius(3).Background(k.busy)
		case agents.Completed:
			ui.Icon(c, icon("check")).Size(12, 12).TextColor(k.busy)
		case agents.Failed:
			ui.Icon(c, icon("x")).Size(12, 12).TextColor(ui.Hex("#e56c6c"))
		default:
			ui.Icon(c, icon("circle-dot")).Size(9, 9).TextColor(k.textFaint)
		}
	})
	if e.Clicked() {
		a.later(c, func() { a.focusAgentPane(p.SID) })
	}
}

func (a *App) paneIsViewed(p *Pane) bool {
	t := a.tab()
	return a.focusedWin && !a.settingsOpen && !a.paletteOpen && t == p.Tab && t != nil && t.Focus == p && (t.Zoom == nil || t.Zoom == p)
}

func (a *App) closeAgentNotice(sid string) {
	if close := a.agentNotices[sid]; close != nil {
		close()
		delete(a.agentNotices, sid)
		delete(a.agentNoticeKinds, sid)
	}
}

func (a *App) updateAgentNotice(p *Pane, previous rex.AgentState) {
	s := paneAgentState(p)
	// A completed one-shot agent may already have returned to the shell
	// before polling. The authenticated event can still announce its result.
	if s.State == "" && (p.info.Agent.State == agents.Completed || p.info.Agent.State == agents.Failed) {
		s = p.info.Agent
	}
	finished := s.State == agents.Completed || s.State == agents.Failed
	if s.State != agents.Waiting && !finished {
		if s.State != "" || a.agentNoticeKinds[p.SID] == agents.Waiting {
			a.closeAgentNotice(p.SID)
		}
		return
	}
	if (s.State != previous.State && !finished) || (finished && a.agentNoticeKinds[p.SID] == agents.Waiting) {
		a.closeAgentNotice(p.SID)
	}
	var hidden bool
	if finished {
		if a.agentFinishedNotified == nil {
			a.agentFinishedNotified = map[string]uint64{}
		}
		if s.CompletionRevision <= a.agentFinishedNotified[p.SID] {
			return
		}
		a.agentFinishedNotified[p.SID] = s.CompletionRevision
		// Restoring a window must not announce historical completed turns.
		if previous.SessionID == s.SessionID && previous.CompletionRevision == s.CompletionRevision && (previous.State == agents.Completed || previous.State == agents.Failed) {
			return
		}
		hidden = prefs.HideAgentCompletionNotifications
	} else {
		if a.agentNotified == nil {
			a.agentNotified = map[string]uint64{}
		}
		if s.WaitRevision <= a.agentNotified[p.SID] {
			return
		}
		a.agentNotified[p.SID] = s.WaitRevision
		hidden = prefs.HideAgentNotifications
	}
	if hidden || a.paneIsViewed(p) {
		return
	}
	opts := mygo.NotificationOptions{Title: programOf(s.ID).Name + " · " + agentStateLabel(s), Body: rex.AgentNoticeBody(rex.Dir(), p.info)}
	a.showPaneNotice(p, s.State, opts)
}

func (a *App) showPaneNotice(p *Pane, kind string, opts mygo.NotificationOptions) {
	// Headless views use an injected notifier, never the native AppKit API.
	if a.win == nil && a.agentNotify == nil {
		return
	}
	if a.agentNotices == nil {
		a.agentNotices = map[string]func(){}
	}
	a.closeAgentNotice(p.SID)
	sid := p.SID
	click := func() {
		if a.win == nil && a.agentNotify == nil {
			a.open()
		}
		if a.win != nil {
			if a.win.IsMinimized() {
				a.win.Restore()
			}
			a.win.Show()
			a.win.Focus()
			a.win.Update(func() { a.focusAgentPane(sid) })
		} else {
			a.focusAgentPane(sid)
		}
	}
	if a.agentNotify != nil {
		a.agentNotices[sid] = a.agentNotify(opts, click)
	} else {
		n := mygo.NewNotification(opts)
		n.OnClick(click)
		if err := n.Show(); err != nil {
			a.agentHookError = fmt.Sprintf("无法发送通知：%v。请检查系统设置中的通知权限。", err)
			n.Close()
			return
		}
		a.agentNotices[sid] = n.Close
	}
	if a.agentNoticeKinds == nil {
		a.agentNoticeKinds = map[string]string{}
	}
	a.agentNoticeKinds[sid] = kind
}

func (a *App) focusAgentPane(sid string) bool {
	for i, t := range a.tabs {
		for _, p := range t.panes() {
			if p.SID != sid || p.closed {
				continue
			}
			a.settingsOpen, a.paletteOpen, a.renaming = false, false, nil
			p.find.open, p.find.focus = false, false
			if t.Zoom != nil && t.Zoom != p {
				t.Zoom = nil
			}
			t.setFocus(p)
			a.selectTab(i)
			return true
		}
	}
	return false
}
