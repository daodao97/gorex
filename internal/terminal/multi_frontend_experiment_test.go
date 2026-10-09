package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Research-only replay of scripts/experiment-multi-frontend.py. Each capture
// came from a PTY that was never resized, so decoding at that fixed geometry
// is exact by construction; this checks only that each frontend shows the
// shared response at its own size without projection or a smaller font.
func TestMultiFrontendCaptureExperiment(t *testing.T) {
	dir := os.Getenv("RETTY_MULTI_FRONTEND_CAPTURE")
	if dir == "" {
		t.Skip("requires owned multi-frontend capture")
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
	for _, record := range manifest {
		if filepath.Base(record.File) != record.File {
			t.Fatal("capture filename must be local")
		}
		data, err := os.ReadFile(filepath.Join(dir, record.File))
		if err != nil {
			t.Fatal(err)
		}
		term, err := New(Options{Conn: newPipe(), FixedCols: record.Cols, FixedRows: record.Rows})
		if err != nil {
			t.Fatal(err)
		}
		defer term.Close()
		term.Feed(data)
		width, height, scale := 1000, 920, float32(2)
		if record.Cols == 48 {
			width, height, scale = 393, 680, 3
		}
		tester := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, width, height)
		tester.SetScale(scale)
		tester.Frame()
		if term.v.reflow || term.v.font.size != 13 {
			t.Fatal("viewport used projection or tiny font")
		}
		text := term.Text()
		name := strings.TrimSuffix(record.File, ".ansi")
		save(t, tester, "multi-"+name)
		t.Logf("%s %dx%d: fixture-099=%v draft=%v", name, record.Cols, record.Rows,
			strings.Contains(text, "fixture-099"), strings.Contains(text, "multi frontend fixture"))
		if !strings.Contains(text, "fixture-099") {
			t.Errorf("%s: final streamed line missing", name)
		}
	}
}
