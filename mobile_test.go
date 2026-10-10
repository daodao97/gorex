package main

import (
	"net"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/tailscale/tailcat"
	"retty/internal/remote"
	"retty/internal/rex"
	"retty/internal/terminal"
)

func TestMobileNavigationBackReleasesOnlyCurrentPage(t *testing.T) {
	registerFonts()
	conn, peer := net.Pipe()
	defer peer.Close()
	client := rex.NewClient(conn, nil)
	defer client.Close()
	m := &mobileApp{client: client, sessions: []rex.SessionInfo{{ID: "fixture"}}, history: []desktopRecent{{Name: "Mac"}}}
	tt := ui.NewTester(m.view, 390, 750)
	term, err := terminal.New(terminal.Options{Conn: nopConn{}, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	m.term, m.selected = term, m.sessions[0]
	tt.Frame()
	m.navigation.Back()
	tt.Frame()
	if m.term != nil || m.selected.ID != "" || m.client != client || len(m.sessions) != 1 {
		t.Fatal("terminal Back did not detach while preserving desktop sessions")
	}
	if m.navigation.Path() != "/sessions" || m.navigation.CanGoForward() {
		t.Fatal("terminal Back did not return to the list or retained a closed terminal in history")
	}
	m.navigation.Back()
	tt.Frame()
	if m.client != client || !m.home || m.navigation.CanGoBack() || len(m.history) != 1 {
		t.Fatal("sessions Back did not keep the connection on the home page")
	}
}

func TestHistoryKeepsNewestLinkPerDesktop(t *testing.T) {
	link := func() string {
		key := tailcat.NewPrivateKey()
		key.Public.RegionID = 301
		return remote.Link(key.Public.Addr())
	}
	latest, older, sameNameOther, legacy := link(), link(), link(), link()
	got := mergeDesktopHistory([]desktopRecent{{ID: "desktop-a", Name: "Mac", Link: latest}}, []desktopRecent{
		{ID: "desktop-a", Name: "Old name", Link: older},
		{ID: "desktop-b", Name: "Mac", Link: sameNameOther},
		{Name: "Mac", Link: legacy},
		{Name: "invalid", Link: "not a connection"},
	})
	if len(got) != 2 || got[0].Link != latest || got[1].Link != sameNameOther {
		t.Fatalf("rotated/legacy connections were not replaced: %+v", got)
	}
	got = mergeDesktopHistory([]desktopRecent{{Name: "Mac", Link: latest}}, []desktopRecent{{ID: "desktop-a", Name: "Mac", Link: older}})
	if len(got) != 1 || got[0].ID != "desktop-a" || got[0].Link != latest {
		t.Fatal("legacy migration lost newest URL or desktop identity")
	}
	got = mergeDesktopHistory([]desktopRecent{{ID: "machine:new", Name: "Mac", Link: latest}}, []desktopRecent{{ID: "early-installation-id", Name: "Mac", Link: older}})
	if len(got) != 1 || got[0].ID != "machine:new" {
		t.Fatal("early installation records duplicated the same device")
	}
	got = mergeDesktopHistory([]desktopRecent{{ID: "legacy:active", Name: "Mac", Link: latest}}, []desktopRecent{{ID: "machine:old", Name: "Mac", Link: older}})
	if len(got) != 1 || got[0].ID != "legacy:active" || got[0].Link != latest {
		t.Fatal("legacy notifications cannot reconnect through the newest history entry")
	}
}

func TestMobileHistoryReplacesConnectionCodeInput(t *testing.T) {
	registerFonts()
	key := tailcat.NewPrivateKey()
	key.Public.RegionID = 301
	m := &mobileApp{history: []desktopRecent{{Name: "我的 Mac", Link: remote.Link(key.Public.Addr())}}}
	tt := ui.NewTester(m.view, 390, 750)
	if r, ok := tt.Find("重新连接 我的 Mac"); !ok || r.H < 44 || r.Y < 200 {
		t.Fatal("desktop history missing below scan button")
	}
	if _, ok := tt.Find("桌面连接码"); ok {
		t.Fatal("manual connection-code module remains")
	}
	saveSettingsImage(t, tt, "mobile-history-light")
	tt.SetDark(true)
	tt.Frame()
	saveSettingsImage(t, tt, "mobile-history-dark")
}

func TestMobileScreensFitPhoneAndKeyboard(t *testing.T) {
	registerFonts()
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]float32{{390, 750}, {375, 620}, {750, 310}} {
			m := &mobileApp{}
			tt := ui.NewTester(m.view, int(size[0]), int(size[1]))
			tt.SetDark(dark)
			tt.Frame()
			if r, ok := tt.Find("扫码连接桌面"); !ok || r.W < 44 || r.H < 44 || r.X < 0 || r.X+r.W > size[0]+1 {
				t.Fatalf("unusable scan target on %v", size)
			}
			if size[0] == 390 {
				saveSettingsImage(t, tt, "mobile-connect-"+map[bool]string{false: "light", true: "dark"}[dark])
			}
			m.client = &rex.Client{}
			m.hello.Host.Name = "MacBook Pro (2)"
			m.hello.Host.Home = "/Users/fixture"
			m.sessions = []rex.SessionInfo{
				{ID: "fixture", Title: "安装当前修改到 iOS 真机 | Retty 终端与会话同步", Program: "codex", Dir: "/Users/fixture/work/github/quickgui/retty", Cols: 80, Rows: 24, Agent: rex.AgentState{ID: "codex", State: "running"}},
				{ID: "workers", Title: "确认线上 50 个 worker 生效", Program: "codex", Dir: "/Users/fixture/work/github/gpt-pay", Cols: 80, Rows: 24, Agent: rex.AgentState{ID: "codex", State: "waiting"}},
				{ID: "claude", Title: "Claude Code", Program: "claude", Dir: "/Users/fixture/work/github/quickgui/retty", Cols: 80, Rows: 24, Agent: rex.AgentState{ID: "claude", State: "completed"}},
			}
			tt.Frame()
			if r, ok := tt.Find("打开会话 fixture"); !ok || r.H < 44 || r.X+r.W > size[0]+1 {
				t.Fatalf("unusable session row on %v", size)
			}
			if size[0] == 390 {
				saveSettingsImage(t, tt, "mobile-sessions-"+map[bool]string{false: "light", true: "dark"}[dark])
			}
			if r, ok := tt.Find("新建会话"); !ok || r.W < 44 || r.H < 44 || r.X+r.W > size[0]+1 {
				t.Fatalf("unusable create target on %v", size)
			}
			term, err := terminal.New(terminal.Options{Conn: nopConn{}, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm, DarkTheme: darkTerm, AdaptiveColors: true})
			if err != nil {
				t.Fatal(err)
			}
			term.Feed([]byte("\x1b[?25l\x1b[HRetty on iPhone\r\n$ "))
			m.term, m.selected = term, m.sessions[0]
			tt.Frame()
			r, ok := tt.Find("Terminal")
			if !ok || r.W < size[0]-10 || r.H < 80 {
				t.Fatalf("terminal did not fill phone viewport on %v: %+v", size, r)
			}
			if size[0] == 390 {
				saveSettingsImage(t, tt, "mobile-terminal-"+map[bool]string{false: "light", true: "dark"}[dark])
			}
			// Back must detach without drawing a closed terminal in the same frame.
			back, ok := tt.Find("返回")
			if !ok || back.W < 44 || back.H < 44 {
				t.Fatal("compact header shrank the back touch target")
			}
			// The part of the touch target behind the title still goes back.
			tt.ClickAt(back.X+back.W-2, back.Y+back.H/2)
			tt.Frame()
			if m.term != nil {
				t.Fatal("Back retained the terminal")
			}
		}
	}
}

