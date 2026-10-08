package main

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gorex/internal/agents"
	"gorex/internal/rex"
	"sort"
	"strings"
	"unicode/utf8"
)

type mobileSessionPreference struct {
	Name   string `json:"name,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
}

func (m *mobileApp) desktopKey() string {
	if m.hello.Host.ID != "" {
		return m.hello.Host.ID
	}
	return m.link
}
func (m *mobileApp) preferenceKey(sid string) string { return m.desktopKey() + "\x00" + sid }
func (m *mobileApp) preference(sid string) mobileSessionPreference {
	return m.sessionPreferences[m.preferenceKey(sid)]
}
func (m *mobileApp) sessionTitle(s rex.SessionInfo) string {
	if name := m.preference(s.ID).Name; name != "" {
		return name
	}
	return mobileSessionTitle(s)
}
func (m *mobileApp) sessionStatus(s rex.SessionInfo) string {
	if state := sessionAgentState(s); state.State != "" {
		if label := agentStateLabel(state); label != "" {
			return label
		}
	}
	if s.Exited {
		return "已结束"
	}
	if s.Idle {
		return "等待输入"
	}
	return "运行中"
}
func (m *mobileApp) sessionValue(s rex.SessionInfo) string {
	value := m.sessionTitle(s) + " · " + m.sessionStatus(s)
	if m.preference(s.ID).Pinned {
		value += " · 置顶"
	}
	return value
}
func (m *mobileApp) orderedSessions() []rex.SessionInfo {
	sessions := append([]rex.SessionInfo(nil), m.sessions...)
	sort.SliceStable(sessions, func(i, j int) bool {
		a, b := sessionAgentState(sessions[i]), sessionAgentState(sessions[j])
		if (a.State == agents.Waiting) != (b.State == agents.Waiting) {
			return a.State == agents.Waiting
		}
		return m.preference(sessions[i].ID).Pinned && !m.preference(sessions[j].ID).Pinned
	})
	return sessions
}

func (m *mobileApp) setSessionPreference(sid, name string, pinned bool) {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	if m.sessionPreferences == nil {
		m.sessionPreferences = map[string]mobileSessionPreference{}
	}
	key := m.preferenceKey(sid)
	if name == "" && !pinned {
		delete(m.sessionPreferences, key)
	} else {
		m.sessionPreferences[key] = mobileSessionPreference{Name: name, Pinned: pinned}
	}
	m.preferenceEpoch++
	if m.preferenceTouched == nil {
		m.preferenceTouched = map[string]bool{}
	}
	m.preferenceTouched[key] = true
	m.persistSessionPreferences()
	m.invalidate()
}
func (m *mobileApp) loadSessionPreferences() {
	if m.store == nil || m.storage == nil {
		return
	}
	epoch := m.preferenceEpoch
	m.storage <- func() {
		saved, err := m.store.Get("session-preferences")
		var preferences map[string]mobileSessionPreference
		if err == nil {
			json.Unmarshal(saved, &preferences)
		}
		mygo.RunOnMain(func() { m.applyLoadedSessionPreferences(preferences, epoch) })
	}
}
func (m *mobileApp) persistSessionPreferences() {
	if m.store == nil || m.storage == nil {
		return
	}
	saved, _ := json.Marshal(m.sessionPreferences)
	epoch := m.preferenceEpoch
	m.storage <- func() {
		if err := m.store.Set("session-preferences", saved); err != nil {
			mygo.RunOnMain(func() {
				if m.preferenceEpoch == epoch {
					m.error = "无法保存会话设置，请重试"
					m.invalidate()
				}
			})
		}
	}
}

func (m *mobileApp) applyLoadedSessionPreferences(preferences map[string]mobileSessionPreference, epoch int) {
	// Locally edited/deleted entries win over delayed Keychain reads.
	if m.preferenceEpoch == epoch {
		m.sessionPreferences = preferences
	} else {
		if m.sessionPreferences == nil {
			m.sessionPreferences = map[string]mobileSessionPreference{}
		}
		for key, value := range preferences {
			if !m.preferenceTouched[key] {
				m.sessionPreferences[key] = value
			}
		}
		// An earlier queued save may lack the entries just loaded. Persist the
		// merged snapshot last so unrelated desktops' names are not lost.
		m.persistSessionPreferences()
	}
	m.invalidate()
}

func (m *mobileApp) editSession(s rex.SessionInfo) {
	p := m.preference(s.ID)
	m.editingSession, m.editingName, m.editingPinned, m.editingOpen = s.ID, p.Name, p.Pinned, true
}
func (m *mobileApp) sessionEditor(c *ui.Context) {
	ui.DialogBase(c, &m.editingOpen, func(back, panel *ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(330).MaxWidth(330).Padding(20).Radius(18).Background(c.Theme().Surface).Column().Gap(14)
		ui.Text(c, "会话设置").FontSize(18).Bold()
		ui.TextInput(c, &m.editingName).Label("会话名称").Placeholder("留空使用原名称").FillWidth().Height(44).InputOptions(ui.InputOptions{Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone})
		ui.SwitchBase(c, &m.editingPinned).Label("置顶会话").FillWidth().Height(44).Children(func() {
			ui.Text(c, "置顶").Grow(1)
			color, left := c.Theme().Border, float32(2)
			if m.editingPinned {
				color, left = c.Theme().Accent, 18
			}
			ui.Box(c).Size(36, 20).Radius(10).Background(color).Children(func() {
				ui.Box(c).Absolute().Left(left).Top(2).Size(16, 16).Radius(8).Background(ui.RGB(255, 255, 255))
			})
		})
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			if ui.Button(c, "取消").Height(44).Grow(1).Clicked() {
				m.editingOpen = false
				c.Blur()
			}
			if ui.PrimaryButton(c, "保存").Height(44).Grow(1).Clicked() {
				m.setSessionPreference(m.editingSession, m.editingName, m.editingPinned)
				m.editingOpen = false
				c.Blur()
			}
		})
	})
}
