package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsNavigationSearchAndDefaults(t *testing.T) {
	// Installed user hooks are modified settings too; use a fresh profile
	// when asserting the empty state after restoring the font default.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	previous, previousFont := prefs, termFont
	prefs = settings{FontSize: defaultFontSize}
	t.Cleanup(func() { prefs, termFont = previous, previousFont })
	a, tt := newTestApp(t)
	p := a.tab().Focus
	sid := p.SID
	tt.Key(ui.Cmd, ui.KeyComma)
	if panel, ok := tt.Find("Settings"); !ok || panel.X != 0 || panel.Y != 0 || panel.W != 1000 || panel.H != 620 {
		t.Fatalf("settings should fill the window: %+v", panel)
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("font size")
	if _, ok := tt.Find("Terminal font size"); !ok {
		t.Fatal("search did not find a setting in another section")
	}
	if _, ok := tt.Find("Show host name"); ok {
		t.Fatal("search kept unrelated settings")
	}
	if err := tt.Click("Terminal font size"); err != nil {
		t.Fatal(err)
	}
	before := prefs.FontSize
	tt.Key(0, ui.KeyUp)
	if prefs.FontSize <= before || readSettings().FontSize != prefs.FontSize {
		t.Fatal("font stepper did not apply and persist its new value")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("compact")
	if err := tt.Click("Only modified settings"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Compact mode"); ok {
		t.Fatal("modified filter included a default setting")
	}
	if err := tt.Click("Clear settings search"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Restore default Terminal font size"); err != nil {
		t.Fatal(err)
	}
	if prefs.FontSize != defaultFontSize || readSettings().FontSize != defaultFontSize {
		t.Fatal("restoring a default did not apply and persist")
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "所有设置均使用默认值") {
		t.Fatal("modified filter has no empty state")
	}
	for _, section := range []string{"Appearance", "Terminal settings", "About GoRex", "General"} {
		if err := tt.Click(section); err != nil {
			t.Fatal(err)
		}
	}
	// Capture both appearances and the minimum size when requested for QA.
	tt.SetSize(1512, 948)
	tt.SetScale(2)
	tt.SetDark(true)
	saveSettingsImage(t, tt, "settings-general-dark")
	tt.SetDark(false)
	saveSettingsImage(t, tt, "settings-general-light")
	tt.SetSize(560, 340)
	tt.SetDark(true)
	saveSettingsImage(t, tt, "settings-small-dark")
	for _, label := range []string{"Done", "Close Settings", "Only modified settings"} {
		if r, ok := tt.Find(label); !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > 560 || r.Y+r.H > 340 {
			t.Fatalf("small-window control %q is not reachable: %+v", label, r)
		}
	}
	tt.Key(0, ui.KeyEscape)
	if a.settingsOpen || p.SID != sid {
		t.Fatal("closing settings changed the session")
	}
	if !tt.Focused("Terminal") {
		t.Fatal("closing settings did not restore terminal focus")
	}
}

func saveSettingsImage(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("MYGO_TEST_IMAGES")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionHeaderSettingsCompatibility(t *testing.T) {
	previousPrefs := prefs
	t.Cleanup(func() { prefs = previousPrefs })
	t.Setenv("GOREX_DIR", t.TempDir())
	if s := readSettings(); s.HideSessionHeader || s.HideHost || s.CompactMode || s.FontSize != defaultFontSize {
		t.Fatalf("fresh settings: %+v", s)
	}
	// Old files have no header setting and must keep headers visible.
	if err := os.WriteFile(settingsPath(), []byte(`{"appearance":"dark","fontSize":14}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs.HideSessionHeader, prefs.HideHost = true, true
	prefs = readSettings()
	if prefs.HideSessionHeader || prefs.HideHost || prefs.Appearance != "dark" || prefs.FontSize != 14 {
		t.Fatalf("old settings: %+v", prefs)
	}
	a := &App{}
	a.setSessionHeadersVisible(false)
	prefs = readSettings()
	if !prefs.HideSessionHeader {
		t.Fatal("hidden headers were not persisted")
	}
	a.setSessionHeadersVisible(true)
	prefs = readSettings()
	if prefs.HideSessionHeader {
		t.Fatal("visible headers were not persisted")
	}
}

func TestCompactModeLayoutAndSessionContinuity(t *testing.T) {
	previousPrefs := prefs
	prefs = settings{FontSize: defaultFontSize}
	t.Cleanup(func() { prefs = previousPrefs })
	a, tt := newTestApp(t)
	tt.SetTitleBar(ui.TitleBar{Left: 80, Height: 44})
	first := a.tab()
	p := first.Focus
	waitFor(t, tt, "the shell", func() bool { return strings.Contains(p.term.Text(), "$") })
	oldCols, oldRows := p.term.Size()
	sid := p.SID
	tt.Key(ui.Cmd, ui.KeyComma)
	if err := tt.Click("Compact mode"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s := readSettings(); !s.CompactMode || s.HideHost || s.HideSessionHeader {
		t.Fatalf("compact mode overwrote standard preferences: %+v", s)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if p.SID != sid {
		t.Fatal("changing layout replaced the session")
	}
	if p.bounds.X != 0 || p.bounds.Y != compactTitleH || p.bounds.W != 1000 || p.bounds.Y+p.bounds.H != 620 {
		t.Fatalf("compact terminal does not reach window edges: %+v", p.bounds)
	}
	cols, rows := p.term.Size()
	if cols <= oldCols || rows <= oldRows {
		t.Fatalf("compact mode did not increase terminal space: %dx%d -> %dx%d", oldCols, oldRows, cols, rows)
	}
	if tabs, ok := tt.Find("Tabs"); !ok || tabs.X < 80 || tabs.H != compactTitleH || tabs.W < 800 {
		t.Fatalf("compact tab strip: %+v", tabs)
	}
	if _, ok := tt.Find("Split Right"); ok {
		t.Fatal("compact mode kept pane headers")
	}
	if _, ok := tt.Find("Command Palette"); ok {
		t.Fatal("compact mode kept expanded toolbar")
	}
	tt.Type("echo compact-focus")
	waitFor(t, tt, "terminal focus", func() bool { return strings.Contains(p.term.Text(), "echo compact-focus") })
	tt.Key(ui.Ctrl, ui.KeyU)
	p.term.Feed([]byte("\r\ncompact-target\r\n"))
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("compact-target")
	if s := p.term.SearchState(); s.Total != 1 {
		t.Fatalf("compact search: %+v", s)
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("New Tab"); err != nil {
		t.Fatal(err)
	}
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatal("compact new-tab button failed")
	}
	a.startRename()
	tt.Frame()
	tt.Type("compact logs")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.tab().Name != "compact logs" {
		t.Fatal("compact tab rename failed")
	}
	a.moveTab(a.tab(), 0)
	tt.Frame()
	if a.active != 0 || a.tab().Name != "compact logs" {
		t.Fatal("compact tab reorder lost active tab")
	}
	a.split(false)
	tt.Frame()
	if len(a.tab().panes()) != 2 {
		t.Fatal("compact split failed")
	}
	if _, ok := tt.Find("Divider"); !ok {
		t.Fatal("compact divider missing")
	}
	// The compact switch remains reachable at the minimum window size.
	tt.SetSize(560, 340)
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	tt.Type("compact")
	if err := tt.Click("Compact mode"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.CompactMode {
		t.Fatal("compact setting was not reachable through search in a small window")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if _, ok := tt.Find("Split Right"); !ok {
		t.Fatal("standard headers did not return")
	}
	if s := readSettings(); s.CompactMode || s.HideHost || s.HideSessionHeader {
		t.Fatalf("standard preferences not restored: %+v", s)
	}
	if a.tabs[1] != first || p.SID != sid {
		t.Fatal("layout toggle changed original session")
	}
}

func TestSettingsPageAndHiddenSessionHeaders(t *testing.T) {
	previousPrefs := prefs
	prefs = settings{FontSize: defaultFontSize}
	t.Cleanup(func() { prefs = previousPrefs })
	a, tt := newTestApp(t)
	p := a.tab().Focus
	waitFor(t, tt, "the shell", func() bool { return strings.Contains(p.term.Text(), "$") })
	if _, ok := tt.Find("Split Right"); !ok {
		t.Fatal("session controls are hidden by default")
	}
	hostName, _ := os.Hostname()
	hostLabel := "Host " + strings.TrimSuffix(hostName, ".local")
	if _, ok := tt.Find(hostLabel); !ok {
		t.Fatal("host name is hidden by default")
	}
	originalTabs, _ := tt.Find("Tabs")
	_, originalRows := p.term.Size()
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("Cmd-comma did not open settings")
	}
	// The search field takes focus; settings input must not reach the shell.
	if !tt.Focused("Search settings") {
		t.Fatal("settings search did not take focus")
	}
	if err := tt.Click("Show session titles and controls"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !prefs.HideSessionHeader {
		t.Fatal("settings switch did not hide session headers")
	}
	if _, ok := tt.Find("Split Right"); ok {
		t.Fatal("hidden session controls still exist")
	}
	_, hiddenRows := p.term.Size()
	if hiddenRows <= originalRows {
		t.Fatalf("hiding the header did not reclaim space: %d -> %d", originalRows, hiddenRows)
	}
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.HideSessionHeader {
		t.Fatal("switch did not save settings")
	}
	// The host switch is independent, and hiding it reclaims title-bar space.
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if s := readSettings(); !s.HideHost || !s.HideSessionHeader {
		t.Fatalf("host switch did not persist independent settings: %+v", s)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("Escape did not close settings")
	}
	if _, ok := tt.Find(hostLabel); ok {
		t.Fatal("hidden host name is still visible")
	}
	if tabs, _ := tt.Find("Tabs"); tabs.X >= originalTabs.X {
		t.Fatal("hiding the host did not reclaim title-bar space")
	}
	tt.Type("echo header-hidden")
	waitFor(t, tt, "focus restored", func() bool { return strings.Contains(p.term.Text(), "echo header-hidden") })
	tt.Key(ui.Ctrl, ui.KeyU)
	// Search continues working without a header, and returns to the find field.
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("header")
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("-query")
	if p.find.query != "header-query" {
		t.Fatalf("search focus after settings: %q", p.find.query)
	}
	a.closeFind(p)
	a.split(false)
	tt.Frame()
	if len(a.tab().panes()) != 2 {
		t.Fatal("splitting a hidden-header pane failed")
	}
	if _, ok := tt.Find("Split Right"); ok {
		t.Fatal("new pane did not follow header preference")
	}
	// Settings is also accessible from the palette when pane buttons are hidden.
	a.openPalette()
	tt.Frame()
	tt.Type("settings")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("palette did not open settings")
	}
	if err := tt.Click("Show session titles and controls"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if prefs.HideSessionHeader {
		t.Fatal("could not restore headers from settings")
	}
	if _, ok := tt.Find("Split Right"); !ok {
		t.Fatal("restored session controls are missing")
	}
	tt.SetSize(560, 340)
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if s := readSettings(); s.HideHost || s.HideSessionHeader {
		t.Fatalf("restoring both controls did not persist: %+v", s)
	}
	if done, ok := tt.Find("Done"); !ok || done.Y+done.H > 340 {
		t.Fatal("settings footer does not fit the minimum window height")
	}
	if err := tt.Click("Close Settings"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("close button did not close settings")
	}
	if _, ok := tt.Find(hostLabel); !ok {
		t.Fatal("restored host name is missing")
	}
}
