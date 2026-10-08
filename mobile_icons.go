package main

import (
	"strings"

	"gorex/internal/rex"
)

// Linux hosts report a distribution's PRETTY_NAME instead of "linux".
func desktopPlatform(host rex.HostInfo) string {
	os := strings.ToLower(strings.TrimSpace(host.OS))
	switch {
	case strings.HasPrefix(os, "macos"), strings.HasPrefix(os, "mac os"), os == "darwin", strings.HasPrefix(os, "os x"):
		return "darwin"
	case strings.HasPrefix(os, "windows"):
		return "windows"
	case strings.Contains(os, "linux"), strings.EqualFold(host.Model, "Linux"):
		return "linux"
	default:
		return ""
	}
}

func desktopPlatformProgram(platform string) program {
	switch platform {
	case "darwin":
		return program{Name: "macOS", Glyph: "platform:apple"}
	case "windows":
		return program{Name: "Windows", Glyph: "platform:windows"}
	case "linux":
		return program{Name: "Linux", Glyph: "platform:linux"}
	default:
		return program{Name: "桌面", Glyph: "monitor"}
	}
}
