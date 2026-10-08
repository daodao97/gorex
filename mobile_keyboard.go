package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"gorex/internal/mobile"
)

var mobileKeyboardActions = []ui.InputAction{
	{ID: "escape", Label: "Esc"},
	{ID: "ctrl", Label: "Ctrl", LongPressID: "lock:ctrl"},
	{ID: "option", Label: "Option", Symbol: "option", LongPressID: "lock:option"},
	{ID: "cmd", Label: "Cmd", Symbol: "command", LongPressID: "lock:cmd"},
	{ID: "left", Label: "←", Symbol: "arrow.left"},
	{ID: "right", Label: "→", Symbol: "arrow.right"},
	{ID: "more", Label: "更多", Symbol: "ellipsis", Items: []ui.InputAction{
		{ID: "shift", Label: "Shift", Symbol: "shift", LongPressID: "lock:shift"},
		{ID: "tab", Label: "Tab"},
		{ID: "up", Label: "↑", Symbol: "arrow.up"}, {ID: "down", Label: "↓", Symbol: "arrow.down"},
		{ID: "newline", Label: "换行"},
		{ID: "/", Label: "/"}, {ID: "-", Label: "-"}, {ID: "|", Label: "|"}, {ID: "\\", Label: "\\"}, {ID: "paste", Label: "粘贴"},
	}},
	{ID: "dismiss", Label: "收起", Symbol: "chevron.down"},
}

func keyboardModifier(id string) ui.Modifiers {
	switch id {
	case "ctrl":
		return ui.Ctrl
	case "option":
		return ui.Alt
	case "cmd":
		return ui.Super
	case "shift":
		return ui.Shift
	}
	return 0
}

func (m *mobileApp) keyboardActions() []ui.InputAction {
	actions := append([]ui.InputAction(nil), mobileKeyboardActions...)
	actions[6].Items = append([]ui.InputAction(nil), actions[6].Items...)
	decorate := func(a *ui.InputAction) {
		if mod := keyboardModifier(a.ID); mod != 0 {
			a.Selected = m.keyboardModifiers&mod != 0
			a.Locked = m.keyboardLocked&mod != 0
		}
	}
	for i := range actions {
		decorate(&actions[i])
		for j := range actions[i].Items {
			decorate(&actions[i].Items[j])
		}
	}
	return actions
}

func (m *mobileApp) clearKeyboardModifiers() {
	if m.keyboardModifiers == 0 && m.keyboardLocked == 0 {
		return
	}
	m.keyboardModifiers, m.keyboardLocked = 0, 0
	m.invalidate()
}
func (m *mobileApp) consumeKeyboardModifiers() {
	m.keyboardModifiers = m.keyboardLocked
	m.invalidate()
}

func (m *mobileApp) keyboardAction(c *ui.Context, id string) {
	if m.term == nil || m.background || m.needsRecovery() {
		return
	}
	lock := strings.HasPrefix(id, "lock:")
	if mod := keyboardModifier(strings.TrimPrefix(id, "lock:")); mod != 0 {
		if lock {
			m.keyboardModifiers |= mod
			m.keyboardLocked |= mod
		} else if m.keyboardModifiers&mod != 0 {
			m.keyboardModifiers &^= mod
			m.keyboardLocked &^= mod
		} else {
			m.keyboardModifiers |= mod
		}
		c.Invalidate()
		return
	}
	if key, ok := map[string]ui.Key{"escape": ui.KeyEscape, "tab": ui.KeyTab, "up": ui.KeyUp, "down": ui.KeyDown, "left": ui.KeyLeft, "right": ui.KeyRight, "/": ui.KeySlash, "-": ui.KeyMinus, "|": ui.KeyBackslash, "\\": ui.KeyBackslash}[id]; ok {
		mods := m.keyboardModifiers
		if id == "|" {
			mods |= ui.Shift
		}
		if m.term.SendKey(key, mods) {
			m.consumeKeyboardModifiers()
		}
		return
	}
	switch id {
	case "newline":
		m.term.InsertNewline()
	case "paste":
		m.pasteClipboard(c)
	case "more":
		m.keyboardMore = !m.keyboardMore
	case "dismiss":
		m.keyboardMore = false
		m.clearKeyboardModifiers()
		c.Blur()
		mobile.HideKeyboard()
	}
	c.Invalidate()
}

// Native buttons own iOS gestures. Desktop/headless previews expose the same
// state transitions through pointer long-press events for regression testing.
func (m *mobileApp) keyboardPreview(c *ui.Context) {
	button := func(action ui.InputAction) {
		b := ui.ButtonBase(c).Label(action.Label).Role(ui.RoleButton).KeepFocus().Height(44).MinWidth(44).Grow(1).Shrink(1).Radius(8)
		if b.Pressed() || action.Selected {
			b.Background(c.Theme().SurfacePressed)
		}
		b.Children(func() {
			label := action.Label
			if action.ID == "option" {
				label = "⌥"
			}
			if action.ID == "cmd" {
				label = "⌘"
			}
			if action.ID == "shift" {
				label = "⇧"
			}
			ui.Text(c, label).FontSize(13)
		})
		if action.LongPressID != "" {
			b.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputLongPress {
					m.keyboardAction(c, action.LongPressID)
					return true
				}
				return false
			})
		}
		if b.Clicked() {
			m.keyboardAction(c, action.ID)
		}
	}
	actions := m.keyboardActions()
	ui.Column(c).FillWidth().Padding(0, 8).Background(c.Theme().Surface).Children(func() {
		if m.keyboardMore {
			items := actions[6].Items
			for i := 0; i < len(items); i += 5 {
				ui.Row(c).FillWidth().Gap(2).Children(func() {
					for _, a := range items[i:min(i+5, len(items))] {
						button(a)
					}
				})
			}
		}
		ui.Row(c).FillWidth().Gap(2).Children(func() {
			for _, a := range actions {
				button(a)
			}
		})
	})
}
