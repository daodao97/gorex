package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"retty/internal/terminal"
)

const (
	compactTitleH = 32
	compactGap    = 1.0 // visible separator; its pointer target is wider
	compactHit    = 4
	activeFor     = 1500 * time.Millisecond
)

func (a *App) view(c *ui.Context) {
	a.services = c.Services()
	a.runPosted()
	k := colorsOf(c)
	a.focusedWin = a.win == nil || a.win.IsFocused()
	a.syncRemotePaneActivity()
	a.closeViewedPaneNotice()
	c.Root().Background(terminalBackground(c))
	ui.Column(c).Fill().Children(func() {
		a.compactTitleBar(c, k, c.TitleBar())
		ui.Column(c).Grow(1).MinHeight(0).Children(func() {
			if t := a.tab(); t != nil {
				a.tabContent(c, k, t)
			}
		})
	})
	if a.err != "" {
		a.errorBanner(c, k)
	}
	a.shortcuts(c)
	a.palette(c, k)
	a.settingsPage(c, k)
	a.desktopConnectionDialog(c, k)
	if a.saveDue && time.Since(a.lastSave) > time.Second {
		a.save()
	}
	// The window's title, which the Window menu and Mission Control show,
	// is the active tab's.
	if t := a.tab(); t != nil && a.win != nil {
		name, detail := t.label()
		if title := strings.TrimSpace(name + " " + detail); title != a.title {
			a.title = title
			a.win.SetTitle(title)
		}
	}
}

// iconButton is a borderless button of an icon, with a face on hover;
// label names it for screen readers and its tooltip.
func iconButton(c *ui.Context, k *colors, name, label string, size, iconSize float32) ui.Element {
	b := ui.Box(c).Size(size, size).Center().Radius(size / 2.6).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label)
	if b.Pressed() {
		b.Background(k.pressed)
	} else if b.Hovered() {
		b.Background(k.hover)
	}
	b.Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})
	col := k.iconMuted
	if b.Hovered() {
		col = k.text
	}
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(iconSize, iconSize).TextColor(col)
	})
	return b
}

// tabContent lays out the panes of a tab, or the one zoomed.
func (a *App) tabContent(c *ui.Context, k *colors, t *Tab) {
	if t.Zoom != nil {
		a.paneCard(c, k, t, t.Zoom).Grow(1)
		return
	}
	content := a.node(c, k, t, t.Root).Grow(1)
	if t.Root.Pane == nil {
		// Paint after every pane has recorded its current bounds, outside
		// the cards' clips, so the outline reaches the divider centerlines.
		content.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if t.Focus != nil {
				paintCompactFocus(p, r, t.Focus.bounds, k.cardBorderFocused)
			}
		})
	}
}

// node lays out a node of the tree of splits.
func (a *App) node(c *ui.Context, k *colors, t *Tab, n *Node) ui.Element {
	if n.Pane != nil {
		return a.paneCard(c, k, t, n.Pane)
	}
	var box ui.Element
	if n.Vertical {
		box = ui.Column(c.Key(n.ID))
	} else {
		box = ui.Row(c.Key(n.ID))
	}
	box.MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	bounds := box.Bounds()
	// The tree may change as its panes build, as a split: build the
	// children it has now.
	first, second, ratio := n.A, n.B, n.Ratio
	box.Children(func() {
		a.node(c, k, t, first).Grow(ratio).Basis(0).MinWidth(0).MinHeight(0)
		space := ui.Box(c.Key("divider-space")).Shrink(0)
		if n.Vertical {
			space.Height(compactGap)
		} else {
			space.Width(compactGap)
		}
		div := ui.Box(c.Key("divider")).Role(ui.RoleSplitter).Label("Divider")
		// Only one point takes layout space. The absolute pointer
		// target spans both panes and stays centered on that line.
		offset := compactGap*(0.5-ratio) - compactHit/2
		if n.Vertical {
			div.Absolute().Height(compactHit).Left(0).Right(0).
				TopPercent(100 * ratio).MarginY(offset).Cursor(ui.CursorResizeRow)
		} else {
			div.Absolute().Width(compactHit).Top(0).Bottom(0).
				LeftPercent(100 * ratio).MarginX(offset).Cursor(ui.CursorResizeColumn)
		}
		if dx, dy, ok := div.Dragged(); ok {
			total := bounds.W - compactGap
			d := dx
			if n.Vertical {
				total, d = bounds.H-compactGap, dy
			}
			if total > 0 {
				n.Ratio = min(max(n.Ratio+d/total, 0.08), 0.92)
				a.changed()
			}
		}
		if div.DoubleClicked() {
			n.Ratio = 0.5
			a.changed()
		}
		showGrip := div.Hovered() || div.Dragging() || div.Pressed()
		width, height := c.Size()
		div.Draw(func(p *ui.Painter, r ui.Rect) {
			// Reach the center of adjoining gaps at nested split
			// junctions, without drawing into the title bar.
			if n.Vertical {
				x0, x1 := max(0, r.X-compactGap/2), min(width, r.X+r.W+compactGap/2)
				p.Fill(ui.Rect{X: x0, Y: r.Y + r.H/2 - 0.5, W: x1 - x0, H: 1}, k.headerBorder, 0)
			} else {
				y0, y1 := max(compactTitleH, r.Y-compactGap/2), min(height, r.Y+r.H+compactGap/2)
				p.Fill(ui.Rect{X: r.X + r.W/2 - 0.5, Y: y0, W: 1, H: y1 - y0}, k.headerBorder, 0)
			}
			if showGrip {
				if n.Vertical {
					p.Fill(ui.Rect{X: r.X + r.W/2 - 18, Y: r.Y + r.H/2 - 1.5, W: 36, H: 3}, k.iconMuted.Alpha(0.6), 1.5)
				} else {
					p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, k.iconMuted.Alpha(0.6), 1.5)
				}
			}
		})
		a.node(c, k, t, second).Grow(1 - ratio).Basis(0).MinWidth(0).MinHeight(0)
	})
	return box
}

