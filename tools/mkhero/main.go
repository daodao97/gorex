// Mkhero composes the README hero image from the existing screenshots.
//
//	GOWORK=off go run ./tools/mkhero
//
// The desktop window, the iPhone home screen and the iPhone terminal tell the
// whole story in one picture: every host and session is one tap away on the
// phone, and the terminal it opens is the same Workspace session as on the Mac.
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"

	xdraw "golang.org/x/image/draw"
)

const (
	width   = 2100
	height  = 1560
	outPath = "docs/images/hero-dark.jpg"

	phoneHeight  = 1340
	bezel        = 20
	phoneOverlap = 90
)

func main() {
	desktop, err := load("docs/images/desktop-workspace-dark.jpg")
	if err != nil {
		log.Fatal(err)
	}
	home, err := load("docs/images/ios-home-dark.png")
	if err != nil {
		log.Fatal(err)
	}
	terminal, err := load("docs/images/ios-terminal-dark.png")
	if err != nil {
		log.Fatal(err)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		t := float64(y) / height
		c := color.RGBA{lerp(0x24, 0x0e, t), lerp(0x2a, 0x10, t), lerp(0x3a, 0x16, t), 0xff}
		for x := range width {
			canvas.SetRGBA(x, y, c)
		}
	}

	// Only the desktop window's left half is kept: the Workspace tab and the
	// pane holding the session the phone terminal shows. The phones overlap
	// it in layers, leaving the session text itself uncovered.
	b := desktop.Bounds()
	desktop = desktop.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(image.Rect(b.Min.X, b.Min.Y, b.Min.X+b.Dx()*51/100, b.Max.Y))
	dh := 1180
	dw := dh * desktop.Bounds().Dx() / desktop.Bounds().Dy()
	dr := image.Rect(110, 110, 110+dw, 110+dh)
	shadow(canvas, dr, 24, 60, 0.55)
	paste(canvas, desktop, dr, 24)

	// Both screenshots share the iPhone aspect ratio. Reading left to right,
	// the home screen leads into the session, each phone a step lower and in
	// front of the previous one; the overlap hides only the list's chevrons.
	pw := phoneHeight * home.Bounds().Dx() / home.Bounds().Dy()
	hx := dr.Min.X + dw*62/100 + bezel
	phone(canvas, home, hx, 60)
	phone(canvas, terminal, hx+pw+2*bezel-phoneOverlap, 140)

	f, err := os.Create(outPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, canvas, &jpeg.Options{Quality: 90}); err != nil {
		log.Fatal(err)
	}
}

// phone draws a simple bezel around a real iOS screenshot whose screen starts
// at (x, y).
func phone(dst *image.RGBA, src image.Image, x, y int) {
	pw := phoneHeight * src.Bounds().Dx() / src.Bounds().Dy()
	pr := image.Rect(x, y, x+pw, y+phoneHeight)
	fr := pr.Inset(-bezel)
	shadow(dst, fr, 96, 70, 0.7)
	fill(dst, fr, 96, color.RGBA{0x3c, 0x40, 0x4a, 0xff})
	fill(dst, fr.Inset(3), 93, color.RGBA{0x05, 0x05, 0x07, 0xff})
	paste(dst, src, pr, 78)
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

// sdf returns the signed distance from (x, y) to a rounded rectangle.
func sdf(r image.Rectangle, radius float64, x, y float64) float64 {
	cx := float64(r.Min.X+r.Max.X) / 2
	cy := float64(r.Min.Y+r.Max.Y) / 2
	hx := float64(r.Dx())/2 - radius
	hy := float64(r.Dy())/2 - radius
	qx := math.Abs(x-cx) - hx
	qy := math.Abs(y-cy) - hy
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	return outside + math.Min(math.Max(qx, qy), 0) - radius
}

func blend(dst *image.RGBA, x, y int, c color.RGBA, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	d := dst.RGBAAt(x, y)
	mix := func(s, d uint8) uint8 { return uint8(float64(s)*a + float64(d)*(1-a)) }
	dst.SetRGBA(x, y, color.RGBA{mix(c.R, d.R), mix(c.G, d.G), mix(c.B, d.B), 0xff})
}

// shadow draws a soft drop shadow below a rounded rectangle.
func shadow(dst *image.RGBA, r image.Rectangle, radius, blur int, strength float64) {
	sr := r.Add(image.Pt(0, blur/3))
	area := sr.Inset(-blur).Intersect(dst.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			d := sdf(sr, float64(radius), float64(x)+0.5, float64(y)+0.5)
			t := 1 - math.Max(d, 0)/float64(blur)
			if t > 0 {
				blend(dst, x, y, color.RGBA{0, 0, 0, 0xff}, strength*t*t)
			}
		}
	}
}

func fill(dst *image.RGBA, r image.Rectangle, radius int, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := sdf(r, float64(radius), float64(x)+0.5, float64(y)+0.5)
			blend(dst, x, y, c, 0.5-d)
		}
	}
}

// paste scales src into r and clips it to rounded corners, which also hides
// the light corners baked into the JPEG window capture.
func paste(dst *image.RGBA, src image.Image, r image.Rectangle, radius int) {
	scaled := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := sdf(r, float64(radius), float64(x)+0.5, float64(y)+0.5)
			blend(dst, x, y, scaled.RGBAAt(x-r.Min.X, y-r.Min.Y), 0.5-d)
		}
	}
}
