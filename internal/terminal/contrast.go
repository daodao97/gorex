package terminal

import (
	"math"

	"github.com/egoist/mygo/ui"
)

type contrastPair struct{ fg, bg ui.Color }

// Cache color pairs across rows, keeping long sessions with many RGB colors
// bounded. Adjustment happens when building changed rows, never when drawing.
func (v *view) readableForeground(fg, bg ui.Color) ui.Color {
	minimum := v.theme.MinimumContrast
	if v.adaptiveColors {
		minimum = max(minimum, 4.5)
	}
	if minimum <= 1 {
		return fg
	}
	key := contrastPair{fg, bg}
	if c, ok := v.contrast[key]; ok {
		return c
	}
	c := contrastingText(fg, bg, minimum)
	if v.contrast == nil {
		v.contrast = make(map[contrastPair]ui.Color)
	} else if len(v.contrast) >= 512 {
		clear(v.contrast)
	}
	v.contrast[key] = c
	return c
}

// Programs can retain a light RGB panel after the surrounding
// terminal becomes dark, or vice versa. Mirror its offset from the previous
// background onto the current one. Only neutral colors with the opposite
// light/dark polarity qualify; colored panels and matching colors stay put.
func (v *view) adaptiveBackground(bg ui.Color) ui.Color {
	if !v.adaptiveColors || max(bg.R, bg.G, bg.B)-min(bg.R, bg.G, bg.B) > 12 {
		return bg
	}
	current := color(v.colors.Background)
	lb, lc := luminance(bg), luminance(current)
	if !(lb >= .5 && lc < .18 || lb < .18 && lc >= .5) {
		return bg
	}
	previous := v.oppositeBackground
	channel := func(value, from, to uint8) uint8 {
		delta := min(max(int(value)-int(from), -64), 64)
		return uint8(min(max(int(to)-delta, 0), 255))
	}
	return ui.RGB(channel(bg.R, previous.R, current.R), channel(bg.G, previous.G, current.G), channel(bg.B, previous.B, current.B))
}

var linearChannels = func() (values [256]float64) {
	for i := range values {
		c := float64(i) / 255
		if c <= 0.04045 {
			values[i] = c / 12.92
		} else {
			values[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return
}()

func luminance(c ui.Color) float64 {
	return .2126*linearChannels[c.R] + .7152*linearChannels[c.G] + .0722*linearChannels[c.B]
}

func contrastRatio(a, b ui.Color) float64 {
	l1, l2 := luminance(a), luminance(b)
	return (max(l1, l2) + .05) / (min(l1, l2) + .05)
}

// contrastingText preserves sufficient colors, otherwise blending toward
// black or white only as far as necessary. Flatten faint alpha against the
// background first so the contrast check measures the displayed color.
func contrastingText(fg, bg ui.Color, minimum float64) ui.Color {
	visible := fg
	if fg.A < 255 {
		alpha := float64(fg.A) / 255
		channel := func(f, b uint8) uint8 {
			return uint8(math.Round(float64(f)*alpha + float64(b)*(1-alpha)))
		}
		visible = ui.RGB(channel(fg.R, bg.R), channel(fg.G, bg.G), channel(fg.B, bg.B))
	}
	if contrastRatio(visible, bg) >= minimum {
		return fg
	}
	target := ui.RGB(0, 0, 0)
	white := ui.RGB(255, 255, 255)
	if contrastRatio(white, bg) > contrastRatio(target, bg) {
		target = white
	}
	blend := func(amount int) ui.Color {
		channel := func(from, to uint8) uint8 {
			return uint8((int(from)*(256-amount) + int(to)*amount + 128) / 256)
		}
		return ui.RGB(channel(visible.R, target.R), channel(visible.G, target.G), channel(visible.B, target.B))
	}
	lo, hi := 0, 256
	for lo < hi {
		mid := (lo + hi) / 2
		if contrastRatio(blend(mid), bg) >= minimum {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return blend(hi)
}
