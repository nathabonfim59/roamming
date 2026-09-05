package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"fyne.io/fyne/v2"
	"golang.org/x/image/draw"
)

// The tray icon is the app logo (docs/design/favicon.png, embedded here
// as appicon.png) with a small status badge; the palette swatches are
// still drawn at runtime.

//go:embed appicon.png
var appIconPNG []byte

// appLogo is the decoded logo; an empty tile if decoding ever fails.
var appLogo = func() image.Image {
	img, err := png.Decode(bytes.NewReader(appIconPNG))
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	}
	return img
}()

// AppIcon is the app logo resource for the window/taskbar icon.
func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("appicon.png", appIconPNG)
}

const iconSize = 64

type trayStatus int

const (
	trayIdle trayStatus = iota
	trayActive
	trayError
)

func (t trayStatus) String() string {
	switch t {
	case trayActive:
		return "active"
	case trayError:
		return "error"
	default:
		return "idle"
	}
}

// trayBadgeColor is the status badge drawn over the logo; idle shows
// the plain logo without a badge.
var trayBadgeColor = map[trayStatus]color.RGBA{
	trayActive: {0x22, 0xC5, 0x5E, 0xFF}, // green
	trayError:  {0xEF, 0x44, 0x44, 0xFF}, // red
}

// trayCache memoizes drawn tray icons; only touched from the UI thread.
var trayCache = map[trayStatus]fyne.Resource{}

// trayIcon returns the system tray resource for a status.
func trayIcon(status trayStatus) fyne.Resource {
	if res, ok := trayCache[status]; ok {
		return res
	}
	res := drawTray(status)
	trayCache[status] = res
	return res
}

func drawTray(status trayStatus) fyne.Resource {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.CatmullRom.Scale(img, img.Bounds(), appLogo, appLogo.Bounds(), draw.Over, nil)
	if dot, ok := trayBadgeColor[status]; ok {
		drawBadge(img, dot)
	}
	return pngResource("tray-"+status.String()+".png", img)
}

// drawBadge paints a status dot with a dark rim over the lower-right
// corner of the logo (2x2 supersampled for smooth edges).
func drawBadge(img *image.RGBA, dot color.RGBA) {
	s := float64(iconSize)
	cx, cy := 0.78*s, 0.78*s
	dotR := 0.14 * s
	rimW := 0.05 * s
	rim := color.RGBA{0x11, 0x18, 0x27, 0xFF} // dark slate rim

	lo, hi := int(cx-dotR-rimW)-1, int(cx+dotR+rimW)+1
	for y := max(lo, 0); y <= min(hi, iconSize-1); y++ {
		for x := max(lo, 0); x <= min(hi, iconSize-1); x++ {
			// Accumulate premultiplied samples, then source-over.
			var r, g, b, a float64
			for sy := range 2 {
				for sx := range 2 {
					px := float64(x) + 0.25 + float64(sx)*0.5
					py := float64(y) + 0.25 + float64(sy)*0.5
					c, alpha := badgeSample(px, py, cx, cy, dotR, rimW, dot, rim)
					r += float64(c.R) * alpha
					g += float64(c.G) * alpha
					b += float64(c.B) * alpha
					a += alpha
				}
			}
			r, g, b, a = r/4, g/4, b/4, a/4
			if a == 0 {
				continue
			}
			dst := img.RGBAAt(x, y)
			outA := a + (1-a)*float64(dst.A)/255
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((r+float64(dst.R)*(1-a))/outA + 0.5),
				G: uint8((g+float64(dst.G)*(1-a))/outA + 0.5),
				B: uint8((b+float64(dst.B)*(1-a))/outA + 0.5),
				A: uint8(outA*255 + 0.5),
			})
		}
	}
}

// badgeSample classifies one subpixel: the dot color inside the dot,
// the rim color inside the surrounding ring, transparent elsewhere.
func badgeSample(px, py, cx, cy, dotR, rimW float64, dot, rim color.RGBA) (color.RGBA, float64) {
	dx, dy := px-cx, py-cy
	d2 := dx*dx + dy*dy
	switch {
	case d2 <= dotR*dotR:
		return dot, 1
	case d2 <= (dotR+rimW)*(dotR+rimW):
		return rim, 1
	default:
		return color.RGBA{}, 0
	}
}

// swatchIcon draws a filled circle for a palette color, with a dark ring
// marking the selected swatch.
func swatchIcon(c color.RGBA, selected bool) fyne.Resource {
	size := 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := float64(size)/2, float64(size)/2
	outer := float64(size)/2 - 1
	inner := outer - 3
	ring := color.RGBA{0x22, 0x22, 0x22, 0xFF}

	for y := range size {
		for x := range size {
			dx, dy := float64(x)-cx+0.5, float64(y)-cy+0.5
			d := dx*dx + dy*dy
			var col color.RGBA
			switch {
			case d <= inner*inner:
				col = c
			case selected && d <= outer*outer:
				col = ring
			}
			img.SetRGBA(x, y, col)
		}
	}
	name := fmt.Sprintf("swatch-%02x%02x%02x", c.R, c.G, c.B)
	if selected {
		name += "-sel"
	}
	return pngResource(name+".png", img)
}

// noneSwatchIcon draws a hollow gray ring for "no color" (quiet badge).
func noneSwatchIcon(selected bool) fyne.Resource {
	size := 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := float64(size)/2, float64(size)/2
	outer := float64(size)/2 - 1
	inner := outer - 3
	ring := color.RGBA{0x9C, 0xA3, 0xAF, 0xFF} // gray outline
	var sel color.RGBA

	for y := range size {
		for x := range size {
			dx, dy := float64(x)-cx+0.5, float64(y)-cy+0.5
			d := dx*dx + dy*dy
			var col color.RGBA
			switch {
			case selected && d <= inner*inner:
				col = sel
			case d <= outer*outer:
				col = ring
			}
			img.SetRGBA(x, y, col)
		}
	}
	name := "swatch-none"
	if selected {
		name += "-sel"
	}
	return pngResource(name+".png", img)
}

func pngResource(name string, img image.Image) fyne.Resource {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fyne.NewStaticResource("empty.png", nil)
	}
	return fyne.NewStaticResource(name, buf.Bytes())
}
