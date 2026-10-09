//go:build darwin || linux

package rex

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSizeLeaseExpiresAndCannotBeRevived(t *testing.T) {
	s := &session{}
	s.lockSize("phone", "iPhone", 0, 0, nil, true)
	t.Cleanup(func() { s.unlockSize(0, 0) })
	if s.renewSizeLock("another-phone") {
		t.Fatal("another device renewed the lock")
	}
	// Simulate the server deadline passing while the phone and its TCP
	// connection remain suspended; no unlock or FIN is sent.
	s.mu.Lock()
	s.sizeLockUntil = time.Now().Add(30 * time.Millisecond)
	s.sizeLockTimer.Reset(30 * time.Millisecond)
	s.mu.Unlock()
	waitSizeLease(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.sizeLock == "" && s.sizeLockDevice == "" && s.sizeLockTimer == nil
	})
	if s.renewSizeLock("phone") || s.lockedResize("phone", 40, 20) {
		t.Fatal("a late heartbeat or resize revived the expired lock")
	}
}

func TestSizeLeaseRenewalProtectsNewDeadlineAndReplacement(t *testing.T) {
	s := &session{}
	oldConn, oldPeer := net.Pipe()
	newConn, newPeer := net.Pipe()
	t.Cleanup(func() { s.unlockSize(0, 0); oldConn.Close(); oldPeer.Close(); newConn.Close(); newPeer.Close() })
	s.lockSize("phone", "old phone", 0, 0, oldConn, true)
	s.mu.Lock()
	oldDeadline := time.Now()
	s.sizeLockUntil = oldDeadline.Add(time.Second)
	s.mu.Unlock()
	if !s.renewSizeLock("phone") {
		t.Fatal("active device could not renew")
	}
	s.mu.Lock()
	s.expireSizeLockLocked(oldDeadline.Add(2 * time.Second))
	owner := s.sizeLock
	s.mu.Unlock()
	if owner != "phone" {
		t.Fatal("an old timer callback cleared the renewed lock")
	}
	s.lockSize("phone", "new phone", 0, 0, newConn, true)
	s.releaseSizeAttachment("phone", oldConn)
	s.mu.Lock()
	owner, device := s.sizeLock, s.sizeLockDevice
	s.mu.Unlock()
	if owner != "phone" || device != "new phone" {
		t.Fatal("an old connection cleared the replacement's lock")
	}
	s.unlockSizeOwned("phone", 0, 0)
	if s.renewSizeLock("phone") {
		t.Fatal("heartbeat reclaimed a manually unlocked session")
	}
	s.lockSize("legacy", "old client", 0, 0, oldConn, false)
	if !s.sizeLockUntil.IsZero() || s.renewSizeLock("legacy") {
		t.Fatal("lease was imposed on a client that never negotiated it")
	}
}

func TestSizeLeaseStreamDetachAndMissingReleaseKeepShell(t *testing.T) {
	ss, client := sizeLeaseTestSession(t)
	original := ss.info()
	phone := client.LockStream(ss.id, "first-phone", "iPhone", 48, 35)
	phone.attach()
	t.Cleanup(func() { phone.Close() })
	if holds, _ := phone.HoldsLock(); !holds || ss.info().SizeLock != "first-phone" {
		t.Fatal("fixture did not acquire its lock")
	}
	// Plain Close sends no explicit unlock on the control channel.
	phone.Close()
	waitSizeLease(t, func() bool { return ss.info().SizeLock == "" })
	phone = client.LockStream(ss.id, "second-phone", "iPhone", 48, 35)
	phone.attach()
	phone.mu.Lock()
	if phone.leaseCancel == nil {
		phone.mu.Unlock()
		t.Fatal("server did not negotiate the lease")
	}
	phone.leaseCancel() // iOS suspension stops work, but keeps the socket open.
	phone.mu.Unlock()
	ss.mu.Lock()
	ss.sizeLockUntil = time.Now().Add(30 * time.Millisecond)
	ss.sizeLockTimer.Reset(30 * time.Millisecond)
	ss.mu.Unlock()
	waitSizeLease(t, func() bool { return ss.info().SizeLock == "" })
	if err := client.Resize(ss.id, original.Cols, original.Rows); err != nil {
		t.Fatal(err)
	}
	current := ss.info()
	if current.PID != original.PID || current.Exited || current.Cols != original.Cols || current.Rows != original.Rows {
		t.Fatalf("lease expiration lost the shell or prevented desktop resize: %+v", current)
	}
}

func TestSizeLeaseQuietViewRenewsWithoutResizing(t *testing.T) {
	ss, client := sizeLeaseTestSession(t)
	phone := client.LockStream(ss.id, "phone", "iPhone", 48, 35)
	phone.attach()
	t.Cleanup(func() { phone.Close() })
	// Speed up the renewal interval and first expiry without waiting 30s.
	phone.mu.Lock()
	phone.leaseCancel()
	ctx, cancel := context.WithCancel(context.Background())
	phone.leaseCancel = cancel
	phone.mu.Unlock()
	ss.mu.Lock()
	oldDeadline := time.Now().Add(time.Second)
	ss.sizeLockUntil = oldDeadline
	ss.sizeLockTimer.Reset(time.Second)
	resizeAt := ss.resizeAt
	ss.mu.Unlock()
	done := make(chan struct{})
	go func() { phone.renewSizeLock(ctx, 10*time.Millisecond); close(done) }()
	waitSizeLease(t, func() bool {
		ss.mu.Lock()
		defer ss.mu.Unlock()
		return ss.sizeLockUntil.After(oldDeadline)
	})
	ss.mu.Lock()
	unchanged := ss.cols == 48 && ss.rows == 35 && ss.resizeAt.Equal(resizeAt)
	ss.mu.Unlock()
	if !unchanged {
		t.Fatal("renewing a quiet session triggered a resize")
	}
	if err := client.UnlockSize(ss.id, 0, 0); err != nil {
		t.Fatal(err)
	}
	waitSizeLease(t, func() bool { holds, _ := phone.HoldsLock(); return !holds })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("renewal continued after desktop unlock")
	}
}

func sizeLeaseTestSession(t *testing.T) (*session, *Client) {
	t.Helper()
	t.Setenv("RETTY_DIR", t.TempDir())
	ss, err := newSession("lease-fixture", CreateOptions{Command: []string{"/bin/sh"}, Dir: t.TempDir(), Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ss.kill)
	server := &Server{sessions: map[string]*session{ss.id: ss}, order: []string{ss.id}}
	client, err := ConnectDial(context.Background(), func(context.Context) (net.Conn, error) {
		local, peer := net.Pipe()
		go server.handle(peer)
		return local, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return ss, client
}

func waitSizeLease(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("size lease transition timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
