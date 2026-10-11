package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMobileStartupWaitsForLocalData(t *testing.T) {
	m := &mobileApp{startup: &ui.Startup{SkipTransition: true}}
	tt := ui.NewTester(m.view, 390, 750)
	if !tt.HasText("Retty 启动画面") || m.navigation != nil {
		t.Fatal("home opened before initial local data loaded")
	}
	m.startupLoaded()
	tt.Frame()
	if tt.HasText("Retty 启动画面") || !tt.HasText("扫码连接桌面") {
		t.Fatal("local data readiness did not hand off to home")
	}
}

func TestMobileStartupLayoutAndSystemTheme(t *testing.T) {
	for _, size := range [][2]int{{320, 568}, {390, 750}, {750, 310}, {768, 1024}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := &mobileApp{startup: &ui.Startup{SkipTransition: true}}
			tt := ui.NewTester(m.view, size[0], size[1])
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				tt.Frame()
				logo, ok := tt.Find("Retty Logo")
				if !ok || logo.X < 0 || logo.Y < 0 || logo.X+logo.W > float32(size[0]) || logo.Y+logo.H > float32(size[1]) {
					t.Fatalf("logo is clipped: %v", logo)
				}
				title, ok := tt.Find("Retty")
				if !ok || title.Y < logo.Y+logo.H || title.Y+title.H > float32(size[1]) {
					t.Fatalf("app name is missing or overlaps the logo: %v", title)
				}
				image := tt.Image()
				pixel := image.RGBAAt(0, 0)
				if dark && pixel.R > 50 || !dark && pixel.R < 230 {
					t.Fatalf("startup background did not follow system theme: dark=%t pixel=%v", dark, pixel)
				}
				green := 0
				for y := int(logo.Y); y < int(logo.Y+logo.H); y++ {
					for x := int(logo.X); x < int(logo.X+logo.W); x++ {
						p := image.RGBAAt(x, y)
						if int(p.G) > int(p.R)+50 && p.G > 150 {
							green++
						}
					}
				}
				if green < 100 {
					t.Fatal("embedded logo did not render")
				}
				if size == [2]int{390, 750} {
					saveSettingsImage(t, tt, fmt.Sprintf("mobile-launch-dark-%t", dark))
				}
			}
		})
	}
}
