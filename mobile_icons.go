package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
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

func mobileSessionIcon(c *ui.Context, name string) *ui.Element {
	prog := programOf(name)
	color := programIconColor(c, prog)
	if prog.Glyph == "square-terminal" {
		prog.Glyph = "terminal"
		color = colorsOf(c).iconMuted
	}
	if name == "" {
		prog.Name = "终端"
	}
	return mobileListIcon(c, prog.Glyph, color).Role(ui.RoleImage).Label(prog.Name + " icon")
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
