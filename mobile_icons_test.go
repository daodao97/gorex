package main

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"retty/internal/rex"
)

func TestMobileHistorySystemMetadata(t *testing.T) {
	for _, tc := range []struct {
		host rex.HostInfo
		want string
	}{
		{rex.HostInfo{OS: "macOS 26.0"}, "darwin"},
		{rex.HostInfo{OS: "Darwin"}, "darwin"},
		{rex.HostInfo{OS: "Windows 11"}, "windows"},
		{rex.HostInfo{OS: "linux"}, "linux"},
		{rex.HostInfo{OS: "Ubuntu 24.04.3 LTS", Model: "Linux"}, "linux"},
		{rex.HostInfo{}, ""},
	} {
		if got := desktopPlatform(tc.host); got != tc.want {
			t.Fatalf("platform for %+v = %q, want %q", tc.host, got, tc.want)
		}
	}
	latest, older := recentTestLink(), recentTestLink()
	for _, previousLink := range []string{latest, older} {
		history := mergeDesktopHistory([]desktopRecent{{ID: "mac", Name: "Mac", Link: latest}},
			[]desktopRecent{{ID: "mac", Name: "Mac", Link: previousLink, OS: "darwin"}})
		data, err := json.Marshal(history)
		if err != nil {
			t.Fatal(err)
		}
		var loaded []desktopRecent
		if err := json.Unmarshal(data, &loaded); err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 || loaded[0].OS != "darwin" || loaded[0].Link != latest {
			t.Fatal("history load or link rotation lost the system icon", loaded)
		}
	}
}

func TestMobileRecentSessionIconsPersistAndRefresh(t *testing.T) {
	m := &mobileApp{history: []desktopRecent{{ID: "mac", Name: "Mac", Link: recentTestLink(), OS: "darwin"}}}
	m.hello.Host.ID = "mac"
	m.rememberSession(rex.SessionInfo{ID: "task", Shell: "zsh", Program: "node", Args: []string{"node", "/opt/node_modules/@openai/codex/bin/codex.js"}})
	data, err := json.Marshal(m.recentSessions)
	if err != nil {
		t.Fatal(err)
	}
	var saved []mobileRecentSession
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	m.recentSessions = nil
	m.applyLoadedRecentSessions(saved, m.historyEpoch)
	if len(m.recentSessions) != 1 || programOf(m.recentSessions[0].Program).Glyph != "agent:codex" {
		t.Fatal("disconnected shortcut lost its wrapped Codex icon", m.recentSessions)
	}
	m.client = &rex.Client{}
	m.sessions = []rex.SessionInfo{{ID: "task", Program: "python3", Shell: "zsh"}}
	m.refreshRecentSessions()
	if programOf(m.recentSessions[0].Program).Glyph != "brand:python" {
		t.Fatal("shortcut did not follow the foreground program")
	}
	m.sessions[0].Idle = true
	m.refreshRecentSessions()
	if got := m.recentSessions[0].Program; got != "zsh" {
		t.Fatal("idle shell retained its previous task icon", got)
	}
}

func TestMobileHomeShowsSystemAndTaskIcons(t *testing.T) {
	registerFonts()
	m := &mobileApp{
		history: []desktopRecent{
			{ID: "mac", Name: "我的 Mac", Link: recentTestLink(), OS: "darwin"},
			{ID: "win", Name: "Windows PC", Link: recentTestLink(), OS: "windows"},
			{ID: "linux", Name: "Linux Server", Link: recentTestLink(), OS: "linux"},
		},
		recentSessions: []mobileRecentSession{
			{Desktop: "mac", Session: "codex", Title: "修复登录流程", Program: "codex"},
			{Desktop: "win", Session: "claude", Title: "代码审查", Program: "claude"},
			{Desktop: "linux", Session: "python", Title: "数据处理", Program: "python3"},
			{Desktop: "mac", Session: "shell", Title: "终端会话"},
		},
	}
	tt := ui.NewTester(m.view, 390, 844)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		for _, label := range []string{"macOS icon", "Windows icon", "Linux icon", "Codex icon", "Claude Code icon", "Python icon", "终端 icon"} {
			if rect, ok := tt.Find(label); !ok || rect.W != 32 || rect.H != 32 {
				t.Fatalf("missing or incorrectly sized %s: %+v", label, rect)
			}
		}
		name := "mobile-system-task-icons-light"
		if dark {
			name = "mobile-system-task-icons-dark"
		}
		saveSettingsImage(t, tt, name)
	}
}
