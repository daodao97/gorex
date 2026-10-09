package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Research-only: shows how today's phone projection renders a single
// desktop-sized capture. RETTY_PROJECTION_CAPTURE names a capture directory
// with manifest.json whose records all share one geometry.
func TestProjectionCaptureExperiment(t *testing.T) {
	dir := os.Getenv("RETTY_PROJECTION_CAPTURE")
	if dir == "" {
		t.Skip("requires owned capture")
	}
	loadLib(t)
	var manifest []struct {
		File       string
		Cols, Rows int
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, record := range manifest {
		if filepath.Base(record.File) != record.File || record.Cols != manifest[0].Cols || record.Rows != manifest[0].Rows {
			t.Fatal("capture must be local and single-geometry")
		}
		data, err := os.ReadFile(filepath.Join(dir, record.File))
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, data...)
	}
	cols, rows := manifest[0].Cols, manifest[0].Rows
	desktop, err := New(Options{Conn: newPipe(), FixedCols: cols, FixedRows: rows})
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	phone, err := New(Options{Conn: newPipe(), FixedCols: cols, FixedRows: rows, ReflowView: true})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	desktop.Feed(raw)
	phone.Feed(raw)
	desktopUI := ui.NewTester(func(c *ui.Context) { View(c, desktop).Fill() }, 1000, 920)
	desktopUI.SetScale(2)
	desktopUI.Frame()
	save(t, desktopUI, "projection-desktop")
	phoneUI := ui.NewTester(func(c *ui.Context) { View(c, phone).Fill() }, 393, 680)
	phoneUI.SetScale(3)
	phoneUI.Frame()
	save(t, phoneUI, "projection-phone")
}
