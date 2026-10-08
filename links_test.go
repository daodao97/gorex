package main

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorex/internal/rex"
	"gorex/internal/terminal"
)

func TestTerminalLinkResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "中文 space #?.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		link   terminal.Link
		editor string
		remote bool
		want   string
		bad    bool
	}{
		{"relative", terminal.Link{Path: filepath.Base(path), Line: 12, Column: 3}, "vscode", false, (&url.URL{Scheme: "vscode", Host: "file", Path: path + ":12:3"}).String(), false},
		{"home", terminal.Link{Path: "~/" + filepath.Base(path), Line: 2}, "cursor", false, (&url.URL{Scheme: "cursor", Host: "file", Path: path + ":2"}).String(), false},
		{"file URL", terminal.Link{URL: (&url.URL{Scheme: "file", Path: path + ":4:2"}).String()}, "vscode", false, (&url.URL{Scheme: "vscode", Host: "file", Path: path + ":4:2"}).String(), false},
		{"default app", terminal.Link{Path: path, Line: 2}, "system", false, (&url.URL{Scheme: "file", Path: path}).String(), false},
		{"directory", terminal.Link{Path: dir}, "cursor", false, (&url.URL{Scheme: "file", Path: dir}).String(), false},
		{"web in ssh", terminal.Link{URL: "https://example.com/?q=1"}, "cursor", true, "https://example.com/?q=1", false},
		{"missing file", terminal.Link{Path: "missing.go"}, "cursor", false, "", true},
		{"remote path", terminal.Link{Path: path}, "cursor", true, "", true},
		{"remote file host", terminal.Link{URL: "file://other-machine/tmp/file.go"}, "cursor", false, "", true},
		{"unsupported scheme", terminal.Link{URL: "javascript:alert(1)"}, "cursor", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTerminalLink(tc.link, dir, dir, tc.editor, tc.remote)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got %q / %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestFileLinksThroughAppAndEditorSettings(t *testing.T) {
	previous := prefs
	prefs = settings{FontSize: defaultFontSize}
	t.Cleanup(func() { prefs = previous })
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	a, tt := newTestApp(t)
	dir := t.TempDir()
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.newTab(dir)
	p := a.tab().Focus
	waitFor(t, tt, "shell", func() bool { refresh(a); return strings.Contains(p.term.Text(), "$") })
	a.openSettings()
	tt.Frame()
	if err := tt.Click("Terminal settings"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("File link editor"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Cursor"); err != nil {
		t.Fatal(err)
	}
	if prefs.LinkEditor != "cursor" || readSettings().LinkEditor != "cursor" {
		t.Fatal("editor selector did not apply/save")
	}
	saveSettingsImage(t, tt, "file-link-settings")
	tt.Key(0, ui.KeyEscape)
	var opened []string
	a.openLinkURL = func(target string) error { opened = append(opened, target); return nil }
	tt.Frame()
	p.term.Feed([]byte("\x1b[H\x1b[2Jmain.go:42:5: example compiler error\r\nhttps://example.com/documentation\r\n"))
	tt.Frame()
	r, ok := tt.Find("Terminal")
	if !ok {
		t.Fatal("terminal not visible")
	}
	tt.ClickAtWith(ui.Cmd, r.X+3, r.Y+7)
	if len(opened) == 0 || opened[len(opened)-1] != (&url.URL{Scheme: "cursor", Host: "file", Path: filepath.Join(dir, "main.go") + ":42:5"}).String() {
		t.Fatalf("app Command click failed: %v (%s)", opened, a.err)
	}
	saveSettingsImage(t, tt, "file-links-compact")
}

func TestPaneLinkOpeningAndPreference(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	t.Setenv("GOREX_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Pane{info: rex.SessionInfo{Dir: dir}}
	var opened string
	a := &App{openLinkURL: func(target string) error { opened = target; return nil }}
	a.setLinkEditor("cursor")
	if readSettings().LinkEditor != "cursor" {
		t.Fatal("editor preference not persisted")
	}
	a.openTerminalLink(p, terminal.Link{Path: "main.go", Line: 7, Column: 2})
	if opened != (&url.URL{Scheme: "cursor", Host: "file", Path: path + ":7:2"}).String() || a.err != "" {
		t.Fatal("pane-relative link did not reach selected editor")
	}
	a.openLinkURL = func(string) error { return errors.New("editor unavailable") }
	a.openTerminalLink(p, terminal.Link{Path: "main.go"})
	if !strings.Contains(a.err, "editor unavailable") {
		t.Fatal("open failure not shown")
	}
	// Compatible default for old settings and invalid manually edited values.
	os.WriteFile(settingsPath(), []byte(`{"linkEditor":"invalid"}`), 0o600)
	if readSettings().LinkEditor != "" {
		t.Fatal("invalid editor preference retained")
	}
}
