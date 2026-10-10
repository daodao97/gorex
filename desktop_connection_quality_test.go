package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/remote"
)

func TestDesktopQualityPopoverIsIndependentOfHostNavigation(t *testing.T) {
	a, tt := newStaticTestApp(t)
	q := &remote.Quality{}
	q.RecordRequest(time.Now(), 40*time.Millisecond, nil)
	q.RecordRequest(time.Now(), 120*time.Millisecond, nil)
	q.RecordRequest(time.Now(), 10*time.Second, context.DeadlineExceeded)
	q.RecoveryStarted(time.Now().Add(-time.Second))
	q.RecoveryFinished(time.Now())
	h := &desktopHost{key: "metrics", quality: q, recent: desktopRecent{Name: "Mac mini", OS: "macOS"}}
	a.desktops.hosts = []*desktopHost{h}
	before := a.tab().Focus.bounds
	if err := tt.Click("连接"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("连接指标 Mac mini"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.desktops.selected != nil || a.desktops.qualityHost != h || h.busy || h.client != nil {
		t.Fatal("info click navigated or connected to host")
	}
	for _, text := range []string{"最近 5 分钟", "P50 80 ms · P95 120 ms", "1 / 3 次", "1 次 · 1.0 s", "当前未连接 · 保留最近记录"} {
		if !slices.Contains(tt.Texts(), text) {
			t.Fatal("missing measured value", text, tt.Texts())
		}
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		saveDesktopImage(t, tt, map[bool]string{false: "desktop-quality-light.png", true: "desktop-quality-dark.png"}[dark])
	}
	tt.SetSize(560, 340)
	tt.Frame()
	panel, ok := tt.Find("连接指标弹层 Mac mini")
	if !ok || panel.X < 0 || panel.Y < 0 || panel.X+panel.W > 560 || panel.Y+panel.H > 340 {
		t.Fatal("popover clipped", panel)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.desktops.qualityHost != nil || !a.desktops.open {
		t.Fatal("Escape closed drawer instead of popover")
	}
	tt.SetSize(1000, 620)
	tt.Frame()
	if a.tab().Focus.bounds != before {
		t.Fatal("popover resized terminal")
	}
	if err := tt.Click("连接指标 Mac mini"); err != nil {
		t.Fatal(err)
	}
	tt.ClickAt(100, 100)
	tt.Frame()
	if a.desktops.qualityHost != nil || a.desktops.selected != nil {
		t.Fatal("outside click failed to dismiss")
	}
}
