package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"gorex/internal/agents"
	"gorex/internal/rex"
)

func TestAgentTabs(t *testing.T) {
	previous := prefs
	t.Cleanup(func() { prefs = previous })
	registerFonts()
	p := &Pane{ID: 2, info: rex.SessionInfo{Shell: "zsh", Dir: "/work/repo"}}
	tab := &Tab{ID: 1, Focus: p, Root: &Node{ID: 3, Pane: p}}
	p.Tab = tab
	a := &App{tabs: []*Tab{tab}}
	tt := ui.NewTester(a.view, 1000, 620)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for _, agent := range agents.All {
		p.info.Program, p.info.Args, p.info.Idle = agent.Aliases[0], nil, false
		p.title, tab.Name = "", ""
		tt.Frame()
		if name, _ := tab.label(); name != agent.Name {
			t.Fatalf("%s title %q", agent.ID, name)
		}
		if _, ok := tt.Find(agent.Name + " icon"); !ok {
			t.Fatalf("missing %s tab icon", agent.ID)
		}
		// All SVGs are parsed and painted in both title bar styles.
		if prog := paneProgram(p); !prog.Agent || prog.Glyph == "square-terminal" {
			t.Fatalf("%s has a generic program icon", agent.ID)
		}
	}
	p.info.Program, p.info.Args = "claude", nil
	for title, want := range map[string]string{
		"✳ Claude Code":         "Claude Code",
		" ✳\ufe0e Claude Code ": "Claude Code",
		"✳\ufe0f Review login":  "Review login",
		"Review ✳ login":        "Review ✳ login",
		"✳":                     "Claude Code",
	} {
		p.title = title
		tt.Frame()
		if name, _ := tab.label(); name != want {
			t.Fatalf("Claude title %q: got %q, want %q", title, name, want)
		}
		if _, ok := tt.Find("Claude Code icon"); !ok {
			t.Fatal("cleaning the Claude title hid its brand icon")
		}
	}
	p.title = ""
	p.info.Program = "node"
	p.info.Args = []string{"node", "/opt/node_modules/@google/gemini-cli/dist/index.js"}
	tt.Frame()
	if name, _ := tab.label(); name != "Gemini CLI" {
		t.Fatalf("wrapped agent title %q", name)
	}
	p.title = "Review the login flow"
	if name, _ := tab.label(); name != p.title {
		t.Fatal("agent's meaningful terminal title was lost")
	}
	tab.Name = "my project"
	tt.Frame()
	if name, _ := tab.label(); name != tab.Name {
		t.Fatal("manual tab name was overwritten")
	}
	if _, ok := tt.Find("Gemini CLI icon"); !ok {
		t.Fatal("manual name hid the agent icon")
	}
	p.info = rex.SessionInfo{Shell: "zsh", Program: "zsh", Dir: "/work/repo", Idle: true}
	tab.Name = ""
	tt.Frame()
	if name, _ := tab.label(); name != "zsh" {
		t.Fatal("exiting the agent did not restore shell title")
	}
	if _, ok := tt.Find("Gemini CLI icon"); ok {
		t.Fatal("exiting the agent left its icon behind")
	}

	// Optional screenshots show the real embedded icons without launching
	// agents or changing any of the user's running sessions.
	if dir := os.Getenv("MYGO_TEST_IMAGES"); dir != "" {
		a.tabs = nil
		for i, id := range []string{"claude", "codex", "gemini", "cursor-agent", "opencode", "copilot", "pi", "omp"} {
			p := &Pane{ID: 100 + i, info: rex.SessionInfo{Shell: "zsh", Program: id, Dir: "/work/repo"}}
			if id == "claude" {
				p.title = "✳ Claude Code"
			}
			tab := &Tab{ID: 200 + i, Focus: p, Root: &Node{ID: 300 + i, Pane: p}}
			p.Tab = tab
			a.tabs = append(a.tabs, tab)
		}
		tt.SetSize(1512, 400)
		tt.SetScale(2)
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)

			tt.Frame()
			name := "agent-tabs-light.png"
			if dark {
				name = "agent-tabs-dark.png"
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