// paneCard draws a pane with its optional search field and terminal.
func (a *App) paneCard(c *ui.Context, k *colors, t *Tab, p *Pane) ui.Element {
	card := ui.Column(c.Key(p.ID)).Clip().MinWidth(0).MinHeight(0).Background(terminalBackground(c))
	card.Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
	card.Children(func() {
		if p.find.open {
			a.findBar(c, k, t, p)
		}
		if p.locked {
			a.sizeLockBar(c, k, p)
		}
		body := ui.Box(c.Key("terminal-body")).Grow(1).MinHeight(0)
		body.Children(func() {
			if p.term == nil {
				return
			}
			p.term.SetSelectOnDrag(paneProgram(p).Agent)
			tv := terminal.View(c, p.term).Fill()
			if p.find.open && tv.Shortcut(0, ui.KeyEscape) {
				a.closeFind(p)
			}
			if a.focusReq == p && a.tab() == t {
				tv.Focus()
				a.focusReq = nil
			}
			if tv.Focused() && t.Focus != p {
				t.setFocus(p)
				a.changed()
			}
			if tv.Pressed() && p.host != nil {
				a.activateRemotePane(p)
			}
			if tv.Focused() {
				p.attention = false
			}
		})
		if p.host != nil && (!p.host.connected() || p.streamEnded) {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(32).Children(func() { a.desktopOfflineBar(c, k, p.host) })
		} else if p.host != nil && p.remoteView != nil && !p.remoteView.inputReady() {
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(28).Padding(0, 8, 0, 12).AlignItems(ui.Center).Gap(8).Background(k.panel).Children(func() {
				message := "点击窗格接入会话"
				if p.remoteYielded {
					message = "此会话已在其他窗口打开"
				} else if p.remoteView.hasTransport() {
					message = "正在载入会话…"
				}
				ui.Text(c, message).FontSize(12).TextColor(k.textMuted).Grow(1)
				if !p.remoteView.hasTransport() && iconButton(c, k, "rotate-ccw", "接入当前窗格", 24, 13).Clicked() {
					t.setFocus(p)
					a.focusReq = p
					a.activateRemotePane(p)
				}
			})
		}
	})
	// Bounds during view building are from the previous layout. Record
	// this frame's rectangle so focus navigation works just after unzoom.
	card.Draw(func(_ *ui.Painter, r ui.Rect) {
		p.bounds = r
	})
	if card.Pressed() && t.Focus != p {
		t.setFocus(p)
		a.focusReq = p
	}
	a.syncRemotePaneActivity()
	return card
}

// status is what a pane is doing.
type status int

const (
	statusIdle      status = iota // a shell at its prompt
	statusRunning                 // a program printing
	statusQuiet                   // a program running quietly
	statusAttention               // done or ringing out of sight
)

func (a *App) statusOf(c *ui.Context, p *Pane) status {
	if s := paneAgentState(p); s.State != "" {
		switch s.State {
		case "running":
			return statusRunning
		case "waiting", "failed":
			return statusAttention
		default:
			return statusQuiet
		}
	}
	if p.attention {
		return statusAttention
	}
	if p.info.Idle || p.info.Program == "" || programOf(p.info.Program).Shell {
		return statusIdle
	}
	last := time.Unix(0, p.lastData.Load())
	if since := c.Now().Sub(last); since < activeFor {
		c.After(activeFor - since)
		return statusRunning
	}
	return statusQuiet
}

// activityDot is the dot of a program printing: a solid dot in a soft
// halo. It does not animate, as drawing frames all along would cost more
// than it tells.
func activityDot(c *ui.Context, col ui.Color, size float32) ui.Element {
	e := ui.Box(c).Size(size+6, size+6).Margin(0, 0, 0, 1).Tooltip("Printing")
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		halo := size/2 + 2.5
		p.Fill(ui.Rect{X: cx - halo, Y: cy - halo, W: 2 * halo, H: 2 * halo}, col.Alpha(0.22), halo)
		p.Fill(ui.Rect{X: cx - size/2, Y: cy - size/2, W: size, H: size}, col, size/2)
	})
	return e
}

// errorBanner shows what went wrong with the server.
func (a *App) errorBanner(c *ui.Context, k *colors) {
	ui.Overlay(c, func() {
		ui.Row(c).Absolute().Bottom(18).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
			ui.Row(c).Gap(10).Padding(8, 10, 8, 14).Radius(12).Background(k.panel).Border(1, k.panelBorder).
				Shadow(0, 8, 24, 0, k.shadowFocused).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(ui.Hex("#ef4444"))
				ui.Text(c, a.err).FontSize(12.5).TextColor(k.text).MaxWidth(520)
				if iconButton(c, k, "x", "Dismiss", 22, 14).Clicked() {
					a.err = ""
				}
			})
		})
	})
}
