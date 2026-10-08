package main

import (
	"net"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gorex/internal/rex"
)

func TestMobileHistorySelectionClearsOnlyChosenRecords(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	a, b, c := recentTestLink(), recentTestLink(), recentTestLink()
	m := &mobileApp{
		home: true, client: client, link: b,
		history: []desktopRecent{
			{ID: "a", Name: "MacBook", Link: a, OS: "darwin"},
			{ID: "b", Name: "PC", Link: b, OS: "windows"},
			{ID: "c", Name: "Server", Link: c, OS: "linux"},
		},
		recentSessions: []mobileRecentSession{
			{Desktop: "a", Session: "a1"}, {Desktop: a, Session: "legacy-a"},
			{Desktop: "b", Session: "b1"}, {Desktop: "c", Session: "c1"},
		},
	}
	m.hello.Host.ID = "b"
	tt := ui.NewTester(m.view, 390, 750)
	tt.Click("清除连接记录")
	if m.historySelection == nil || len(m.history) != 3 || m.client != client {
		t.Fatal("entering selection cleared history or disconnected")
	}
	if _, ok := tt.Find("确认清除连接记录"); !ok {
		t.Fatal("clear did not become confirm")
	}
	for _, label := range []string{"macOS icon", "Windows icon", "Linux icon"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("selection retained the system icon %s", label)
		}
	}
	// Confirming an empty selection simply leaves selection mode.
	tt.Click("确认清除连接记录")
	if m.historySelection != nil || len(m.history) != 3 || m.client != client {
		t.Fatal("empty confirmation cleared records")
	}
	tt.Click("清除连接记录")
	tt.Click("选择连接 MacBook")
	tt.Click("选择连接 Server")
	tt.Click("选择连接 Server") // Unchecking must keep this record.
	if !m.historySelection[a] || m.historySelection[c] || !m.home || m.busy || m.client != client {
		t.Fatal("selection opened a desktop or failed to toggle")
	}
	saveSettingsImage(t, tt, "mobile-history-selected")
	epoch := m.historyEpoch
	previous := append([]desktopRecent(nil), m.history...)
	previousSessions := append([]mobileRecentSession(nil), m.recentSessions...)
	tt.Click("确认清除连接记录")
	if m.historySelection != nil || len(m.history) != 2 || m.history[0].ID != "b" || m.history[1].ID != "c" || m.client != client {
		t.Fatal("confirmation removed an unchecked desktop or active connection", m.history)
	}
	if len(m.recentSessions) != 2 || m.recentSessions[0].Desktop != "b" || m.recentSessions[1].Desktop != "c" {
		t.Fatal("selected desktop shortcuts were not removed selectively", m.recentSessions)
	}
	m.applyLoadedConnectionHistory(previous, previousSessions, epoch)
	if len(m.history) != 2 || len(m.recentSessions) != 2 {
		t.Fatal("late storage read restored deleted records")
	}
	// Selecting the active record closes only this phone's connection.
	tt.Click("清除连接记录")
	tt.Click("选择连接 PC")
	tt.Click("确认清除连接记录")
	if m.client != nil || m.link != "" || len(m.history) != 1 || m.history[0].ID != "c" {
		t.Fatal("clearing the active record removed another desktop or kept its connection")
	}
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("selected control connection stayed open")
	}
	tt.Click("清除连接记录")
	tt.Click("选择连接 Server")
	tt.Click("确认清除连接记录")
	if len(m.history) != 0 || len(m.recentSessions) != 0 || m.historySelection != nil {
		t.Fatal("clearing the final record retained history or selection")
	}
	if _, ok := tt.Find("清除连接记录"); ok {
		t.Fatal("empty history retained the clear action")
	}
}
