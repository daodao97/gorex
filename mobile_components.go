package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

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
	TitleAccessory, SubtitleAccessory func()
}

func mobileListText(c *ui.Context, opts mobileListTextOptions) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, opts.Title).FontSize(15).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if opts.TitleAccessory != nil {
				opts.TitleAccessory()
			}
		})
		if opts.Subtitle != "" || opts.SubtitleAccessory != nil {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, opts.Subtitle).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
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
