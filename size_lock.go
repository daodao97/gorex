package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"retty/internal/rex"
)

// followSizeLock shows a session whose size a phone holds at the phone's
// size: one PTY has one size, and the program draws for the phone. The
// pane follows that size until the phone leaves or a window unlocks it.
func (a *App) followSizeLock(p *Pane, was rex.SessionInfo) {
	in := p.info
	if p.host != nil {
		return
	} // Remote viewers follow framed source dimensions.
	// A list made before an unlock completed still names the lock.
	if p.term == nil || p.unlocking || time.Since(p.unlockedAt) < 2*time.Second {
		return
	}
	switch {
	case in.SizeLock != "" && (was.SizeLock != in.SizeLock || was.Cols != in.Cols || was.Rows != in.Rows || !p.locked):
		first := !p.locked
		p.locked = true
		p.term.SetFixedSize(in.Cols, in.Rows)
		if first {
			// What the phone's program drew reached this pane before the
			// list did. Replace it with the session's screen at its size.
			sid, client := p.SID, a.paneClient(p)
			go client.Resync(sid)
		}
	case in.SizeLock == "" && p.locked:
		// The phone became inactive, or another window released it.
		p.locked = false
		if p.stream != nil {
			p.stream.ResendSize()
		}
		p.term.SetFixedSize(0, 0)
	}
}

// unlockSize gives the session's size back to the desktop: the pane sizes
// it to its view again, and the phone reflows the desktop's screen.
func (a *App) unlockSize(p *Pane) {
	if !p.locked || p.unlocking {
		return
	}
	p.unlocking = true
	sid, client := p.SID, a.paneClient(p)
	go func() {
		err := client.UnlockSize(sid, 0, 0)
		a.post(func() {
			p.unlocking = false
			if err != nil {
				a.err = err.Error()
				return
			}
			p.locked, p.unlockedAt = false, time.Now()
			p.info.SizeLock, p.info.SizeLockDevice = "", ""
			// The view's next frame resizes the session to the pane.
			if p.stream != nil {
				p.stream.ResendSize()
			}
			p.term.SetFixedSize(0, 0)
		})
		if a.win != nil {
			a.win.Invalidate()
		}
	}()
}

// sizeLockBar tells that a phone holds the pane's size, with the unlock.
func (a *App) sizeLockBar(c *ui.Context, k *colors, p *Pane) {
	device := p.info.SizeLockDevice
	if device == "" {
		device = "iPhone"
	}
	ui.Row(c.Key("size-lock")).Height(32).Padding(0, 8, 0, 12).Gap(8).
		AlignItems(ui.Center).Background(k.panel).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder).
		Children(func() {
			ui.Text(c, fmt.Sprintf("%s 正在使用此会话（%d×%d），窗格尺寸已锁定", device, p.info.Cols, p.info.Rows)).
				FontSize(12).TextColor(k.textMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if ui.Button(c, "解锁").Disabled(p.unlocking).Label("Unlock Size").Clicked() {
				a.unlockSize(p)
			}
		})
}
