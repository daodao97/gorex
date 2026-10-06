package main

import (
	"slices"
	"testing"
)

func TestTerminalContextMenuSplit(t *testing.T) {
	for _, compact := range []bool{false, true} {
		mode := "normal"
		if compact {
			mode = "compact"
		}
		for _, direction := range []struct {
			label string
			down  bool
		}{
			{"Split Pane Vertically", false},
			{"Split Pane Horizontally", true},
		} {
			t.Run(mode+"/"+direction.label, func(t *testing.T) {
				previous := prefs
				prefs.CompactMode = compact
				t.Cleanup(func() { prefs = previous })
				a, tt := newTestApp(t)
				tab := a.tab()
				target := tab.Focus
				a.split(false)
				other := tab.Focus
				tt.Frame()
				// Give the unfocused target its own directory. Right-clicking
				// it must split that pane and inherit its real shell directory.
				target.term.Send([]byte("cd /usr/bin\r"))
				waitFor(t, tt, "target directory", func() bool {
					refresh(a)
					return target.info.Dir == "/usr/bin"
				})
				tab.setFocus(other)
				a.focusReq = other
				tt.Frame()
				node, otherNode := target.Node, other.Node
				bounds := target.bounds
				tt.RightClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
				for _, label := range []string{"Copy", "Paste", "Clear Scrollback", "Split Pane Vertically", "Split Pane Horizontally"} {
					if !slices.Contains(tt.Menu(), label) {
						t.Fatalf("missing %q in menu %q", label, tt.Menu())
					}
				}
				if err := tt.ChooseMenuItem(direction.label); err != nil {
					t.Fatal(err)
				}
				if len(tab.panes()) != 3 || node.Pane != nil || node.Vertical != direction.down || node.A.Pane != target {
					t.Fatalf("wrong split: panes=%d, node=%+v", len(tab.panes()), node)
				}
				added := node.B.Pane
				if added == nil || added == target || added == other || added.SID == "" || tab.Focus != added {
					t.Fatal("new session did not receive focus")
				}
				if other.Node != otherNode || otherNode.Pane != other {
					t.Fatal("split changed the previously focused pane")
				}
				if added.startDir != "/usr/bin" {
					t.Fatalf("new pane directory = %q", added.startDir)
				}
				waitFor(t, tt, "new shell directory", func() bool {
					refresh(a)
					return added.info.Dir == "/usr/bin"
				})
			})
		}
	}
}

// A native menu can outlive the pane that originally opened it.
func TestContextSplitIgnoresClosedPane(t *testing.T) {
	a, tt := newTestApp(t)
	tab := a.tab()
	closed := tab.Focus
	a.split(false)
	a.closePane(closed)
	tt.Frame()
	a.splitPane(closed, true)
	if len(tab.panes()) != 1 {
		t.Fatal("stale context menu split a surviving pane")
	}
}
