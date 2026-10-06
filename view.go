package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

const (
	gap           = 8  // between panes, and around them
	titleH        = 44 // the title bar
	cardR         = 12 // the panes' corners
	headerH       = 33 // the panes' headers
	compactTitleH = 32
	compactGap    = 1.0 // visible separator; its pointer target is wider
	compactHit    = 4
	activeFor     = 1500 * time.Millisecond
)

func (a *App) view(c *ui.Context) {
	a.ctx = c
	a.runPosted()
	k := colorsOf(c)
	a.focusedWin = a.win == nil || a.win.IsFocused()
	c.Root().Background(k.bgBottom)
	ui.Column(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { paintBackground(p, r, k) }).Children(func() {
		a.titleBar(c, k)
		inset := float32(gap)
		if prefs.CompactMode {
			inset = 0
		}
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, inset, inset, inset).Children(func() {
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

// titleBar draws the title bar under the window's controls: the host,
// the tabs, and the buttons of the command palette and a new tab.
func (a *App) titleBar(c *ui.Context, k *colors) {
	bar := c.TitleBar()
	if prefs.CompactMode {
		a.compactTitleBar(c, k, bar)
		return
	}
	left := bar.Left
	if left == 0 {
		left = 12 // in full screen
	} else {
		left += 14
	}
	ui.Row(c).Height(titleH).Padding(0, max(bar.Right, 10), 0, left).Gap(14).AlignItems(ui.Center).DragWindow().Children(func() {
		if !prefs.HideHost {
			a.hostChip(c, k)
		}
		a.tabStrip(c, k)
		// The title bar between the tabs and the buttons, which drags the
		// window, and a double click on which zooms it.
		ui.Spacer(c).Key("title-space").MinWidth(titleFree - 14)
		ui.Row(c).Key("title-actions").Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
			a.appearanceButton(c, k)
			if iconButton(c, k, "command", "Command Palette", 30, 17).Tooltip("Command Palette  ⇧⌘P").Clicked() {
				a.openPalette()
			}
			if iconButton(c, k, "plus", "New Tab", 30, 19).Tooltip("New Tab  ⌘T").Clicked() {
				a.newTab(a.currentDir())
			}
		})
	})
}

// appearanceButton makes the theme easy to switch without opening the
// menu bar. Its context menu also offers following the system.
func (a *App) appearanceButton(c *ui.Context, k *colors) {
	glyph, label, next := "moon", "Switch to Dark Mode", "dark"
	if c.Theme().Dark {
		glyph, label, next = "sun", "Switch to Light Mode", "light"
	}
	b := iconButton(c, k, glyph, label, 30, 17).Tooltip(label)
	if b.Clicked() {
		a.setAppearance(next)
	}
	b.ContextMenu(func(m *ui.Menu) {
		for _, option := range []struct{ label, value string }{
			{"System", ""}, {"Light", "light"}, {"Dark", "dark"},
		} {
			if m.Item(option.label).Checked(prefs.Appearance == option.value).Chosen() {
				a.setAppearance(option.value)
			}
		}
	})
}

// iconButton is a borderless button of an icon, with a face on hover;
// label names it for screen readers and its tooltip.
func iconButton(c *ui.Context, k *colors, name, label string, size, iconSize float32) *ui.Element {
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
	if prefs.CompactMode && t.Root.Pane == nil {
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
func (a *App) node(c *ui.Context, k *colors, t *Tab, n *Node) *ui.Element {
	if n.Pane != nil {
		return a.paneCard(c, k, t, n.Pane)
	}
	box := ui.Row(c)
	if n.Vertical {
		box = ui.Column(c)
	}
	box.Key(n.ID).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch)
	bounds := box.Bounds()
	// The tree may change as its panes build, as a split: build the
	// children it has now.
	first, second, ratio := n.A, n.B, n.Ratio
	spacing := float32(gap)
	if prefs.CompactMode {
		spacing = compactGap
	}
	box.Children(func() {
		a.node(c, k, t, first).Grow(ratio).Basis(0).MinWidth(0).MinHeight(0)
		if prefs.CompactMode {
			space := ui.Box(c).Key("divider-space").Shrink(0)
			if n.Vertical {
				space.Height(spacing)
			} else {
				space.Width(spacing)
			}
		}
		div := ui.Box(c).Key("divider").Role(ui.RoleSplitter).Label("Divider")
		if prefs.CompactMode {
			// Only one point takes layout space. The absolute pointer
			// target spans both panes and stays centered on that line.
			offset := spacing*(0.5-ratio) - compactHit/2
			if n.Vertical {
				div.Absolute().Height(compactHit).Left(0).Right(0).
					TopPercent(100 * ratio).MarginY(offset).Cursor(ui.CursorResizeRow)
			} else {
				div.Absolute().Width(compactHit).Top(0).Bottom(0).
					LeftPercent(100 * ratio).MarginX(offset).Cursor(ui.CursorResizeColumn)
			}
		} else if n.Vertical {
			div.Height(spacing).Cursor(ui.CursorResizeRow)
		} else {
			div.Width(spacing).Cursor(ui.CursorResizeColumn)
		}
		if dx, dy, ok := div.Dragged(); ok {
			total := bounds.W - spacing
			d := dx
			if n.Vertical {
				total, d = bounds.H-spacing, dy
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
		compact := prefs.CompactMode
		showGrip := div.Hovered() || div.Dragging() || div.Pressed()
		if compact || showGrip {
			width, height := c.Size()
			div.Draw(func(p *ui.Painter, r ui.Rect) {
				if compact {
					// Reach the center of adjoining gaps at nested split
					// junctions, without drawing into the title bar.
					if n.Vertical {
						x0, x1 := max(0, r.X-compactGap/2), min(width, r.X+r.W+compactGap/2)
						p.Fill(ui.Rect{X: x0, Y: r.Y + r.H/2 - 0.5, W: x1 - x0, H: 1}, k.headerBorder, 0)
					} else {
						y0, y1 := max(compactTitleH, r.Y-compactGap/2), min(height, r.Y+r.H+compactGap/2)
						p.Fill(ui.Rect{X: r.X + r.W/2 - 0.5, Y: y0, W: 1, H: y1 - y0}, k.headerBorder, 0)
					}
				}
				if showGrip {
					if n.Vertical {
						p.Fill(ui.Rect{X: r.X + r.W/2 - 18, Y: r.Y + r.H/2 - 1.5, W: 36, H: 3}, k.iconMuted.Alpha(0.6), 1.5)
					} else {
						p.Fill(ui.Rect{X: r.X + r.W/2 - 1.5, Y: r.Y + r.H/2 - 18, W: 3, H: 36}, k.iconMuted.Alpha(0.6), 1.5)
					}
				}
			})
		}
		a.node(c, k, t, second).Grow(1 - ratio).Basis(0).MinWidth(0).MinHeight(0)
	})
	return box
}

// paneCard draws a pane: a card with a header over its terminal.
func (a *App) paneCard(c *ui.Context, k *colors, t *Tab, p *Pane) *ui.Element {
	focused := t.Focus == p
	card := ui.Column(c).Key(p.ID).Radius(cardR).Clip().MinWidth(0).MinHeight(0)
	bg, border, shadow := k.card, k.cardBorder, k.shadow
	if focused {
		bg, border, shadow = k.cardFocused, k.cardBorderFocused, k.shadowFocused
	}
	if prefs.CompactMode {
		card.Radius(0).Background(terminalBackground(c))
	} else {
		card.Background(bg).Border(1, border).
			Shadow(0, 1, 2, 0, shadow).
			Shadow(0, 6, 22, -2, shadow)
	}
	card.Transition(ui.ElementTransition{Colors: true, Duration: 160 * time.Millisecond})
	hovered := card.Hovered()
	card.Children(func() {
		if !prefs.HideSessionHeader && !prefs.CompactMode {
			a.paneHeader(c, k, t, p, focused, hovered)
		}
		if p.find.open {
			a.findBar(c, k, t, p)
		}
		body := ui.Box(c).Key("terminal-body").Grow(1).MinHeight(0).Padding(0, 5, 6, 5)
		if prefs.CompactMode {
			body.Padding(0)
		}
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
			if tv.Focused() {
				p.attention = false
			}
		})
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
	return card
}

// paneHeader draws a pane's program, title and directory, its activity,
// and its buttons.
func (a *App) paneHeader(c *ui.Context, k *colors, t *Tab, p *Pane, focused, hovered bool) {
	name, detail := p.label()
	prog := paneProgram(p)
	h := ui.Row(c).Key("session-header").Height(headerH).Padding(0, 8, 0, 12).Gap(7).AlignItems(ui.Center).MinWidth(0)
	if c.Theme().Dark {
		bg := k.header
		if focused {
			bg = k.headerFocused
		}
		h.Background(bg).BorderWidth(0, 0, 1, 0).BorderColor(k.headerBorder)
	}
	if h.DoubleClicked() {
		t.setFocus(p)
		a.toggleZoom()
	}
	if h.Clicked() {
		t.setFocus(p)
		a.focusReq = p
	}
	h.Children(func() {
		ui.Icon(c, icon(prog.Glyph)).Size(14.5, 14.5).TextColor(k.text)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, name).FontSize(12.5).FontWeight(600).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0).MaxWidthPercent(80)
			if detail != "" {
				ui.Text(c, detail).FontSize(12.5).FontWeight(500).TextColor(k.text.Alpha(0.86)).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			}
			a.activityBadge(c, k, p)
		})
		show := focused || hovered
		ui.Row(c).Gap(1).AlignItems(ui.Center).Opacity(map[bool]float32{true: 1, false: 0}[show]).Children(func() {
			if !show {
				return
			}
			if iconButton(c, k, "columns-2", "Split Right", 26, 16).Tooltip("Split Right  ⌘D").Clicked() {
				t.setFocus(p)
				a.split(false)
			}
			if iconButton(c, k, "rows-2", "Split Down", 26, 16).Tooltip("Split Down  ⇧⌘D").Clicked() {
				t.setFocus(p)
				a.split(true)
			}
			zoom, tip := "maximize-2", "Zoom  ⇧⌘↩"
			if t.Zoom == p {
				zoom, tip = "minimize-2", "Unzoom  ⇧⌘↩"
			}
			if iconButton(c, k, zoom, "Zoom", 26, 16).Tooltip(tip).Clicked() {
				t.setFocus(p)
				a.toggleZoom()
			}
			if iconButton(c, k, "x", "Close Pane", 26, 17).Tooltip("Close Pane  ⌘W").Clicked() {
				a.later(c, func() { a.closePane(p) })
			}
		})
	})
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

// activityBadge shows what a pane does: a pulse while its program
// prints, a dot when it asks for attention.
func (a *App) activityBadge(c *ui.Context, k *colors, p *Pane) {
	if s := paneAgentState(p); s.State != "" {
		a.agentIndicator(c, k, p, s)
		return
	}
	switch a.statusOf(c, p) {
	case statusRunning:
		activityDot(c, k.busy, 6)
	case statusAttention:
		ui.Box(c).Size(7, 7).Radius(4).Background(k.attention).Margin(0, 0, 0, 2).Tooltip("Needs attention")
	}
}

// activityDot is the dot of a program printing: a solid dot in a soft
// halo. It does not animate, as drawing frames all along would cost more
// than it tells.
func activityDot(c *ui.Context, col ui.Color, size float32) *ui.Element {
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
