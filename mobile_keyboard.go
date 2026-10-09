package main

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
		{ID: "paste", Label: "粘贴"},
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
	return m.keyboardLatch.Decorate(mobileKeyboardActions, keyboardModifier)
}

func (m *mobileApp) clearKeyboardModifiers() {
	if m.keyboardLatch.Clear() {
		m.invalidate()
	}
}
func (m *mobileApp) consumeKeyboardModifiers() {
	m.keyboardLatch.Consume()
	m.invalidate()
}

// Accessory and Return actions run outside a build pass, so they use the
// window's persistent services rather than a build-scoped Context.
func (m *mobileApp) dismissKeyboard(s ui.Services) {
	m.keyboardMore = false
	m.clearKeyboardModifiers()
	s.Blur()
	mygo.App.DismissKeyboard()
	s.Invalidate()
}

func (m *mobileApp) keyboardAction(s ui.Services, id string) {
	if m.term == nil || m.background || m.needsRecovery() {
		return
	}
	if mod := keyboardModifier(strings.TrimPrefix(id, "lock:")); mod != 0 {
		if strings.HasPrefix(id, "lock:") {
			m.keyboardLatch.Lock(mod)
		} else {
			m.keyboardLatch.Tap(mod)
		}
		s.Invalidate()
		return
	}
	if key, ok := map[string]ui.Key{"escape": ui.KeyEscape, "tab": ui.KeyTab, "up": ui.KeyUp, "down": ui.KeyDown, "left": ui.KeyLeft, "right": ui.KeyRight}[id]; ok {
		if m.term.SendKey(key, m.keyboardLatch.Active()) {
			m.consumeKeyboardModifiers()
		}
		return
	}
	switch id {
	case "newline":
		m.term.InsertNewline()
	case "paste":
		m.pasteClipboard(s)
	case "dismiss":
		m.dismissKeyboard(s)
	}
	s.Invalidate()
}

// Native buttons own iOS gestures. Desktop/headless previews draw the same
// actions with MyGo's Go accessory bar, which sends the same IDs.
func (m *mobileApp) keyboardPreview(c *ui.Context) {
	ui.InputAccessoryBar(c, m.keyboardActions(), &m.keyboardMore, func(id string) { m.keyboardAction(c.Services(), id) })
}
