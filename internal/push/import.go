package push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/egoist/mygo/push/apns"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Import validates a downloaded signing key before storing it. macOS uses the
// login keychain; other desktop hosts use a mode-0600 file in the private app
// directory. The mobile bundle and repository never contain provider secrets.
func Import(dir, keyFile, keyID, teamID, environment string) error {
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return errors.New("无法读取 APNs 密钥")
	}
	cfg := Config{KeyID: keyID, TeamID: teamID, Topic: "dev.gorex.app", Environment: environment}
	if _, err = apns.New(apns.Config{KeyID: keyID, TeamID: teamID, PrivateKey: key, Environment: apns.Environment(environment)}); err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		cfg.KeychainService = "dev.gorex.app.apns"
		// Interactive security input avoids exposing the private key in argv/ps.
		secret := base64.StdEncoding.EncodeToString(key)
		input := "add-generic-password -U -s '" + cfg.KeychainService + "' -a '" + keyID + "' -w '" + secret + "'\n"
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "security", "-i")
		cmd.Stdin = bytes.NewBufferString(input)
		if err = cmd.Run(); err != nil {
			return errors.New("无法将 APNs 密钥保存到钥匙串")
		}
		// security's interactive mode may succeed at process level after a failed command.
		stored, err := readKeychain(cfg.KeychainService, keyID)
		decoded, _ := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(stored)))
		if err != nil || !bytes.Equal(decoded, key) {
			return errors.New("APNs 钥匙串保存验证失败")
		}
	} else {
		cfg.KeyFile = filepath.Join(dir, "apns.p8")
		if err = os.WriteFile(cfg.KeyFile, key, 0600); err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	f, err := os.CreateTemp(dir, ".push-config-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "push-config.json"))
}
