package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"retty/internal/rex"
	"retty/internal/terminal"
)

var renderLogMu sync.Mutex

const renderLogLimit = 1 << 20

// Keep only timing metadata for abnormal redraws, never terminal text or
// input. Two bounded files are enough to diagnose an intermittent flicker.
func logRenderEvent(sid string, event terminal.RenderEvent) {
	renderLogMu.Lock()
	defer renderLogMu.Unlock()
	path := filepath.Join(rex.Dir(), "render.log")
	if info, err := os.Stat(path); err == nil && info.Size() >= renderLogLimit {
		if err := os.Rename(path, path+".1"); err != nil {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s pid=%d session=%s event=%s duration_ms=%d\n",
		time.Now().Format(time.RFC3339Nano), os.Getpid(), sid, event.Kind, event.Duration.Milliseconds())
}
