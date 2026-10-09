package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"retty/internal/agents"
	"retty/internal/remote"
	"retty/internal/rex"
	"retty/internal/terminal"
)

func TestMobileSessionRecoverySharesHomeUIAndKeepsCachedList(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{320, 568}, {390, 750}, {750, 390}} {
		m := &mobileApp{link: "fixture", reconnecting: true, sessions: []rex.SessionInfo{
			{ID: "agent", Title: "修复 CLI 配色问题 | retty", Dir: "/work/github/quickgui/retty", Program: "codex", Agent: rex.AgentState{ID: "codex", State: agents.Running}},
			{ID: "shell", Title: "zsh", Dir: "/work/github/quickgui/retty", Program: "zsh"},
		}}
		m.hello.Host.Name, m.hello.Host.Home = "MacBook Pro (2)", "/work"
		tt := ui.NewTester(m.view, size[0], size[1])
		status, ok := tt.Find("连接状态")
		row, rowOK := tt.Find("打开会话 agent")
		if !ok || !rowOK || status.X != row.X || status.W != row.W || row.Y < status.Y+status.H+11.99 || row.H < 63.99 || row.H > 64.01 {
			t.Fatalf("recovery does not align with the session list at %v: status %v, row %v", size, status, row)
		}
		home := &mobileApp{home: true, link: "fixture", reconnecting: true}
		homeUI := ui.NewTester(home.view, size[0], size[1])
		homeStatus, homeOK := homeUI.Find("连接状态")
		if !homeOK || homeStatus.W < status.W-.01 || homeStatus.W > status.W+.01 || homeStatus.H < status.H-.01 || homeStatus.H > status.H+.01 {
			t.Fatalf("home and session recovery cards differ at %v: home %v, session %v", size, homeStatus, status)
		}
		for _, label := range []string{"重试连接", "重新扫码连接", "取消重连"} {
			r, ok := tt.Find(label)
			if !ok || r.W < 44 || r.H < 44 || r.X < status.X || r.X+r.W > status.X+status.W || r.Y+r.H > status.Y+status.H {
				t.Fatalf("shared recovery action %s is clipped at %v: %v", label, size, r)
			}
		}
		for _, label := range []string{"执行中", "桌面已连接"} {
			if _, ok := tt.Find(label); ok {
				t.Fatalf("recovery list shows distracting or stale state: %s", label)
			}
		}
		tt.Click("新建会话")
		if m.creating {
			t.Fatal("recovery allowed creating a session")
		}
		if size[0] == 390 {
			saveSettingsImage(t, tt, "mobile-sessions-reconnecting")
			tt.SetDark(true)
			saveSettingsImage(t, tt, "mobile-sessions-reconnecting-dark")
			tt.SetDark(false)
		}
		// Cached rows remain readable and explain why they cannot open yet.
		tt.Click("打开会话 agent")
		tt.Frame()
		if !m.connectionDetailsOpen || m.term != nil || m.navigation.Path() != "/sessions" {
			t.Fatal("cached row opened a stale session instead of connection details")
		}
		for _, label := range []string{"重试连接", "重新扫码连接", "取消重连", "关闭连接提示"} {
			r, ok := tt.Find(label)
			if !ok || r.W < 43.99 || r.H < 43.99 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
				t.Fatalf("recovery action %s does not fit %v: %v", label, size, r)
			}
		}
		tt.Click("关闭连接提示")
		tt.Frame()
		if !m.reconnecting {
			t.Fatal("closing details interrupted recovery")
		}
		m.connectionIssue = connectionIssueFor(&remote.ConnectionError{Kind: remote.ProtocolMismatch})
		tt.Frame()
		if !tt.HasText("桌面版本不兼容") || tt.HasText("正在自动重试") || tt.HasText("正在重连") {
			t.Fatal("permanent failure looks like automatic recovery")
		}
		if size[0] == 390 {
			saveSettingsImage(t, tt, "mobile-sessions-recovery-blocked")
		}
		m.connectionIssue, m.reconnecting, m.client = nil, false, &rex.Client{}
		tt.Frame()
		if _, ok := tt.Find("连接状态"); ok || !tt.HasText("桌面已连接") || !tt.HasText("执行中") {
			t.Fatal("restored connection kept stale recovery feedback")
		}
	}
}

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
