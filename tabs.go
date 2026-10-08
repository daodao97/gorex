package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// tabStrip divides the title bar evenly between tabs and lets its gaps drag the window.
func (a *App) tabStrip(c *ui.Context, k *colors) {
	track := ui.Row(c).Key("tabs").Grow(1).Basis(0).MinWidth(0).Height(compactTitleH).
		Background(k.track).AlignItems(ui.Center).ClipX().DragWindow().Role(ui.RoleTabList).Label("Tabs")
	track.Children(func() {
		for i, t := range slices.Clone(a.tabs) {
			a.tabItem(c, k, i, t)
		}
	})
}

// tabItem draws a flat tab with its label, agent status and shortcut.
func (a *App) tabItem(c *ui.Context, k *colors, i int, t *Tab) {
	active := i == a.active
	bottom := float32(1)
	bg := k.track
	e := ui.Row(c).Key(t.ID).Grow(1).Basis(0).MinWidth(0).Height(compactTitleH).
		Padding(0, 8).Gap(6).AlignItems(ui.Center).Role(ui.RoleTab).Selected(active)
	name, detail := t.label()
	e.Label(name + " " + detail)
	if agentPane, agentState := tabAgentState(t); agentPane != nil {
		e.Tooltip(programOf(agentState.ID).Name + " · " + agentStateLabel(agentState))
	}
	if active {
		bottom, bg = 0, terminalBackground(c)
	} else if e.Hovered() {
		bg = k.hover
	}
	e.Background(bg).BorderWidth(0, 1, bottom, 0).BorderColor(k.headerBorder)
	e.Transition(ui.ElementTransition{Colors: true, Position: true, Duration: 160 * time.Millisecond})
	e.Drag(t)
	if dragged, ok := ui.Drop[*Tab](e); ok && dragged != t {
		a.moveTab(dragged, i)
	}
	if _, over := ui.DragOver[*Tab](e); over {
		e.Border(1.5, k.busy.Alpha(0.6))
	}
	if e.Dragging() {
		e.Opacity(0.4)
	}
	if e.Clicked() && !active {
		a.selectTab(i)
	}
	if e.DoubleClicked() {
		a.selectTab(i)
		a.startRename()
	}
	e.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, t) })
	hovered := e.Hovered()
	e.Children(func() { a.compactTabLabel(c, k, i, t, name, detail, hovered) })
}

// renameField edits the name of a tab in place: Enter or moving the focus
// away keeps it, Escape goes back, and an empty name follows the panes
// again.
func (a *App) renameField(c *ui.Context, t *Tab, name string) {
	in := ui.TextInput(c, &a.renameText).Placeholder(name).Grow(1).MinWidth(0).Height(22).FontSize(13).AutoFocus()
	done := func(keep bool) {
		if keep {
			t.Name = strings.TrimSpace(a.renameText)
			a.changed()
		}
		a.renaming, a.renameFocus = nil, false
		a.focusReq = t.Focus
	}
	switch {
	case in.Shortcut(0, ui.KeyEscape):
		done(false)
	case in.Submitted():
		done(true)
	case in.Focused():
		a.renameFocus = true
	case a.renameFocus:
		done(true) // the focus went elsewhere
	}
}

func (t *Tab) attention() bool {
	for _, p := range t.panes() {
		if p.attention {
			return true
		}
	}
	return false
}

func (a *App) tabBusy(c *ui.Context, t *Tab) bool {
	for _, p := range t.panes() {
		if a.statusOf(c, p) == statusRunning {
			return true
		}
	}
	return false
}

func (a *App) moveTab(t *Tab, to int) {
	from := slices.Index(a.tabs, t)
	if from < 0 || to == from {
		return
	}
	activeTab := a.tab()
	a.tabs = slices.Delete(a.tabs, from, from+1)
	a.tabs = slices.Insert(a.tabs, min(to, len(a.tabs)), t)
	a.active = slices.Index(a.tabs, activeTab)
	a.changed()
}

func (a *App) tabMenu(m *ui.Menu, t *Tab) {
	if m.Item("Rename Tab…").Chosen() {
		a.selectTab(slices.Index(a.tabs, t))
		a.startRename()
	}
	if t.Name != "" && m.Item("Reset Name").Chosen() {
		t.Name = ""
		a.changed()
	}
	m.Separator()
	i := slices.Index(a.tabs, t)
	if m.Item("Move Left").Disabled(i <= 0).Chosen() {
		a.moveTab(t, i-1)
	}
	if m.Item("Move Right").Disabled(i >= len(a.tabs)-1).Chosen() {
		a.moveTab(t, i+1)
	}
	m.Separator()
	if m.Item("New Tab").Shortcut(ui.Cmd, ui.KeyT).Chosen() {
		a.newTab(a.currentDir())
	}
	if m.Item("Close Tab").Chosen() {
		a.later(a.ctx, func() { a.closeTab(t) })
	}
	if m.Item("Close Other Tabs").Disabled(len(a.tabs) < 2).Chosen() {
		a.later(a.ctx, func() {
			for _, o := range slices.Clone(a.tabs) {
				if o != t {
					a.closeTab(o)
				}
			}
		})
	}
}

func paneProgram(p *Pane) program {
	return programOf(sessionProgramName(p.info))
}

// fadeText draws spans on a line, fading the end out when they do not
// fit, rather than cutting them with an ellipsis.
func fadeText(p *ui.Painter, r ui.Rect, spans []ui.Span) {
	key := fmt.Sprintf("%.0f|%v", r.W, spans)
	f, ok := fades[key]
	if !ok {
		f = layoutFade(p, r.W, spans)
		if len(fades) > 512 {
			clear(fades)
		}
		fades[key] = f
	}
	p.RichText(r.X, r.Y+(r.H-f.h)/2, 0, f.spans...)
}

// fades caches the spans of faded texts, by their width and spans.
var fades = map[string]fadeLayout{}

type fadeLayout struct {
	spans []ui.Span
	h     float32
}

func layoutFade(p *ui.Painter, width float32, spans []ui.Span) fadeLayout {
	w, h := p.MeasureText(0, spans...)
	if w <= width {
		return fadeLayout{spans, h}
	}
	r := ui.Rect{W: width}
	const fade = 30
	// Characters are measured one by one; those past the start of the
	// fade lose their opacity as they near the edge.
	var out []ui.Span
	x := float32(0)
	for _, s := range spans {
		start := 0
		for i := 0; i < len(s.Text); {
			_, n := utf8.DecodeRuneInString(s.Text[i:])
			ch := s
			ch.Text = s.Text[i : i+n]
			cw, _ := p.MeasureText(0, ch)
			if x+cw > r.W-fade {
				if start < i {
					head := s
					head.Text = s.Text[start:i]
					out = append(out, head)
				}
				f := 1 - (x+cw/2-(r.W-fade))/fade
				// A color of no alpha is the text's own color: stop
				// before.
				ch.Color = ch.Color.Alpha(max(f, 0) * max(f, 0))
				if ch.Color.A < 4 {
					return fadeLayout{out, h}
				}
				out = append(out, ch)
				start = i + n
			}
			x += cw
			i += n
		}
		if start < len(s.Text) {
			tail := s
			tail.Text = s.Text[start:]
			out = append(out, tail)
		}
	}
	return fadeLayout{out, h}
}
