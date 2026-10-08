//go:build !ios

package mobile

import (
	"errors"
	"gorex/internal/rex"
)

func Scan(done func(string, error)) {
	done("", errors.New("请在 iPhone 上扫码，或粘贴桌面连接码"))
}
func HideKeyboard() {}

func DeviceInfo() rex.DeviceInfo { return rex.DeviceInfo{Name: "iPhone"} }
