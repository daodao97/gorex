package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
	"gorex/internal/terminal"
)

// colors are the app's own, in light and dark windows.
type colors struct {
	cardBorderFocused ui.Color
	shadowFocused     ui.Color
	headerBorder      ui.Color

	text, textMuted, textFaint, iconMuted ui.Color
	hover, pressed                        ui.Color

	track ui.Color

	busy, attention ui.Color

	panel, panelBorder, panelSel, backdrop ui.Color
}

var lightColors = colors{
	cardBorderFocused: ui.RGBA(255, 255, 255, 1),
	shadowFocused:     ui.RGBA(60, 40, 50, 0.12),

	text:      ui.Hex("#1d1d1f"),
	textMuted: ui.Hex("#565960"),
	textFaint: ui.Hex("#60636a"),
	iconMuted: ui.Hex("#737680"),
	hover:     ui.RGBA(0, 0, 0, 0.055),
	pressed:   ui.RGBA(0, 0, 0, 0.1),

	track: ui.RGBA(255, 255, 255, 0.34),

	busy:      ui.Hex("#26773b"),
	attention: ui.Hex("#9a6500"),

	panel:       ui.RGBA(252, 252, 253, 0.97),
	panelBorder: ui.RGBA(0, 0, 0, 0.08),
	panelSel:    ui.RGBA(46, 111, 208, 0.12),
	backdrop:    ui.RGBA(40, 20, 40, 0.06),
}

var darkColors = colors{
	// Neutral charcoal chrome, inspired by iTerm2's dark appearance.
	cardBorderFocused: ui.Hex("#4a4e54"),
	shadowFocused:     ui.RGBA(0, 0, 0, 0.28),
	headerBorder:      ui.Hex("#303337"),

	text:      ui.Hex("#ededed"),
	textMuted: ui.Hex("#b6b8bc"),
	textFaint: ui.Hex("#91959c"),
	iconMuted: ui.Hex("#a1a5ac"),
	hover:     ui.RGBA(255, 255, 255, 0.07),
	pressed:   ui.RGBA(255, 255, 255, 0.12),

	track: ui.Hex("#1c1e21"),

	busy:      ui.Hex("#4ade80"),
	attention: ui.Hex("#fbbf24"),

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
	Background:    ui.Hex("#111315"),
	Cursor:        ui.Hex("#e8e8e8"),
	CursorText:    ui.Hex("#111315"),
	Selection:     ui.Hex("#385779"),
	SelectionText: ui.Hex("#ffffff"),
	Palette: [16]ui.Color{
		ui.Hex("#1b1d1f"), ui.Hex("#cc6666"), ui.Hex("#b5bd68"), ui.Hex("#f0c674"),
		ui.Hex("#81a2be"), ui.Hex("#b294bb"), ui.Hex("#8abeb7"), ui.Hex("#c5c8c6"),
		ui.Hex("#707880"), ui.Hex("#e88989"), ui.Hex("#c7d28c"), ui.Hex("#f4d58d"),
		ui.Hex("#a1bed6"), ui.Hex("#c9aed3"), ui.Hex("#a5d4ce"), ui.Hex("#ffffff"),
	},
}
