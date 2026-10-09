// Mkicon exports the approved Retty artwork for the native app bundles.
//
//	GOWORK=off go run ./tools/mkicon
//
// macOS gets a 1024px canvas with an inset squircle and transparent margins;
// iOS gets a full-bleed opaque 1024px image for the system's corner mask.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

const (
	size       = 1024
	sourcePath = "assets/branding/retty-source.png"
)

func main() {
	if err := exportIcons(); err != nil {
		log.Fatal(err)
	}
}

func exportIcons() error {
	f, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	if src.Bounds().Dx() != src.Bounds().Dy() {
		return fmt.Errorf("approved artwork must be square: %v", src.Bounds())
	}

	// Flatten defensively: App Store/iOS icons must have no transparent pixels.
	ios := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(ios, ios.Bounds(), image.NewUniform(color.RGBA{0x10, 0x2b, 0x2d, 255}), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(ios, ios.Bounds(), src, src.Bounds(), draw.Over, nil)
	if err := writePNG("resources/ios/app-icon.png", ios); err != nil {
		return err
	}

	// Keep the same artwork and proportions inside the native macOS icon grid.
	const inset, width = 100, 824
	tile := image.Rect(inset, inset, inset+width, inset+width)
	art := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(art, tile, ios, ios.Bounds(), draw.Src, nil)
	mask := image.NewAlpha(art.Bounds())
	squircle(inset, inset, width).Draw(mask, mask.Bounds(), image.NewUniform(color.White), image.Point{})
	mac := image.NewNRGBA(art.Bounds())
	draw.DrawMask(mac, mac.Bounds(), art, image.Point{}, mask, image.Point{}, draw.Src)
	return writePNG("resources/icon.png", mac)
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// A superellipse preserves continuous corners instead of a circular radius.
func squircle(x, y, width float32) *vector.Rasterizer {
	r := vector.NewRasterizer(size, size)
	cx, cy, a := float64(x+width/2), float64(y+width/2), float64(width/2)
	for i := 0; i <= 720; i++ {
		t := float64(i) / 720 * 2 * math.Pi
		c, s := math.Cos(t), math.Sin(t)
		px := cx + a*math.Copysign(math.Pow(math.Abs(c), 0.4), c)
		py := cy + a*math.Copysign(math.Pow(math.Abs(s), 0.4), s)
		if i == 0 {
			r.MoveTo(float32(px), float32(py))
		} else {
			r.LineTo(float32(px), float32(py))
		}
	}
	r.ClosePath()
	return r
}
