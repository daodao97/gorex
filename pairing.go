package main

import (
	"context"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/skip2/go-qrcode"
	"gorex/internal/push"
	"gorex/internal/remote"
	"gorex/internal/rex"
)

type phonePair struct {
	open, busy    bool
	revoking      bool
	generation    int
	cancel        context.CancelFunc
	bridge        *remote.Bridge
	link, message string
	qr            [][]bool
}

func (a *App) openPhonePair() {
	a.startPhonePair(true)
}

func (a *App) restorePhonePair() {
	if remote.IdentityExists(rex.Dir()) {
		a.startPhonePair(false)
	}
}

func (a *App) startPhonePair(show bool) {
	if a.phone == nil {
		a.phone = &phonePair{}
	}
	p := a.phone
	p.open = p.open || show
	if p.bridge != nil || p.busy || p.revoking {
		return
	}
	p.busy, p.message = true, "正在开启手机连接…"
	p.generation++
	generation := p.generation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancel = cancel
	go func() {
		// Notification registration shares the authenticated control tunnel and
		// runs off the forwarding goroutine. Repeated receipts are idempotent.
		registrations := make(chan push.Registration, 32)
		stopPush := make(chan struct{})
		go func() {
			for {
				select {
				case <-stopPush:
					return
				case r := <-registrations:
					if r.Validate() == nil {
						push.Register(rex.Dir(), r)
					}
				}
			}
		}()
		b, err := remote.Start(ctx, rex.SocketPath(), remote.Options{StateDir: rex.Dir(), OnDevice: func(info rex.DeviceInfo) {
			if info.Push != nil {
				select {
				case registrations <- push.Registration(*info.Push):
				default:
				}
			}
		}, OnPasteImage: pasteRemoteImage})
		if err != nil {
			close(stopPush)
		} else {
			b.OnClose(func() { close(stopPush) })
		}
		cancel()
		mygo.RunOnMain(func() {
			if p.generation != generation {
				if b != nil {
					go b.Close()
				}
				return
			}
			p.busy, p.cancel = false, nil
			if err != nil {
				p.message = "无法开启连接，请检查网络后重试"
			} else {
				p.bridge, p.link, p.message = b, b.Link(), ""
				code, err := qrcode.New(p.link, qrcode.Medium)
				if err == nil {
					p.qr = code.Bitmap()
				}
			}
			if a.win != nil {
				a.win.Invalidate()
			}
		})
	}()
}

func (a *App) stopPhonePair() {
	p := a.phone
	if p == nil {
		return
	}
	p.generation++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if p.bridge != nil {
		b := p.bridge
		p.bridge = nil
		go b.Close()
	}
	p.link, p.qr, p.message, p.busy = "", nil, "", false
	p.open = false
}

// Explicit stop revokes saved credentials. App/window shutdown only closes the
// bridge, so the next launch can serve exactly the same capability.
func (a *App) revokePhonePair() {
	a.stopPhonePair()
	p := a.phone
	if p == nil || p.revoking {
		return
	}
	p.revoking = true
	dir := rex.Dir()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		err := remote.ForgetIdentity(ctx, dir)
		mygo.RunOnMain(func() {
			p.revoking = false
			if err != nil {
				p.open, p.message = true, "无法停止连接，请重试"
			}
			if a.win != nil {
				a.win.Invalidate()
			}
		})
	}()
}

func (a *App) phonePairButton(c *ui.Context, k *colors, size float32) {
	if a.phone == nil || a.phone.bridge == nil || len(a.phone.bridge.Devices()) == 0 {
		return
	}
	label := "手机已连接"
	connectedColors := *k
	connectedColors.iconMuted = k.busy.Mix(k.iconMuted, 0.45)
	connectedColors.text = connectedColors.iconMuted
	k = &connectedColors
	b := iconButton(c, k, "smartphone", label, size, 17).Tooltip(label)
	if b.Clicked() {
		a.openPhonePair()
	}
}

func (a *App) phonePairDialog(c *ui.Context, k *colors) {
	p := a.phone
	if p == nil || !p.open {
		return
	}
	ui.DialogBase(c, &p.open, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .35))
		panel.Width(360).MaxWidthPercent(95).Padding(24).Radius(20).Background(k.panel).Gap(18).Label("连接手机")
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "连接手机").FontSize(22).Bold().Grow(1)
			if iconButton(c, k, "x", "关闭连接二维码", 30, 16).Clicked() {
				p.open = false
			}
		})
		ui.Text(c, "在 iPhone 的 GoRex 中点“扫码连接”，继续桌面会话或新建终端。").FontSize(14).LineHeight(1.5).TextColor(k.textMuted)
		if p.bridge != nil {
			for _, d := range p.bridge.Devices() {
				ui.Column(c).FillWidth().Gap(4).Padding(12).Radius(10).Background(k.hover).Children(func() {
					ui.Text(c, "已连接 · "+d.Name).FontSize(14).Bold()
					detail := d.Connected.Format("15:04") + " 连接"
					if d.OS != "" {
						detail = d.OS + " · " + detail
					}
					ui.Text(c, detail).FontSize(12).TextColor(k.textMuted)
				})
			}
		}
		if len(p.qr) > 0 {
			ui.Box(c).Key("phone-qr").Label("手机连接二维码").Size(288, 288).Background(ui.Hex("#ffffff")).Draw(func(painter *ui.Painter, r ui.Rect) { paintQR(painter, r, p.qr) })
			ui.Text(c, "二维码包含会话访问凭据，请仅用自己的设备扫描。").FontSize(12).TextColor(k.textMuted).LineHeight(1.4)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "复制连接码").Height(44).Grow(1).Clicked() {
					c.WriteClipboard(p.link)
				}
				if ui.Button(c, "停止连接").Height(44).Grow(1).Clicked() {
					a.revokePhonePair()
					p.open = false
				}
			})
		} else {
			ui.Text(c, p.message).FontSize(14).TextColor(k.textMuted)
			if !p.busy && !p.revoking && ui.PrimaryButton(c, "重试").Height(44).FillWidth().Clicked() {
				a.openPhonePair()
			}
			if !p.busy && !p.revoking && remote.IdentityExists(rex.Dir()) && ui.Button(c, "停止连接").Height(44).FillWidth().Clicked() {
				a.revokePhonePair()
			}
			if p.busy && ui.Button(c, "取消").Height(44).FillWidth().Clicked() {
				a.stopPhonePair()
				p.open = false
			}
		}
	})
}

func paintQR(p *ui.Painter, r ui.Rect, code [][]bool) {
	if len(code) == 0 {
		return
	}
	p.Fill(r, ui.Hex("#ffffff"), 0)
	size := float32(math.Floor(float64(min(r.W, r.H)*p.Scale()/float32(len(code))))) / p.Scale()
	x, y := r.X+(r.W-size*float32(len(code)))/2, r.Y+(r.H-size*float32(len(code)))/2
	for row, cells := range code {
		for col, on := range cells {
			if on {
				p.Fill(ui.Rect{X: x + float32(col)*size, Y: y + float32(row)*size, W: size, H: size}, ui.Hex("#000000"), 0)
			}
		}
	}
}
