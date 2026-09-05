package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"fyne.io/fyne/v2"
)

// The tray and picker icons are drawn at runtime (no binary assets): a
// dark rounded tile with a status dot for the system tray, and filled
// circles for the palette swatches.

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

var trayDotColor = map[trayStatus]color.RGBA{
	trayIdle:   {0x9C, 0xA3, 0xAF, 0xFF}, // gray
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
	bg := color.RGBA{0x1F, 0x29, 0x37, 0xFF} // dark slate tile
	dot := trayDotColor[status]

	cornerR := iconSize / 4
	dotR := iconSize / 10
	cx, cy := float64(iconSize)/2, float64(iconSize)/2

	for y := range iconSize {
		for x := range iconSize {
			// 2x2 supersampling for smoother edges.
			var r, g, b, a float64
			for sy := range 2 {
				for sx := range 2 {
					px := float64(x) + 0.25 + float64(sx)*0.5
					py := float64(y) + 0.25 + float64(sy)*0.5
					c := trayPixel(px, py, bg, dot, cx, cy, dotR, cornerR)
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a += float64(c.A)
				}
			}
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(r / 4), G: uint8(g / 4), B: uint8(b / 4), A: uint8(a / 4),
			})
		}
	}
	return pngResource("tray-"+status.String()+".png", img)
}

// trayPixel picks a pixel color: transparent outside the rounded tile,
// the dot color inside the centered dot, tile color otherwise.
func trayPixel(px, py float64, bg, dot color.RGBA, cx, cy float64, dotR, cornerR int) color.RGBA {
	if !inRoundedRect(px, py, cornerR) {
		return color.RGBA{}
	}
	dx, dy := px-cx, py-cy
	if dx*dx+dy*dy <= float64(dotR)*float64(dotR) {
		return dot
	}
	return bg
}

func inRoundedRect(px, py float64, r int) bool {
	s := float64(iconSize)
	if px < 0 || py < 0 || px >= s || py >= s {
		return false
	}
	rf := float64(r)
	// Inside the straight edges there is no corner constraint.
	if px >= rf && px <= s-rf || py >= rf && py <= s-rf {
		return true
	}
	// Corner zones: distance to the nearest corner center.
	cxr := clampToCorner(px, rf, s-rf)
	cyr := clampToCorner(py, rf, s-rf)
	dx, dy := px-cxr, py-cyr
	return dx*dx+dy*dy <= rf*rf
}

func clampToCorner(v, lo, hi float64) float64 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
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
