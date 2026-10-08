package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (m *mobileApp) sessionSettingsDialog(c *ui.Context) {
	if m.navigation.Path() != "/sessions" {
		m.sessionSettingsOpen = false
	}
	ui.DialogBase(c, &m.sessionSettingsOpen, func(back, panel *ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, .4)).Column().Justify(ui.End).AlignItems(ui.Center).Padding(12)
		panel.Label("显示与提醒面板").FillWidth().MaxWidth(480).Padding(16).Radius(20).Background(c.Theme().Surface).Column().Gap(12)
		ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "显示与提醒").FontSize(17).Bold().Grow(1)
			if ui.ButtonBase(c).Label("关闭显示与提醒").Role(ui.RoleButton).Size(40, 40).Radius(20).Background(c.Theme().Background).Children(func() {
				ui.Icon(c, icon("x")).Size(16, 16).TextColor(c.Theme().TextMuted)
			}).Clicked() {
				m.sessionSettingsOpen = false
			}
		})
		enabled := !m.pushDisabled
		if mobileSettingSwitch(c, &enabled, "后台任务提醒", "bell", "任务提醒", "完成或需要确认时通知").Changed() {
			m.setPushEnabled(enabled)
		}
		if m.hello.Version >= 6 {
			enabled := m.sizeLock
			if mobileSettingSwitch(c, &enabled, "按手机尺寸显示", "smartphone", "按手机尺寸显示", "离开后恢复桌面尺寸").Changed() {
				m.setSizeLock(enabled)
			}
		}
		if m.pushError != "" && !m.pushDisabled {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, m.pushError).FontSize(12).LineHeight(1.4).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
				if m.notificationDenied && mobileIconAction(c, "打开通知设置", "chevron-right").Clicked() {
					go mygo.Permissions.OpenSettings()
				}
			})
		}
	})
}

func mobileSettingSwitch(c *ui.Context, enabled *bool, label, glyph, title, description string) *ui.Element {
	switchRow := ui.SwitchBase(c, enabled).Label(label).FillWidth().MinHeight(72).Padding(12).Radius(12).Background(c.Theme().Background).Gap(12).AlignItems(ui.Center)
	on := float32(0)
	if *enabled {
		on = 1
	}
	position := switchRow.Animate("toggle", on, 140*time.Millisecond)
	return switchRow.Children(func() {
		color := c.Theme().TextMuted
		if *enabled {
			color = c.Theme().Accent
		}
		ui.Box(c).Size(32, 32).Shrink(0).Radius(10).Background(c.Theme().Surface).Center().Children(func() {
			ui.Icon(c, icon(glyph)).Size(17, 17).TextColor(color)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(15)
			ui.Text(c, description).FontSize(12).LineHeight(1.4).TextColor(c.Theme().TextMuted).FillWidth()
		})
		track, left := c.Theme().Border.Mix(c.Theme().Accent, position), 2+14*position
		ui.Box(c).Size(36, 22).Shrink(0).Radius(11).Background(track).Children(func() {
			ui.Box(c).Absolute().Left(left).Top(2).Size(18, 18).Radius(9).Background(ui.RGB(255, 255, 255))
		})
	})
}
