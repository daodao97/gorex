package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/tailscale/tailcat"
	"gorex/internal/remote"
	"gorex/internal/rex"
)

func recentTestLink() string {
	key := tailcat.NewPrivateKey()
	key.Public.RegionID = 301
	return remote.Link(key.Public.Addr())
}

func TestMobileRecentSessionsRememberOrderAndDesktopIdentity(t *testing.T) {
	m := &mobileApp{}
	m.hello.Host.ID = "mac-a"
	m.rememberSession(rex.SessionInfo{ID: "same", Title: "first"})
	m.hello.Host.ID = "mac-b"
	m.rememberSession(rex.SessionInfo{ID: "same", Title: "second"})
	m.hello.Host.ID = "mac-a"
	m.rememberSession(rex.SessionInfo{ID: "same", Title: "renamed"})
	if len(m.recentSessions) != 2 || m.recentSessions[0].Title != "renamed" || m.recentSessions[1].Desktop != "mac-b" {
		t.Fatal("recents did not deduplicate per desktop or move to the top", m.recentSessions)
	}
	m.rememberSession(rex.SessionInfo{ID: "ended", Exited: true})
	if len(m.recentSessions) != 2 {
		t.Fatal("ended session was remembered")
	}
	for i := range 12 {
		m.rememberSession(rex.SessionInfo{ID: fmt.Sprint(i)})
	}
	if len(m.recentSessions) != mobileRecentSessionLimit || m.recentSessions[0].Session != "11" {
		t.Fatal("recents exceeded their limit or lost newest order")
	}
}

func TestMobileRecentSessionsLoadMergesEarlyOpenAndRotatedLink(t *testing.T) {
	oldLink, newLink := recentTestLink(), recentTestLink()
	m := &mobileApp{history: []desktopRecent{{ID: "mac", Name: "Mac", Link: newLink}}}
	m.hello.Host.ID = "mac"
	m.rememberSession(rex.SessionInfo{ID: "early", Title: "already opened"})
	saved := []mobileRecentSession{{Desktop: oldLink, Session: "saved", Title: "previous task"}, {Desktop: oldLink, Session: "early", Title: "old title"}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var loaded []mobileRecentSession
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	m.applyLoadedConnectionHistory([]desktopRecent{{Name: "Mac", Link: oldLink}}, loaded, m.historyEpoch)
	if len(m.recentSessions) != 2 || m.recentSessions[0].Title != "already opened" || m.recentSessions[1].Desktop != "mac" {
		t.Fatal("late storage load lost early open or old link identity", m.recentSessions)
	}
	desktop, ok := m.recentDesktop(m.recentSessions[1].Desktop)
	if !ok || desktop.Link != newLink {
		t.Fatal("shortcut would use the obsolete connection capability")
	}
	m.setSessionPreference("saved", "自定义名称", false)
	if m.recentSessionTitle(m.recentSessions[1]) != "自定义名称" {
		t.Fatal("shortcut ignored custom session name")
	}
	epoch := m.historyEpoch
	m.disconnect(true)
	m.applyLoadedConnectionHistory([]desktopRecent{{ID: "mac", Name: "Mac", Link: oldLink}}, loaded, epoch)
	if len(m.recentSessions) != 0 || len(m.history) != 0 {
		t.Fatal("late Keychain load restored cleared connections or sessions")
	}
}

func TestMobileRecentSessionsRefreshOnlyConnectedDesktop(t *testing.T) {
	m := &mobileApp{
		client:  &rex.Client{},
		history: []desktopRecent{{ID: "a", Link: recentTestLink()}, {ID: "b", Link: recentTestLink()}},
		recentSessions: []mobileRecentSession{
			{Desktop: "a", Session: "alive", Title: "old"},
			{Desktop: "b", Session: "unreachable"},
			{Desktop: "a", Session: "gone"},
			{Desktop: "a", Session: "ended"},
		},
	}
	m.hello.Host.ID = "a"
	m.sessions = []rex.SessionInfo{{ID: "alive", Title: "new"}, {ID: "ended", Exited: true}}
	m.refreshRecentSessions()
	if len(m.recentSessions) != 2 || m.recentSessions[0].Title != "new" || m.recentSessions[1].Desktop != "b" {
		t.Fatal("refresh removed an unreachable desktop or kept ended sessions", m.recentSessions)
	}
	m.openSessionID("gone")
	if m.error == "" || m.term != nil || m.creating {
		t.Fatal("missing shortcut opened a new session or failed silently")
	}
}

func TestMobileRecentSessionHomeRowOpensTargetWithoutChangingDesktopSize(t *testing.T) {
	registerFonts()
	control, peer := net.Pipe()
	defer peer.Close()
	attached := make(chan rex.Attach, 1)
	client := rex.NewClient(control, func(context.Context) (net.Conn, error) {
		conn, remote := net.Pipe()
		go func() {
			defer remote.Close()
			var request rex.Attach
			if json.NewDecoder(remote).Decode(&request) == nil {
				attached <- request
				io.WriteString(remote, "{\"ok\":true}\n")
				io.Copy(io.Discard, remote)
			}
		}()
		return conn, nil
	})
	defer client.Close()
	m := &mobileApp{client: client, history: []desktopRecent{{ID: "mac", Name: "我的 Mac", Link: recentTestLink()}}}
	defer m.detach()
	m.hello.Host.ID = "mac"
	m.sessions = []rex.SessionInfo{{ID: "target", Title: "任务", Cols: 112, Rows: 42}}
	m.recentSessions = []mobileRecentSession{{Desktop: "mac", Session: "target", Title: "任务"}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).FillWidth().Grow(1).Children(func() { m.connectView(c) }) }, 390, 750)
	row, ok := tt.Find("进入最近会话 mac target")
	desktopRow, desktopOK := tt.Find("打开桌面 我的 Mac")
	if !ok || !desktopOK || row.Y < desktopRow.Y+desktopRow.H || row.H < 44 || row.X+row.W > 390 {
		t.Fatal("recent session row is missing, misplaced or too small", row)
	}
	saveSettingsImage(t, tt, "mobile-recent-sessions-light")
	tt.SetDark(true)
	tt.Frame()
	saveSettingsImage(t, tt, "mobile-recent-sessions-dark")
	tt.Click("进入最近会话 mac target")
	if m.term == nil || m.selected.ID != "target" || m.client != client || len(m.recentSessions) != 1 {
		t.Fatal("one tap did not open the existing session")
	}
	// Wait for the lazy attachment to our isolated transport.
	var request rex.Attach
	select {
	case request = <-attached:
	case <-time.After(2 * time.Second):
		t.Fatal("session shortcut never attached")
	}
	if request.SID != "target" || request.Cols != 0 || request.Rows != 0 || !request.ScreenFrames {
		t.Fatal("shortcut changed the desktop PTY size or opened the wrong session", request)
	}
}
