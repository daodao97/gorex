package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gorex/internal/remote"
	"gorex/internal/rex"
	"gorex/internal/terminal"
)

func TestMobileFailureMessagesOfferUsefulRecoveryWithoutRawErrors(t *testing.T) {
	for _, kind := range []remote.FailureKind{remote.DesktopUnavailable, remote.InvalidLink, remote.NetworkUnavailable, remote.RelayUnavailable, remote.ConnectionTimeout, remote.ProtocolMismatch} {
		issue := connectionIssueFor(&remote.ConnectionError{Kind: kind, Cause: errors.New("secret-link/raw tunnel error")})
		if strings.Contains(issue.title+issue.body, "secret") || issue.title == "" || issue.body == "" {
			t.Fatal("unhelpful or unsafe feedback", issue)
		}
		if kind == remote.ConnectionTimeout && strings.Contains(issue.body, "已失效") {
			t.Fatal("timeout claimed capability expired")
		}
		if (kind == remote.InvalidLink || kind == remote.ProtocolMismatch) && issue.automatic {
			t.Fatal("permanent issue will retry forever")
		}
	}
	m := &mobileApp{link: "fixture", reconnecting: true, busy: true}
	m.connectionFailed(&remote.ConnectionError{Kind: remote.ProtocolMismatch}, true)
	if m.busy || m.retryTimer != nil || !m.reconnecting {
		t.Fatal("incompatible version kept automatic retrying")
	}
	m.enterBackground()
	m.enterForeground()
	if m.busy || m.retryTimer != nil {
		t.Fatal("foreground retried incompatible desktop")
	}
}

func TestMobileRecoveryDoesNotResizeTerminalAndActionsFit(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{320, 568}, {375, 750}, {390, 750}, {750, 390}} {
		term, err := terminal.New(terminal.Options{Conn: nopConn{}, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm})
		if err != nil {
			t.Fatal(err)
		}
		m := &mobileApp{term: term, selected: rex.SessionInfo{ID: "fixture", Title: "修复中文输入与终端阅读"}, link: "fixture"}
		tt := ui.NewTester(m.view, size[0], size[1])
		before, ok := tt.Find("Terminal")
		if !ok {
			t.Fatal("terminal missing")
		}
		m.pauseConnection()
		m.connectionIssue = connectionIssueFor(&remote.ConnectionError{Kind: remote.ConnectionTimeout})
		tt.Frame()
		after, ok := tt.Find("Terminal")
		if !ok || before != after {
			t.Fatalf("recovery changed viewport at %v: %v -> %v", size, before, after)
		}
		tt.Click("连接恢复操作")
		tt.Frame()
		for _, label := range []string{"重试连接", "重新扫码连接", "取消重连", "关闭连接提示"} {
			r, ok := tt.Find(label)
			if !ok || r.W < 44 || r.H < 44 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
				t.Fatalf("action %s does not fit %v: %v", label, size, r)
			}
		}
		if size[0] == 390 {
			saveSettingsImage(t, tt, "mobile-recovery-dialog")
			tt.Click("关闭连接提示")
			tt.Frame()
			saveSettingsImage(t, tt, "mobile-recovery-reading")
		}
		tt.Click("关闭连接提示")
		tt.Frame()
		if m.term != term || !m.reconnecting {
			t.Fatal("closing details ended recovery")
		}
		m.disconnect(false)
	}
}

func TestMobileInitialFailureActionsStayAboveRecentHistory(t *testing.T) {
	registerFonts()
	for _, kind := range []remote.FailureKind{remote.ConnectionTimeout, remote.InvalidLink} {
		m := &mobileApp{link: "fixture", connectionIssue: connectionIssueFor(&remote.ConnectionError{Kind: kind}), history: []desktopRecent{{Name: "Mac"}}}
		tt := ui.NewTester(m.view, 375, 750)
		scan, _ := tt.Find("扫码连接桌面")
		action, ok := tt.Find("重新扫码连接")
		recent, _ := tt.Find("重新连接 Mac")
		if !ok || action.Y < scan.Y || action.Y+action.H > recent.Y || action.H < 44 {
			t.Fatal("recovery actions buried in history", action, recent)
		}
		_, retry := tt.Find("重试连接")
		if retry != (kind != remote.InvalidLink) {
			t.Fatal("invalid capability offered useless retry")
		}
		saveSettingsImage(t, tt, "mobile-connection-failure")
	}
}
