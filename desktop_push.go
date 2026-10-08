package main

import (
	"fmt"
	"github.com/egoist/mygo"
	"gorex/internal/push"
	"gorex/internal/rex"
	"os"
	"time"
)

// Presence goes straight to the push worker, so legacy session servers can keep
// their live PTYs. Focus changes are immediate; heartbeats recover worker restarts.
func startDesktopPushPresence(win *mygo.Window) {
	changes := make(chan bool, 1)
	done := make(chan struct{})
	update := func(active bool) {
		select {
		case <-changes:
		default:
		}
		changes <- active
	}
	win.OnFocus(func() { update(!win.IsMinimized()) })
	win.OnBlur(func() { update(false) })
	win.OnClosed(func() { close(done) })
	activity := push.DesktopActivity{ID: fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()), Active: win.IsFocused() && !win.IsMinimized()}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		report := func() { activity.Sequence++; _ = push.ReportDesktop(rex.Dir(), activity) }
		report()
		for {
			select {
			case <-done:
				activity.Active = false
				report()
				return
			case active := <-changes:
				activity.Active = active
				report()
			case <-ticker.C:
				report()
			}
		}
	}()
}
