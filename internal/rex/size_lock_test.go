//go:build darwin || linux

package rex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInactiveSizeOwnerCannotUnlockNewAttachment(t *testing.T) {
	s := &session{sizeLock: "new-attachment", sizeLockDevice: "iPhone"}
	s.unlockSizeOwned("old-attachment", 0, 0)
	if s.sizeLock != "new-attachment" || s.sizeLockDevice != "iPhone" {
		t.Fatal("old attachment released the new owner's lock")
	}
	s.unlockSizeOwned("new-attachment", 0, 0)
	if s.sizeLock != "" || s.sizeLockDevice != "" {
		t.Fatal("active owner could not release its lock")
	}
}

// A phone holding the size lock owns the one PTY size: windows' resizes are
// ignored, its own resizes apply, and only an unlock gives the size back.
func TestSizeLockOwnsPTYUntilUnlocked(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "rex")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("RETTY_DIR", dir)
	go Serve()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "server.sock")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()
	info, err := c.Create(CreateOptions{Command: []string{"/bin/sh"}, Dir: "/tmp", Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	size := func() SessionInfo {
		t.Helper()
		list, err := c.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.ID == info.ID {
				return s
			}
		}
		t.Fatal("session missing")
		return SessionInfo{}
	}
	wait := func(cols, rows int, lock string) SessionInfo {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			s := size()
			if s.Cols == cols && s.Rows == rows && s.SizeLock == lock {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("session %dx%d lock %q, want %dx%d lock %q", s.Cols, s.Rows, s.SizeLock, cols, rows, lock)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	window := c.Stream(info.ID, 100, 30)
	defer window.Close()
	phone := c.LockStream(info.ID, "phone-run", "Test iPhone", 48, 35)
	defer phone.Close()
	phone.Resize(48, 35)
	if s := wait(48, 35, "phone-run"); s.SizeLockDevice != "Test iPhone" {
		t.Fatalf("lock device %q", s.SizeLockDevice)
	}
	window.Resize(120, 40)
	time.Sleep(3 * resizeDelay)
	wait(48, 35, "phone-run")
	phone.Resize(50, 30)
	wait(50, 30, "phone-run")
	if err := c.UnlockSize(info.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	wait(50, 30, "")
	// The released phone resizes nothing; the window takes the size back.
	phone.Resize(40, 20)
	time.Sleep(3 * resizeDelay)
	wait(50, 30, "")
	window.Resize(110, 32)
	wait(110, 32, "")
}
