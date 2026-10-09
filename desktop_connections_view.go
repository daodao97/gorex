package main

import (
	"github.com/egoist/mygo/ui"
	"slices"
	"strings"
)

func (a *App) desktopConnectionsSettings(c *ui.Context, k *colors) {
	ui.Text(c, "连接此电脑").FontSize(14).FontWeight(600).TextColor(k.text)
	ui.Row(c).FillWidth().Padding(16, 0).Gap(18).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, "允许其他设备连接").FontSize(13).FontWeight(500).TextColor(k.text)
			ui.Text(c, "手机扫码，其他桌面粘贴连接码。").FontSize(12).TextColor(k.textMuted)
		})
		if ui.Button(c, "显示二维码").Label("Show phone connection QR code").Clicked() {
			a.openPhonePair()
		}
	})
	ui.Box(c).FillWidth().Height(1).Background(k.panelBorder).Margin(18, 0, 24, 0)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "连接其他桌面").FontSize(14).FontWeight(600).TextColor(k.text).Grow(1)
		if iconButton(c, k, "plus", "添加桌面连接", 24, 14).Clicked() {
			a.showDesktopConnections(nil)
		}
	})
	ui.Text(c, "打开远端会话，或在远端电脑上新建终端。").FontSize(12).TextColor(k.textMuted).Margin(8, 0, 18, 0)
	a.desktopConnectionInput(c, k, false)
	a.desktopHostRows(c, k)
}
func (a *App) desktopConnectionInput(c *ui.Context, k *colors, focus bool) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c, &a.desktops.input).Grow(1).MinWidth(0).Height(34).FontSize(12).Placeholder("粘贴 gorex://connect 连接码").Label("桌面连接码")
		if focus && a.desktops.initialFocus {
			in.Focus()
		}
		if in.Submitted() {
			a.submitDesktopConnection()
		}
		if iconButton(c, k, "clipboard-paste", "粘贴桌面连接码", 28, 15).Tooltip("从剪贴板粘贴").Clicked() {
			a.desktops.input = c.ReadClipboard()
			in.Focus()
		}
		if ui.PrimaryButton(c, "连接").Height(32).Disabled(strings.TrimSpace(a.desktops.input) == "").Label("连接其他桌面").Clicked() {
			a.submitDesktopConnection()
		}
	})
	if a.desktops.err != "" {
		ui.Text(c, a.desktops.err).FontSize(12).TextColor(k.attention).Margin(8, 0)
	}
}
func (a *App) desktopHostRows(c *ui.Context, k *colors) {
	if len(a.desktops.hosts) == 0 {
		ui.Row(c).FillWidth().Padding(24, 0).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("monitor")).Size(18, 18).TextColor(k.iconMuted)
			ui.Text(c, "连接过的电脑会保留在这里").FontSize(12).TextColor(k.textFaint)
		})
		return
	}
	ui.Column(c).FillWidth().Margin(14, 0).Children(func() {
		for _, h := range slices.Clone(a.desktops.hosts) {
			ui.Row(c.Key("desktop-"+h.key)).FillWidth().Padding(12, 0).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder).Children(func() {
				ui.Box(c).Size(32, 32).Center().Radius(8).Background(k.hover).Children(func() { ui.Icon(c, icon("monitor")).Size(17, 17).TextColor(k.iconMuted) })
				b := ui.ButtonBase(c).Grow(1).MinWidth(0).Label("查看电脑 " + h.name()).Justify(ui.Start)
				b.Children(func() {
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, h.name()).FontSize(13).FontWeight(500).TextColor(k.text).SingleLine().Ellipsis("…")
						status := "未连接"
						if h.busy {
							status = "连接中…"
						} else if h.connected() {
							status = "已连接"
						} else if h.err != "" {
							status = "连接已断开"
						}
						ui.Text(c, status).FontSize(11).TextColor(k.textMuted)
					})
				})
				if b.Clicked() {
					a.showDesktopConnections(h)
					if !h.connected() {
						a.connectDesktop(h)
					}
				}
				if h.busy {
					ui.Spinner(c).Size(14, 14)
				} else if h.connected() {
					if iconButton(c, k, "arrow-up-right", "打开电脑 "+h.name(), 26, 14).Clicked() {
						a.showDesktopConnections(h)
					}
					if iconButton(c, k, "unplug", "断开电脑 "+h.name(), 26, 14).Tooltip("断开连接，保留远端会话").Clicked() {
						a.later(c, func() { a.disconnectDesktop(h) })
					}
				} else {
					if iconButton(c, k, "rotate-ccw", "重连电脑 "+h.name(), 26, 14).Clicked() {
						a.desktops.selected = h
						a.connectDesktop(h)
					}
					if iconButton(c, k, "x", "移除电脑 "+h.name(), 26, 13).Clicked() {
						a.later(c, func() {
							a.disconnectDesktop(h)
							a.desktops.hosts = slices.DeleteFunc(a.desktops.hosts, func(v *desktopHost) bool { return v == h })
							if a.desktops.selected == h {
								a.desktops.selected = nil
							}
							a.saveDesktopHistory()
						})
					}
				}
			})
		}
	})
}
func (a *App) desktopConnectionDialog(c *ui.Context, k *colors) {
	if !a.desktops.open {
		return
	}
	ui.DialogBase(c, &a.desktops.open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop)
		panel.Width(520).MaxWidthPercent(94).MaxHeightPercent(88).Padding(24).Radius(12).Background(k.panel).Border(1, k.panelBorder).Gap(18).Label("桌面连接")
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("monitor")).Size(19, 19).TextColor(k.iconMuted)
			ui.Text(c, "桌面连接").FontSize(17).FontWeight(600).TextColor(k.text).Grow(1)
			if iconButton(c, k, "x", "关闭桌面连接", 24, 14).Clicked() {
				a.finishDesktopDialog()
			}
		})
		a.desktopConnectionInput(c, k, true)
		ui.Scroll(c.Key("desktop-browser")).FillWidth().MinHeight(80).MaxHeight(400).Children(func() {
			a.desktopHostRows(c, k)
			if h := a.desktops.selected; h != nil {
				ui.Row(c).FillWidth().Margin(12, 0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, h.name()).FontSize(13).FontWeight(600).TextColor(k.text).Grow(1)
					if h.creating {
						ui.Spinner(c).Size(14, 14)
					}
				})
				if h.err != "" {
					ui.Text(c, h.err).FontSize(12).TextColor(k.attention).Margin(0, 0, 10, 0)
				}
				if h.connected() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c, &a.desktops.directory).Grow(1).MinWidth(0).Height(32).FontSize(12).Placeholder(h.hello.Host.Home).Label("远端工作目录")
						if ui.Button(c, "新建会话").Height(32).Disabled(h.creating).Label("新建远端会话").Clicked() {
							a.createDesktopSession(h, strings.TrimSpace(a.desktops.directory), nil, false)
						}
					})
					ui.Text(c, "已有会话").FontSize(11).TextColor(k.textFaint).Margin(18, 0, 8, 0)
					if len(h.sessions) == 0 {
						ui.Text(c, "还没有运行中的会话").FontSize(12).TextColor(k.textMuted).Margin(8, 0)
					}
					for _, in := range h.sessions {
						p := &Pane{info: in, startDir: in.Dir}
						name, _ := p.label()
						row := ui.ButtonBase(c.Key("remote-session-"+h.key+in.ID)).FillWidth().Padding(11, 10).Gap(10).Radius(6).AlignItems(ui.Center).Justify(ui.Start).Label("打开远端会话 " + in.ID)
						if row.Hovered() {
							row.Background(k.hover)
						}
						row.Children(func() {
							prog := paneProgram(p)
							programIcon(c, prog.Glyph).Size(18, 18).Shrink(0).TextColor(programIconColor(c, prog))
							ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
								ui.Text(c, name).FontSize(13).TextColor(k.text).SingleLine().Ellipsis("…")
								ui.Text(c, remoteShortDir(in.Dir, h.hello.Host.Home)).FontSize(11).TextColor(k.textFaint).SingleLine().Ellipsis("…")
							})
							ui.Icon(c, icon("chevron-right")).Size(13, 13).TextColor(k.iconMuted)
						})
						if row.Clicked() {
							a.later(c, func() { a.openDesktopSession(h, in, true) })
						}
					}
				}
			}
		})
		ui.Text(c, "会话运行在对应电脑上，断开连接后仍会继续。").FontSize(11).TextColor(k.textFaint)
	})
	a.desktops.initialFocus = false
	if !a.desktops.open {
		a.finishDesktopDialog()
	}
}
func remoteShortDir(dir, home string) string {
	if home != "" {
		if dir == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(dir, home+"/"); ok {
			return "~/" + rest
		}
	}
	return dir
}
func (a *App) desktopOfflineBar(c *ui.Context, k *colors, h *desktopHost) {
	ui.Row(c).FillWidth().Height(32).Padding(0, 10).Gap(8).AlignItems(ui.Center).Background(k.panel).Children(func() {
		ui.Icon(c, icon("monitor")).Size(13, 13).TextColor(k.iconMuted)
		ui.Text(c, h.name()+" · 连接已断开，远端会话继续运行").FontSize(11).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").TextColor(k.textMuted)
		if h.busy {
			ui.Spinner(c).Size(14, 14)
		} else if ui.Button(c, "重连").Label("重连远端电脑").Clicked() {
			if h.connected() {
				h.err = "会话连接已中断。"
			}
			a.connectDesktop(h)
		}
	})
}