func TestMobileKeyboardActionsStayVisibleAndKeepFocus(t *testing.T) {
	registerFonts()
	for _, width := range []int{375, 390, 750} {
		term, err := terminal.New(terminal.Options{Conn: nopConn{}, Font: terminal.Font{Family: termFont.Family, Size: 13}, Theme: lightTerm, DarkTheme: darkTerm})
		if err != nil {
			t.Fatal(err)
		}
		m := &mobileApp{client: &rex.Client{}, term: term}
		tt := ui.NewTester(m.view, width, 620)
		if _, ok := tt.Find("Esc"); ok {
			t.Fatal("reading mode retained a command toolbar")
		}
		tt.Click("Terminal")
		for _, action := range mobileKeyboardActions {
			r, ok := tt.Find(action.Label)
			if !ok || r.W < 44 || r.H < 44 || r.X < 0 || r.X+r.W > float32(width)+1 {
				t.Fatalf("unreachable accessory action %q at width %d: %+v", action.Label, width, r)
			}
		}
		tt.Compose("你好", 2)
		tt.Click("更多")
		if !tt.Focused("Terminal") {
			t.Fatal("secondary keys dismissed terminal input")
		}
		for _, label := range []string{"Shift", "Tab", "←", "→", "↑", "↓", "换行", "粘贴"} {
			if r, ok := tt.Find(label); !ok || r.W < 44 || r.H < 44 || r.X+r.W > float32(width)+1 {
				t.Fatalf("secondary key hidden: %s", label)
			}
		}
		shift, _ := tt.Find("Shift")
		paste, _ := tt.Find("粘贴")
		escape, _ := tt.Find("Esc")
		if shift.Y != paste.Y || escape.Y-shift.Y != 44 {
			t.Fatalf("expanded accessory must use two rows at width %d: Shift=%+v Paste=%+v Esc=%+v", width, shift, paste, escape)
		}
		if width == 390 {
			saveSettingsImage(t, tt, "mobile-keyboard-expanded")
		}
		tt.Click("更多")
		if _, ok := tt.Find("粘贴"); ok {
			t.Fatal("secondary panel did not collapse")
		}
		tt.Click("收起")
		if tt.Focused("Terminal") {
			t.Fatal("reading mode retained terminal input focus")
		}
		if _, ok := tt.Find("Esc"); ok {
			t.Fatal("reading mode retained the keyboard accessory")
		}
		tt.Click("Terminal")
		if !tt.Focused("Terminal") {
			t.Fatal("keyboard entry did not resume terminal input")
		}
		term.Close()
	}
}
