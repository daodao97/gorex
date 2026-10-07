package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

// colors are the app's own, in light and dark windows.
type colors struct {
	// The window's background: a gradient from top through mid to bottom,
	// and a tint toward the right.
	bgTop, bgMid, bgLow, bgBottom, bgTint ui.Color

	card, cardFocused, cardBorder, cardBorderFocused ui.Color
	shadow, shadowFocused                            ui.Color
	header, headerFocused, headerBorder              ui.Color

	text, textMuted, textFaint, iconMuted ui.Color
	hover, pressed                        ui.Color

	track, trackBorder, tabActive, tabSep ui.Color
	tileRim                               ui.Color

	busy, attention, exited ui.Color

	panel, panelBorder, panelSel, backdrop ui.Color
}

var lightColors = colors{
	bgTop:    ui.Hex("#f6e7f6"),
	bgMid:    ui.Hex("#efe1e6"),
	bgLow:    ui.Hex("#eee0d3"),
	bgBottom: ui.Hex("#ece1bd"),
	bgTint:   ui.RGBA(242, 214, 222, 0.35),

	card:              ui.RGBA(255, 255, 255, 0.66),
	cardFocused:       ui.RGBA(255, 255, 255, 0.95),
	cardBorder:        ui.RGBA(255, 255, 255, 0.72),
	cardBorderFocused: ui.RGBA(255, 255, 255, 1),
	shadow:            ui.RGBA(60, 40, 50, 0.07),
	shadowFocused:     ui.RGBA(60, 40, 50, 0.12),

	text:      ui.Hex("#1d1d1f"),
	textMuted: ui.Hex("#565960"),
	textFaint: ui.Hex("#60636a"),
	iconMuted: ui.Hex("#737680"),
	hover:     ui.RGBA(0, 0, 0, 0.055),
	pressed:   ui.RGBA(0, 0, 0, 0.1),

	track:       ui.RGBA(255, 255, 255, 0.34),
	trackBorder: ui.RGBA(255, 255, 255, 0.45),
	tabActive:   ui.RGBA(255, 255, 255, 0.97),
	tabSep:      ui.RGBA(0, 0, 0, 0.13),
	tileRim:     ui.RGBA(255, 255, 255, 0.95),

	busy:      ui.Hex("#26773b"),
	attention: ui.Hex("#9a6500"),
	exited:    ui.Hex("#666a73"),

	panel:       ui.RGBA(252, 252, 253, 0.97),
	panelBorder: ui.RGBA(0, 0, 0, 0.08),
	panelSel:    ui.RGBA(46, 111, 208, 0.12),
	backdrop:    ui.RGBA(40, 20, 40, 0.06),
}

var darkColors = colors{
	// Neutral charcoal chrome, inspired by iTerm2's dark appearance.
	bgTop:    ui.Hex("#27292c"),
	bgMid:    ui.Hex("#212326"),
	bgLow:    ui.Hex("#1e2023"),
	bgBottom: ui.Hex("#1b1d20"),
	bgTint:   ui.RGBA(0, 0, 0, 0),

	card:              ui.Hex("#181a1c"),
	cardFocused:       ui.Hex("#111315"),
	cardBorder:        ui.Hex("#303337"),
	cardBorderFocused: ui.Hex("#4a4e54"),
	shadow:            ui.RGBA(0, 0, 0, 0.16),
	shadowFocused:     ui.RGBA(0, 0, 0, 0.28),
	header:            ui.Hex("#202225"),
	headerFocused:     ui.Hex("#25272a"),
	headerBorder:      ui.Hex("#303337"),

	text:      ui.Hex("#ededed"),
	textMuted: ui.Hex("#b6b8bc"),
	textFaint: ui.Hex("#91959c"),
	iconMuted: ui.Hex("#a1a5ac"),
	hover:     ui.RGBA(255, 255, 255, 0.07),
	pressed:   ui.RGBA(255, 255, 255, 0.12),

	track:       ui.Hex("#1c1e21"),
	trackBorder: ui.Hex("#36393e"),
	tabActive:   ui.Hex("#3a3d42"),
	tabSep:      ui.Hex("#41454b"),
	tileRim:     ui.RGBA(255, 255, 255, 0.18),

	busy:      ui.Hex("#4ade80"),
	attention: ui.Hex("#fbbf24"),
	exited:    ui.Hex("#636366"),

	panel:       ui.Hex("#25272b"),
	panelBorder: ui.Hex("#44484f"),
	panelSel:    ui.Hex("#354863"),
	backdrop:    ui.RGBA(0, 0, 0, 0.32),
}

