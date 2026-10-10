package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

func connectionTheme(original *ui.Theme) ui.Theme {
	theme := *original
	theme.FontSize, theme.Radius = 16, 12
	if theme.Dark {
		theme.Background = ui.Hex("#111315")
		theme.Surface = ui.Hex("#1e2126")
	} else {
		theme.Background = ui.Hex("#f5f6f8")
		theme.Surface = ui.Hex("#ffffff")
	}
	return theme
}

func connectionStatusCard(c *ui.Context, title, body string, animating bool, actions func()) {
	mobileCard(c.Key("connection-feedback")).Label("连接状态").Padding(12).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if animating {
				mobileReconnectIcon(c, 14, c.Theme().Accent)
			}
			ui.Text(c, title).FontSize(14).Bold().Grow(1)
		})
		ui.Text(c, body).FontSize(13).LineHeight(1.4).TextColor(c.Theme().TextMuted)
		if actions != nil {
			actions()
		}
	})
}

func mobileReconnectIcon(c *ui.Context, size float32, color ui.Color) {
	e := ui.Icon(c, icon("rotate-ccw")).Label("重连进度").Role(ui.RoleProgress).Size(size, size).Shrink(0).TextColor(color).PassThrough()
	e.Rotate(-360 * e.Loop("reconnect", 1100*time.Millisecond, ui.Linear))
}

func mobileCard(c *ui.Context) ui.Element {
	return ui.Column(c).FillWidth().Radius(12).Background(c.Theme().Surface)
}

func mobileListGroup(c *ui.Context) ui.Element {
	return mobileCard(c).Clip()
}

func mobileListSectionHeader(c *ui.Context, title string, action func()) {
	ui.Row(c).FillWidth().Height(44).Padding(0, 4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
		if action != nil {
			action()
		}
	})
}

func mobileIconAction(c *ui.Context, label, glyph string) ui.Element {
	return ui.ButtonBase(c).Label(label).Role(ui.RoleButton).Size(44, 44).Children(func() {
		ui.Icon(c, icon(glyph)).Size(17, 17).TextColor(c.Theme().Accent)
	})
}

func mobileTextAction(c *ui.Context, label, text string) ui.Element {
	return ui.ButtonBase(c).Label(label).Role(ui.RoleButton).MinWidth(44).Height(44).Padding(0, 8).Children(func() {
		ui.Text(c, text).FontSize(14).TextColor(c.Theme().Accent)
	})
}

func mobileListRow(c *ui.Context, key, label string, height float32) ui.Element {
	row := ui.ButtonBase(c.Key(key)).Label(label).Role(ui.RoleButton).FillWidth().Height(height).Padding(0, 12).Gap(12)
	if row.Pressed() {
		row.Background(c.Theme().SurfacePressed)
	}
	return row
}

type mobileListTextOptions struct {
	Title, Subtitle                   string
	TitleSize, SubtitleSize           float32
	TitleAccessory, SubtitleAccessory func()
}

func mobileListText(c *ui.Context, opts mobileListTextOptions) {
	if opts.TitleSize == 0 {
		opts.TitleSize = 15
	}
	if opts.SubtitleSize == 0 {
		opts.SubtitleSize = 12
	}
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, opts.Title).FontSize(opts.TitleSize).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if opts.TitleAccessory != nil {
				opts.TitleAccessory()
			}
		})
		if opts.Subtitle != "" || opts.SubtitleAccessory != nil {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, opts.Subtitle).FontSize(opts.SubtitleSize).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				if opts.SubtitleAccessory != nil {
					opts.SubtitleAccessory()
				}
			})
		}
	})
}

func mobileListIcon(c *ui.Context, name string, color ui.Color) ui.Element {
	return ui.Box(c).Size(32, 32).Shrink(0).Center().Children(func() {
		programIcon(c, name).Size(21, 21).TextColor(color)
	})
}

func mobileListChevron(c *ui.Context) {
	ui.Icon(c, icon("chevron-right")).Size(16, 16).TextColor(colorsOf(c).iconMuted)
}

func mobileListDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Margin(0, 0, 0, 56).Background(colorsOf(c).hover)
}

func mobileListEmpty(c *ui.Context, message string) {
	ui.Text(c, message).FontSize(14).TextColor(c.Theme().TextMuted).Padding(16)
}
