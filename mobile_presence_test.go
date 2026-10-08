package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
