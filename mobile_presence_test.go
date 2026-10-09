package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"retty/internal/rex"
)

func TestRecentAvailabilityStaysInCompactRow(t *testing.T) {
	registerFonts()
	m := &mobileApp{
		history:  []desktopRecent{{Name: "我的 MacBook Pro", Link: "fixture"}},
		presence: make(map[string]desktopPresence),
	}
	tt := ui.NewTester(m.view, 375, 620)
	for _, state := range []desktopPresenceState{desktopChecking, desktopOnline, desktopUnavailable} {
		m.presence["fixture"] = desktopPresence{state: state}
		tt.Frame()
		row, ok := tt.Find("重新连接 我的 MacBook Pro")
		if !ok || row.H != 56 || row.X+row.W > 375 {
			t.Fatalf("availability changed the reconnect row: %+v", row)
		}
		status, ok := tt.Find("设备状态 我的 MacBook Pro " + state.label())
		if !ok || status.X+status.W > row.X+row.W || status.Y < row.Y || status.Y+status.H > row.Y+row.H {
			t.Fatalf("availability is clipped: %+v", status)
		}
	}
}

func TestRecentPreviewRepairsLegacyIconsWithoutOpeningDesktop(t *testing.T) {
	registerFonts()
	m := &mobileApp{
		history: []desktopRecent{
			{ID: "mac", Name: "MacBook Pro (2)", Link: "mac-link"},
			{ID: "other", Name: "Other", Link: "other-link", OS: "windows"},
		},
		recentSessions: []mobileRecentSession{
			{Desktop: "mac", Session: "claude", Title: "✳\ufe0f Claude Code"},
			{Desktop: "mac-link", Session: "codex", Title: "旧任务标题"},
			{Desktop: "other", Session: "codex", Title: "其他电脑的任务", Program: "python3"},
		},
	}
	tt := ui.NewTester(m.view, 390, 844)
	hello := rex.Hello{Host: rex.HostInfo{OS: "macOS 26.0"}}
	sessions := []rex.SessionInfo{
		{ID: "claude", Program: "claude", Title: "✳\ufe0f Claude Code"},
		{ID: "codex", Program: "node", Args: []string{"node", "/opt/node_modules/@openai/codex/bin/codex.js"}, Title: "修复登录流程"},
	}
	m.applyRecentDesktopMetadata("mac-link", hello, sessions)
	tt.Frame()
	for _, label := range []string{"macOS icon", "Claude Code icon", "Codex icon", "Python icon"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("home preview did not repair %s", label)
		}
	}
	if m.recentSessions[0].Title != "Claude Code" || m.recentSessions[1].Title != "修复登录流程" {
		t.Fatal("home titles differ from the current task", m.recentSessions)
	}
	if m.client != nil || m.link != "" || m.hello.Host.ID != "" || len(m.sessions) != 0 || m.term != nil {
		t.Fatal("preview opened a desktop or replaced active session state")
	}
	if m.recentSessions[2].Program != "python3" || m.history[1].OS != "windows" {
		t.Fatal("preview modified another desktop")
	}
	saveSettingsImage(t, tt, "mobile-legacy-icons-repaired-light")
	tt.SetDark(true)
	tt.Frame()
	saveSettingsImage(t, tt, "mobile-legacy-icons-repaired-dark")
	// A late preview after history was cleared must not restore old records.
	m.history, m.recentSessions = nil, nil
	m.applyRecentDesktopMetadata("mac-link", hello, sessions)
	if len(m.history) != 0 || len(m.recentSessions) != 0 {
		t.Fatal("preview restored cleared history")
	}
}
