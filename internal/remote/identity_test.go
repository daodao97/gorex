package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tailscale.com/tailcfg"
)

func TestIdentityRestoreIsolationAndRevocation(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	first, lock, err := loadIdentity(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseIdentity(lock)
	if first.Key.IsZero() || first.PresharedKey.IsZero() {
		t.Fatal("new identity lacks authenticated keys")
	}
	// This fixture represents a successfully started bridge's selected relay.
	first.Region = &tailcfg.DERPRegion{RegionID: 301, Nodes: []*tailcfg.DERPNode{{Name: "fixture", HostName: "relay.example", IPv4: "192.0.2.1"}}}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identityPath(dir), data, 0644); err != nil {
		t.Fatal(err)
	}
	releaseIdentity(lock)
	second, restoredLock, err := loadIdentity(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseIdentity(restoredLock)
	if first.Key.Raw32() != second.Key.Raw32() || first.PresharedKey != second.PresharedKey || second.Region.Nodes[0].IPv4 != "192.0.2.1" {
		t.Fatal("restart changed credentials or relay")
	}
	for path, want := range map[string]os.FileMode{identityPath(dir): 0600, filepath.Dir(identityPath(dir)): 0700} {
		stat, err := os.Stat(path)
		if err != nil || stat.Mode().Perm() != want {
			t.Fatal("identity state is not private")
		}
	}
	other, otherLock, err := loadIdentity(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	releaseIdentity(otherLock)
	if other.Key.Raw32() == first.Key.Raw32() || other.PresharedKey == first.PresharedKey {
		t.Fatal("independent installations share credentials")
	}
	releaseIdentity(restoredLock)
	if !IdentityExists(dir) {
		t.Fatal("saved identity not detected for auto restore")
	}
	if err := ForgetIdentity(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if IdentityExists(dir) {
		t.Fatal("explicit stop did not revoke saved identity")
	}
	fresh, freshLock, err := loadIdentity(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	releaseIdentity(freshLock)
	if fresh.Key.Raw32() == first.Key.Raw32() || fresh.PresharedKey == first.PresharedKey {
		t.Fatal("revoked credentials were reused")
	}
}

func TestIdentityLockPreventsCompetingServers(t *testing.T) {
	dir := t.TempDir()
	_, lock, err := loadIdentity(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseIdentity(lock)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, _, err := loadIdentity(ctx, dir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("competing bridge acquired the same node identity")
	}
	// Restart waits for the old bridge to finish closing, instead of starting
	// another relay connection while its predecessor is draining TCP.
	done := make(chan error, 1)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	go func() {
		_, next, err := loadIdentity(ctx2, dir)
		releaseIdentity(next)
		done <- err
	}()
	releaseIdentity(lock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDamagedIdentityIsNotSilentlyRotated(t *testing.T) {
	for _, data := range []string{`{`, `{ "version": 99 }`, `{ "version": 1 }`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(identityPath(dir)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(identityPath(dir), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadIdentity(context.Background(), dir); err == nil {
				t.Fatal("damaged saved identity silently replaced")
			}
			got, _ := os.ReadFile(identityPath(dir))
			if string(got) != data {
				t.Fatal("damaged identity overwritten")
			}
			// Failure must release the lock so the UI's explicit stop can reset it.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := ForgetIdentity(ctx, dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}
