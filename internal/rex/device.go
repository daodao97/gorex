package rex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func deviceFingerprint(identity string) string {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ""
	}
	// Publish an app-specific digest instead of exposing the hardware UUID.
	digest := sha256.Sum256([]byte("gorex-device:" + identity))
	return "machine:" + hex.EncodeToString(digest[:16])
}

// Legacy servers predate the hardware fingerprint. Both remote viewers and
// the local notification worker derive the same non-secret identity from the
// hello, without using a QR capability or restarting live sessions. Exclude OS
// versions and memory, which can change while this server keeps running.
func legacyHostID(host HostInfo) string {
	if strings.TrimSpace(host.Name) == "" {
		return ""
	}
	key := strings.Join([]string{host.Name, host.User, host.Home, host.Model, host.Chip}, "\x00")
	digest := sha256.Sum256([]byte("gorex-legacy-host:" + key))
	return "legacy:" + hex.EncodeToString(digest[:16])
}

// This public installation ID survives server restarts and QR rotations.
// It is separate from the credentials that grant access to sessions.
func loadDeviceID(dir string) (string, error) {
	path := filepath.Join(dir, "device-id")
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if decoded, err := hex.DecodeString(id); err == nil && len(decoded) == 16 {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(data[:])
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", err
	}
	return id, nil
}
