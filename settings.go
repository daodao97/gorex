package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"retty/internal/rex"
)

// settings are the user's choices, kept in settings.json beside the
// server's state.
type settings struct {
	// Appearance is "light", "dark", or "" to follow the system.
	Appearance                       string  `json:"appearance,omitempty"`
	FontSize                         float32 `json:"fontSize,omitempty"`
	LinkEditor                       string  `json:"linkEditor,omitempty"`
	CopyRawText                      bool    `json:"copyRawText,omitempty"`
	HideAgentNotifications           bool    `json:"hideAgentNotifications,omitempty"`
	HideAgentCompletionNotifications bool    `json:"hideAgentCompletionNotifications,omitempty"`
}

const defaultFontSize = 11.6

var prefs settings

func settingsPath() string { return filepath.Join(rex.Dir(), "settings.json") }

func loadSettings() {
	prefs = readSettings()
	termFont.Size = prefs.FontSize
	applyAppearance()
}

func readSettings() settings {
	var s settings
	if b, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(b, &s)
	}
	if s.FontSize < 6 || s.FontSize > 40 {
		s.FontSize = defaultFontSize
	}
	switch s.LinkEditor {
	case "", "vscode", "cursor", "system":
	default:
		s.LinkEditor = ""
	}
	return s
}

func (a *App) setLinkEditor(editor string) {
	prefs.LinkEditor = editor
	saveSettings()
}

func (a *App) setCopyRawText(raw bool) {
	prefs.CopyRawText = raw
	for _, tab := range a.tabs {
		for _, pane := range tab.panes() {
			if pane.term != nil {
				pane.term.SetCopyRawText(raw)
			}
		}
	}
	saveSettings()
}

func saveSettings() {
	b, _ := json.MarshalIndent(prefs, "", "  ")
	os.MkdirAll(rex.Dir(), 0o700)
	os.WriteFile(settingsPath(), b, 0o600)
}

func applyAppearance() {
	switch prefs.Appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

func (a *App) setAppearance(v string) {
	prefs.Appearance = v
	applyAppearance()
	saveSettings()
	if it := a.appearanceItems[v]; it != nil {
		it.SetChecked(true)
	}
}

// setFontSize changes the size of the terminals' text, in every pane.
func (a *App) setFontSize(size float32) {
	size = min(max(size, 8), 32)
	prefs.FontSize = size
	termFont.Size = size
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			if p.term != nil {
				p.term.SetFont(termFont)
			}
		}
	}
	saveSettings()
}