func colorsOf(c *ui.Context) *colors {
	if c.Theme().Dark {
		return &darkColors
	}
	return &lightColors
}

// paintBackground paints the window's background.
func paintBackground(p *ui.Painter, r ui.Rect, k *colors) {
	if prefs.CompactMode {
		bg := lightTerm.Background
		if k == &darkColors {
			bg = darkTerm.Background
		}
		p.Fill(r, bg, 0)
		return
	}
	h1, h2 := r.H*0.38, r.H*0.32
	p.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: h1 + 1}, ui.LinearGradient{From: k.bgTop, To: k.bgMid, Angle: 180}, 0)
	p.FillGradient(ui.Rect{X: r.X, Y: r.Y + h1, W: r.W, H: h2 + 1}, ui.LinearGradient{From: k.bgMid, To: k.bgLow, Angle: 180}, 0)
	p.FillGradient(ui.Rect{X: r.X, Y: r.Y + h1 + h2, W: r.W, H: r.H - h1 - h2}, ui.LinearGradient{From: k.bgLow, To: k.bgBottom, Angle: 180}, 0)
	clear := k.bgTint
	clear.A = 0
	p.FillGradient(r, ui.LinearGradient{From: clear, To: k.bgTint, Angle: 90, Start: 0.35, End: 1}, 0)
}

func terminalBackground(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return darkTerm.Background
	}
	return lightTerm.Background
}

//go:embed assets/fonts/JetBrainsMono-Regular.ttf
var fontRegular []byte

//go:embed assets/fonts/JetBrainsMono-Bold.ttf
var fontBold []byte

//go:embed assets/fonts/JetBrainsMono-Italic.ttf
var fontItalic []byte

//go:embed assets/fonts/JetBrainsMono-BoldItalic.ttf
var fontBoldItalic []byte

func registerFonts() {
	for _, f := range [][]byte{fontRegular, fontBold, fontItalic, fontBoldItalic} {
		ui.RegisterFont(f, "JetBrains Mono")
	}
}

var termFont = terminal.Font{Family: "JetBrains Mono, SF Mono, Menlo, monospace", Size: 11.6, LineHeight: 1.0}

// The terminals' colors: soft ink on the panes' paper, and the dark kind.
var lightTerm = &terminal.Theme{
	Foreground:      ui.Hex("#2a2d31"),
	Background:      ui.Hex("#fbfbfb"),
	Cursor:          ui.Hex("#3a3d42"),
	Selection:       ui.Hex("#d4e4fc"),
	SelectionText:   ui.Hex("#1f2a3d"),
	MinimumContrast: 4.5,
	Palette: [16]ui.Color{
		ui.Hex("#2a2d31"), ui.Hex("#b33449"), ui.Hex("#327343"), ui.Hex("#896014"),
		ui.Hex("#245fb5"), ui.Hex("#7c3daf"), ui.Hex("#176b7a"), ui.Hex("#61656c"),
		ui.Hex("#656971"), ui.Hex("#be354c"), ui.Hex("#28763e"), ui.Hex("#916000"),
		ui.Hex("#2767bc"), ui.Hex("#8544b5"), ui.Hex("#147484"), ui.Hex("#4b4f56"),
	},
}

var darkTerm = &terminal.Theme{
	Foreground:    ui.Hex("#d8d8d8"),
	Background:    darkColors.cardFocused,
	Cursor:        ui.Hex("#e8e8e8"),
	CursorText:    darkColors.cardFocused,
	Selection:     ui.Hex("#385779"),
	SelectionText: ui.Hex("#ffffff"),
	Palette: [16]ui.Color{
		ui.Hex("#1b1d1f"), ui.Hex("#cc6666"), ui.Hex("#b5bd68"), ui.Hex("#f0c674"),
		ui.Hex("#81a2be"), ui.Hex("#b294bb"), ui.Hex("#8abeb7"), ui.Hex("#c5c8c6"),
		ui.Hex("#707880"), ui.Hex("#e88989"), ui.Hex("#c7d28c"), ui.Hex("#f4d58d"),
		ui.Hex("#a1bed6"), ui.Hex("#c9aed3"), ui.Hex("#a5d4ce"), ui.Hex("#ffffff"),
	},
}
