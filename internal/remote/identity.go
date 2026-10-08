package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tailscale/tailcat"
	"golang.org/x/sys/unix"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

type savedIdentity struct {
	Version      int                  `json:"version"`
	Key          key.NodePrivate      `json:"key"`
	PresharedKey tailcat.PresharedKey `json:"preshared_key"`
	Region       *tailcfg.DERPRegion  `json:"region"`
}

func identityPath(dir string) string { return filepath.Join(dir, "remote", "identity.json") }

// IdentityExists also reports damaged state, so it can be surfaced rather than
// silently replacing a previously paired desktop's credentials.
func IdentityExists(dir string) bool {
	_, err := os.Stat(identityPath(dir))
	return !errors.Is(err, os.ErrNotExist)
}

// Hold this lock for the bridge's lifetime: two servers with the same node
// identity would compete for the relay and make both phones unreliable.
func lockIdentity(ctx context.Context, dir string) (*os.File, error) {
	parent := filepath.Dir(identityPath(dir))
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(parent, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(parent, "identity.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func releaseIdentity(f *os.File) {
	if f != nil {
		f.Close() // Releases flock, including on process exit/crash.
	}
}

func loadIdentity(ctx context.Context, dir string) (savedIdentity, *os.File, error) {
	lock, err := lockIdentity(ctx, dir)
	if err != nil {
		return savedIdentity{}, nil, err
	}
	success := false
	defer func() {
		if !success {
			releaseIdentity(lock)
		}
	}()
	f, err := os.Open(identityPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		identity := tailcat.NewPrivateKey()
		success = true
		return savedIdentity{Version: 1, Key: identity.Private, PresharedKey: identity.Public.PresharedKey}, lock, nil
	}
	if err != nil {
		return savedIdentity{}, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return savedIdentity{}, nil, err
	}
	var identity savedIdentity
	if len(data) > 65536 || json.Unmarshal(data, &identity) != nil || identity.Version != 1 || identity.Key.IsZero() || identity.PresharedKey.IsZero() || identity.Region == nil || identity.Region.RegionID <= 0 || len(identity.Region.Nodes) == 0 {
		return savedIdentity{}, nil, errors.New("保存的连接身份无效，请停止连接后重新开启")
	}
	if err := f.Chmod(0600); err != nil {
		return savedIdentity{}, nil, err
	}
	success = true
	return identity, lock, nil
}

// Saving the selected relay as well as both keys keeps the complete QR code
// stable; selecting a different relay would strand clients holding the old one.
func saveIdentity(dir string, server *tailcat.Server) error {
	ci, err := tailcat.ParseAddr(server.TailcatAddr())
	if err != nil || len(ci.Region) != 1 {
		return errors.New("无法保存连接身份")
	}
	data, err := json.Marshal(savedIdentity{Version: 1, Key: server.Key, PresharedKey: ci.PresharedKey, Region: ci.Region[0]})
	if err != nil {
		return errors.New("无法保存连接身份")
	}
	path := identityPath(dir)
	f, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), path)
}

// ForgetIdentity is for explicit revocation only, after closing the bridge.
// Normal application shutdown retains these credentials for the next launch.
func ForgetIdentity(ctx context.Context, dir string) error {
	lock, err := lockIdentity(ctx, dir)
	if err != nil {
		return err
	}
	defer releaseIdentity(lock)
	err = os.Remove(identityPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
