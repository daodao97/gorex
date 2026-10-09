package remote

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retty/internal/rex"
)

// The native runner writes markers only into this disposable shell. Verify
// that a background visit longer than the old 25/30-second cutoffs keeps the
// same control socket, then that losing only control preserves the session.
func TestTailcatIOSBackgroundRetention(t *testing.T) {
	fixture := os.Getenv("RETTY_IOS_BACKGROUND_FIXTURE")
	if fixture == "" {
		t.Skip("set RETTY_IOS_BACKGROUND_FIXTURE for the native iPhone runner")
	}
	dir := t.TempDir()
	t.Setenv("RETTY_DIR", dir)
	go rex.Serve()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", rex.SocketPath())
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	desktop, err := rex.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	defer desktop.Shutdown()
	session, err := desktop.Create(rex.CreateOptions{Dir: dir, Command: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	bridge, err := Start(ctx, rex.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	data, _ := json.Marshal(struct{ Link, Existing, Created, Directory string }{Link: bridge.Link(), Existing: session.ID, Directory: dir})
	if err := os.WriteFile(fixture, data, 0600); err != nil {
		t.Fatal(err)
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	var retained net.Conn
	var started time.Time
	idleObserved, returned, dropped := false, false, false
	for ctx.Err() == nil {
		bridge.mu.Lock()
		if retained == nil && exists("background-start") && len(bridge.devices) == 1 {
			for conn := range bridge.devices {
				retained = conn
			}
			started = time.Now()
		}
		_, stillRetained := bridge.devices[retained]
		controlCount := len(bridge.devices)
		bridge.mu.Unlock()
		// Observe presence only inside the runner's 45-second Home interval.
		// Foreground list replies precede typing the return marker, so checking
		// until that marker arrives would mistake a successful resume for a
		// background heartbeat and tear down the fixture prematurely.
		if retained != nil && time.Since(started) > 25*time.Second && time.Since(started) < 40*time.Second {
			if !stillRetained || len(bridge.Devices()) != 0 {
				t.Fatalf("background control retention failed: socket retained=%v, active phones=%d", stillRetained, len(bridge.Devices()))
			}
			idleObserved = true
		}
		if !returned && exists("background-returned") {
			if !idleObserved || !stillRetained || controlCount != 1 {
				t.Fatal("native return rebuilt the control socket instead of reusing it")
			}
			returned = true
			t.Log("PASS: 45-second background visit retained control; foreground presence resumed")
		}
		if returned && !dropped && exists("disconnect-control") {
			retained.Close()
			dropped = true
		}
		if exists("redial-returned") {
			if !returned || !dropped || stillRetained || controlCount != 1 {
				t.Fatal("control-only recovery did not replace exactly one socket")
			}
			list, err := desktop.List()
			if err != nil || len(list) != 1 || list[0].ID != session.ID || list[0].Cols != 80 || list[0].Rows != 24 {
				t.Fatal("background/recovery recreated or resized the desktop session", err)
			}
			t.Log("PASS: control-only loss recovered into the same unchanged desktop session")
			return
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(fixture), "ios-finished")); err == nil {
			t.Fatal("native runner exited before completing background validation")
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("native background fixture timed out")
}
