package main

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"path"
	"retty/internal/agents"
	"retty/internal/rex"
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

func (m *mobileApp) sessionDirectory(s rex.SessionInfo) string {
	dir := strings.TrimRight(strings.ReplaceAll(s.Dir, "\\", "/"), "/")
	home := strings.TrimRight(strings.ReplaceAll(m.hello.Host.Home, "\\", "/"), "/")
	if dir == "" {
		return s.Dir
	}
	if dir == home && home != "" {
		return "~"
	}
	if home != "" && strings.HasPrefix(dir, home+"/") {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	parent := path.Base(path.Dir(dir))
	if parent == "." || parent == "/" {
		return path.Base(dir)
	}
	return parent + "/" + path.Base(dir)
}
func (m *mobileApp) sessionStatus(s rex.SessionInfo) string {
	if state := sessionAgentState(s); state.State != "" {
		switch state.State {
		case agents.Completed:
			return ""
		case agents.Waiting:
			switch state.Reason {
			case "permission", "question":
			default:
				return ""
			}
		}
		if label := agentStateLabel(state); label != "" {
			return label
		}
	}
	if s.Exited {
		return "已结束"
	}
	if s.Idle {
		return ""
	}
	return "运行中"
}
func (m *mobileApp) sessionValue(s rex.SessionInfo) string {
	value := m.sessionTitle(s)
	if status := m.sessionStatus(s); status != "" {
		value += " · " + status
	}
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

func (m *mobileApp) sessionList(c *ui.Context) {
	mobileListGroup(c).Children(func() {
		if len(m.sessions) == 0 {
			mobileListEmpty(c, "轻点右上角 ＋ 新建终端")
		}
		for i, s := range m.orderedSessions() {
			if i > 0 {
				mobileListDivider(c)
			}
			value := m.sessionValue(s)
			if m.reconnecting {
				value = m.sessionTitle(s) + " · 连接恢复后可打开"
			}
			row := mobileListRow(c, "session-"+s.ID, "打开会话 "+s.ID, 64).Value(value + " · " + s.Dir).TouchSelection().HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputLongPress && !m.reconnecting {
					m.editSession(s)
					c.Invalidate()
					return true
				}
				return false
			})
			row.Children(func() {
				mobileSessionIcon(c, sessionProgramName(s))
				mobileListText(c, mobileListTextOptions{
					Title: m.sessionTitle(s), Subtitle: m.sessionDirectory(s),
					TitleAccessory: func() {
						if m.preference(s.ID).Pinned {
							ui.Icon(c, icon("pin")).Role(ui.RoleImage).Label("置顶").Size(12, 12).TextColor(c.Theme().TextMuted)
						}
					},
					SubtitleAccessory: func() {
						status, color := m.sessionStatus(s), c.Theme().TextMuted
						switch sessionAgentState(s).State {
						case agents.Running:
							color = colorsOf(c).busy
						case agents.Waiting:
							color = colorsOf(c).attention
						case agents.Failed:
							color = c.Theme().Danger
						}
						if m.reconnecting {
							status, color = "重连中", colorsOf(c).attention
						} else if m.closingSession == s.ID {
							status = "正在结束"
						}
						if status != "" {
							ui.Row(c).Gap(4).Shrink(0).Children(func() {
								if m.reconnecting && m.recoveryAnimating() {
									mobileReconnectIcon(c, 12, color)
								}
								ui.Text(c, status).FontSize(11).TextColor(color).Shrink(0)
							})
						}
					},
				})
				if mobileIconAction(c, "会话设置 "+s.ID, "ellipsis").Disabled(m.reconnecting || m.closingSession != "").Clicked() {
					m.editSession(s)
				}
			})
			if row.Clicked() && !m.editingOpen {
				if m.reconnecting {
					m.connectionDetailsOpen = true
				} else {
					m.openSession(s)
				}
			}
		}
	})
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
	ui.DialogBase(c, &m.editingOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.4)).Column().Justify(ui.End).AlignItems(ui.Center).Padding(12)
		panel.Label("会话编辑面板").FillWidth().MaxWidth(480).Padding(16).Radius(20).Background(c.Theme().Surface).Column().Gap(10)
		ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "会话设置").FontSize(17).Bold().Grow(1)
			// The first focusable control is the close button, so opening the
			// sheet does not summon a keyboard before the name is tapped.
			if ui.ButtonBase(c).Label("取消").Role(ui.RoleButton).Size(44, 44).Radius(22).Background(c.Theme().Background).Children(func() {
				ui.Icon(c, icon("x")).Size(17, 17).TextColor(c.Theme().TextMuted)
			}).Clicked() {
				m.editingOpen = false
				c.Blur()
			}
		})
		originalName := "会话名称"
		for _, s := range m.sessions {
			if s.ID == m.editingSession {
				originalName = mobileSessionTitle(s)
				break
			}
		}
		ui.Column(c).FillWidth().Padding(10, 12).Radius(12).Background(c.Theme().Background).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, "名称").FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
				ui.Text(c, "留空使用原名称").FontSize(11).TextColor(c.Theme().TextMuted)
			})
			ui.TextInputBase(c, &m.editingName).Label("会话名称").Placeholder(originalName).FontSize(16).FillWidth().Height(40).
				InputOptions(ui.InputOptions{Return: ui.ReturnDone, Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone})
		})
		ui.SwitchBase(c, &m.editingPinned).Label("置顶会话").FillWidth().Height(48).Padding(0, 12).Radius(12).Background(c.Theme().Background).Gap(10).Children(func() {
			ui.Icon(c, icon("pin")).Size(17, 17).TextColor(c.Theme().TextMuted)
			ui.Text(c, "置顶").FontSize(15).Grow(1)
			color, left := c.Theme().Border, float32(2)
			if m.editingPinned {
				color, left = c.Theme().Accent, 18
			}
			ui.Box(c).Size(36, 20).Radius(10).Background(color).Children(func() {
				ui.Box(c).Absolute().Left(left).Top(2).Size(16, 16).Radius(8).Background(ui.RGB(255, 255, 255))
			})
		})
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			if ui.ButtonBase(c).Label("结束会话").Role(ui.RoleButton).Size(44, 44).Radius(12).Background(c.Theme().Background).Disabled(!m.connectionUsable() || m.closingSession != "").Children(func() {
				ui.Icon(c, icon("trash-2")).Size(17, 17).TextColor(c.Theme().Danger)
			}).Clicked() {
				for _, s := range m.sessions {
					if s.ID == m.editingSession {
						m.endingSession, m.endingOpen, m.editingOpen = s, true, false
						c.Blur()
						break
					}
				}
			}
			ui.Box(c).Grow(1)
			if ui.ButtonBase(c).Label("保存").Role(ui.RoleButton).Size(72, 44).Radius(12).Background(c.Theme().Accent).Children(func() {
				ui.Icon(c, icon("check")).Size(19, 19).TextColor(ui.RGB(255, 255, 255))
			}).Clicked() {
				m.setSessionPreference(m.editingSession, m.editingName, m.editingPinned)
				m.editingOpen = false
				c.Blur()
			}
		})
	})
}

