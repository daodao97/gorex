package main

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/tailscale/tailcat"
	"retty/internal/remote"
	"retty/internal/rex"
)

func TestMobileDeviceAliasSurvivesReconnectAndReset(t *testing.T) {
	link := func() string {
		key := tailcat.NewPrivateKey()
		key.Public.RegionID = 301
		return remote.Link(key.Public.Addr())
	}
	old, rotated, other := link(), link(), link()
	m := &mobileApp{link: old, history: []desktopRecent{
		{ID: "one", Name: "Mac", Link: old},
		{ID: "two", Name: "Mac", Link: other, Alias: "另一台电脑"},
	}}
	m.hello.Host.ID, m.hello.Host.Name = "one", "Mac"
	m.setDeviceName(" 家里的电脑\n ")
	if m.connectedDevice().displayName() != "家里的电脑" || m.history[1].Alias != "另一台电脑" || m.history[0].Name != "Mac" {
		t.Fatal("alias changed the source identity or another device")
	}
	// A new capability and host name must retain the alias by stable identity.
	m.history = mergeDesktopHistory([]desktopRecent{{ID: "one", Name: "New Mac", Link: rotated}}, m.history)
	saved, err := json.Marshal(m.history)
	if err != nil {
		t.Fatal(err)
	}
	var restored []desktopRecent
	if err := json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	m.history, m.link, m.hello.Host.Name = restored, rotated, "New Mac"
	if len(m.history) != 2 || m.connectedDevice().displayName() != "家里的电脑" || m.history[0].Link != rotated {
		t.Fatal("reconnect or storage round trip lost the alias")
	}
	m.setDeviceName("")
	// An older queued Keychain load must not resurrect a cleared alias, and
	// unrelated desktops must still load normally.
	m.applyLoadedConnectionHistory(append(restored[:1:1], desktopRecent{ID: "three", Name: "Server", Link: link(), Alias: "服务器"}), nil, m.historyEpoch)
	if m.connectedDevice().displayName() != "New Mac" || len(m.history) != 3 || m.history[2].Alias != "服务器" {
		t.Fatal("delayed storage read restored a cleared alias or lost another device")
	}
}

func TestMobileDeviceRenameSettingsFlow(t *testing.T) {
	registerFonts()
	key := tailcat.NewPrivateKey()
	key.Public.RegionID = 301
	for _, size := range [][2]int{{320, 568}, {390, 750}, {750, 390}} {
		m := &mobileApp{client: &rex.Client{}, link: remote.Link(key.Public.Addr()), sizeLock: true,
			sessions:       []rex.SessionInfo{{ID: "shell", Title: "zsh"}},
			recentSessions: []mobileRecentSession{{Desktop: "one", Session: "shell", Title: "zsh"}},
		}
		m.hello.Host.ID, m.hello.Host.Name, m.hello.Version = "one", "Mac", 6
		m.history = []desktopRecent{{ID: "one", Name: "Mac", Link: m.link}}
		tt := ui.NewTester(m.view, size[0], size[1])
		tt.SetDark(true)
		tt.Click("会话列表设置")
		if tt.Focused("连接设备名称") {
			t.Fatal("settings summoned the keyboard")
		}
		if size[0] == 390 {
			saveSettingsImage(t, tt, "mobile-device-settings-dark")
		}
		tt.Click("重命名连接设备")
		if tt.Focused("连接设备名称") {
			t.Fatal("rename sheet summoned the keyboard before tapping the field")
		}
		for _, label := range []string{"连接设备名称", "取消设备重命名", "保存设备名称"} {
			r, ok := tt.Find(label)
			if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0])+.01 || r.Y+r.H > float32(size[1])+.01 {
				t.Fatalf("rename action %s clipped at %v: %v", label, size, r)
			}
		}
		tt.Click("连接设备名称")
		tt.Type("我的电脑")
		if size[0] == 390 {
			tt.SetSize(390, 360)
			saveSettingsImage(t, tt, "mobile-device-name-keyboard-dark")
		}
		tt.Click("保存设备名称")
		tt.SetSize(size[0], size[1])
		if m.deviceNameOpen || !m.sessionSettingsOpen || m.connectedDevice().Alias != "我的电脑" || tt.Focused("连接设备名称") {
			t.Fatal("saving did not apply the alias, close input and return to settings")
		}
		tt.Click("重命名连接设备")
		m.deviceNameDraft = "不保存"
		tt.Click("取消设备重命名")
		if m.connectedDevice().Alias != "我的电脑" {
			t.Fatal("cancel changed the saved alias")
		}
		tt.Click("关闭会话列表设置")
		if !tt.HasText("我的电脑") {
			t.Fatal("session header kept the original device name")
		}
		m.goHome()
		tt.Frame()
		if _, ok := tt.Find("打开桌面 我的电脑"); !ok || !tt.HasText("我的电脑") {
			t.Fatal("home connection and recent session displays did not use the alias")
		}
	}
}
