package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (m *mobileApp) connectedDevice() desktopRecent {
	if device, ok := m.recentDesktop(m.desktopKey()); ok {
		if m.hello.Host.Name != "" {
			device.Name = m.hello.Host.Name
		}
		return device
	}
	return desktopRecent{ID: m.hello.Host.ID, Link: m.link, Name: m.hello.Host.Name, OS: desktopPlatform(m.hello.Host)}
}

func (m *mobileApp) setDeviceName(name string) {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name))
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
	}
	device := m.connectedDevice()
	if device.ID == "" && device.Link == "" {
		return
	}
	device.Alias = name
	if m.deviceNameTouched == nil {
		m.deviceNameTouched = map[string]bool{}
	}
	for _, key := range []string{device.ID, device.Link} {
		if key != "" {
			m.deviceNameTouched[key] = true
		}
	}
	for i, entry := range m.history {
		if entry.Link == device.Link || sameDesktop(entry, device) {
			m.history[i].Alias = name
		}
	}
	m.history = mergeDesktopHistory([]desktopRecent{device}, m.history)
	m.persistDesktopHistory()
	m.invalidate()
}

func (m *mobileApp) deviceNameDialog(c *ui.Context) {
	if !m.deviceNameOpen {
		return
	}
	dialog := ui.DialogBase(c.Key("mobile-device-name"), &m.deviceNameOpen, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, .4)).Column().Justify(ui.End).AlignItems(ui.Center).Padding(12)
		panel.Label("设备名称面板").FillWidth().MaxWidth(480).Padding(16).Radius(20).Background(c.Theme().Surface).Column().Gap(12)
		ui.Row(c).FillWidth().Height(44).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "设备名称").FontSize(17).Bold().Grow(1)
			if ui.ButtonBase(c).Label("取消设备重命名").Role(ui.RoleButton).Size(44, 44).Radius(22).Background(c.Theme().Background).Children(func() {
				ui.Icon(c, icon("x")).Size(16, 16).TextColor(c.Theme().TextMuted)
			}).Clicked() {
				m.deviceNameOpen = false
			}
		})
		ui.Column(c).FillWidth().Padding(10, 12).Radius(12).Background(c.Theme().Background).Children(func() {
			ui.Text(c, "留空使用设备原名").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.TextInputBase(c, &m.deviceNameDraft).Label("连接设备名称").Placeholder(m.hello.Host.Name).FontSize(16).FillWidth().Height(40).
				InputOptions(ui.InputOptions{Return: ui.ReturnDone, Correction: ui.CorrectionOff, Capitalization: ui.CapitalizeNone})
		})
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "仅在这台手机上显示").FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
			if ui.ButtonBase(c).Label("保存设备名称").Role(ui.RoleButton).Size(72, 44).Radius(12).Background(c.Theme().Accent).Children(func() {
				ui.Icon(c, icon("check")).Size(19, 19).TextColor(c.Theme().AccentText)
			}).Clicked() {
				m.setDeviceName(m.deviceNameDraft)
				m.deviceNameOpen = false
			}
		})
	})
	if dialog.Dismissed() || !m.deviceNameOpen {
		m.sessionSettingsOpen = true
		c.Blur()
	}
}
