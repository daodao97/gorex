package push

import (
	"errors"
	"github.com/egoist/mygo/push/apns"
	"os"
)

// Import retains the existing GoRex command for compatibility. New projects
// use `mygo push setup`; provider configuration and key storage belong to MyGo.
func Import(dir, keyFile, keyID, teamID, environment string) error {
	key, err := os.ReadFile(keyFile)
	if err != nil {
		return errors.New("无法读取 APNs 密钥")
	}
	return apns.ImportProvider(dir, apns.ProviderConfig{
		KeyID: keyID, TeamID: teamID, Topic: "dev.gorex.app", Environment: apns.Environment(environment),
	}, key)
}