func (m *mobileApp) sessionEndDialog(c *ui.Context) {
	ui.DialogBase(c, &m.endingOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(330).MaxWidth(330).Padding(20).Radius(18).Background(c.Theme().Surface).Column().Gap(14)
		ui.Text(c, "结束这个会话？").FontSize(18).Bold()
		ui.Text(c, m.sessionTitle(m.endingSession)).FontSize(15).FillWidth().SingleLine().Ellipsis("…")
		ui.Text(c, "这会终止此会话中的终端和正在运行的任务。").FontSize(14).LineHeight(1.4).TextColor(c.Theme().TextMuted).FillWidth()
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			if ui.ButtonBase(c).Label("取消").Role(ui.RoleButton).Height(44).Grow(1).Children(func() {
				ui.Icon(c, icon("x")).Size(22, 22).TextColor(c.Theme().TextMuted)
			}).Clicked() {
				m.endingOpen = false
				c.Blur()
			}
			if ui.ButtonBase(c).Label("确认结束会话").Role(ui.RoleButton).Height(44).Grow(1).Disabled(!m.connectionUsable()).Children(func() {
				ui.Icon(c, icon("trash-2")).Size(22, 22).TextColor(c.Theme().Danger)
			}).Clicked() {
				m.endingOpen = false
				m.endSession(m.endingSession.ID)
				c.Blur()
			}
		})
	})
}

func (m *mobileApp) endSession(sid string) {
	if sid == "" || !m.connectionUsable() || m.busy || m.closingSession != "" {
		return
	}
	m.closingSession, m.error = sid, ""
	client, generation := m.client, m.generation
	go func() {
		err := client.Kill(sid)
		mygo.RunOnMain(func() { m.finishSessionEnd(client, generation, sid, err) })
	}()
	m.invalidate()
}

func (m *mobileApp) finishSessionEnd(client *rex.Client, generation int, sid string, err error) {
	if m.client != client || m.generation != generation || m.closingSession != sid {
		return
	}
	m.closingSession = ""
	if err != nil {
		if m.selected.ID == sid && m.stream != nil && !m.stream.hasTransport() {
			m.connectionLost()
		}
		m.error = "结束失败：" + err.Error()
		m.invalidate()
		return
	}
	if m.closedSessions == nil {
		m.closedSessions = map[string]bool{}
	}
	m.closedSessions[m.preferenceKey(sid)] = true
	if m.selected.ID == sid {
		m.detach()
	}
	if m.resumeSID == sid {
		m.resumeSID = ""
	}
	m.setSessionPreference(sid, "", false)
	m.updateSessions(m.sessions, false)
	m.invalidate()
}
