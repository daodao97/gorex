package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retty/internal/terminal"
)

func TestRenderLogIsBoundedTimingMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RETTY_DIR", dir)
	path := filepath.Join(dir, "render.log")
	logRenderEvent("fixture-session", terminal.RenderEvent{Kind: "slow_sync", Duration: 1600 * time.Millisecond})
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "session=fixture-session event=slow_sync duration_ms=1600") {
		t.Fatalf("missing timing metadata: %q, %v", data, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(renderLogLimit); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	logRenderEvent("fixture-session", terminal.RenderEvent{Kind: "sync_timeout", Duration: 2 * time.Second})
	previous, err := os.Stat(path + ".1")
	if err != nil || previous.Size() != renderLogLimit {
		t.Fatalf("log did not rotate: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || len(data) > 256 || !strings.Contains(string(data), "event=sync_timeout duration_ms=2000") {
		t.Fatalf("new log contains old data: %q, %v", data, err)
	}
}
