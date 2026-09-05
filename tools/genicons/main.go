// Command genicons renders the Linux desktop icon set from the canonical
// logo (docs/design/favicon.png): hicolor theme sizes plus the legacy
// pixmaps fallback. The output is committed to the repository so the
// nfpm-built packages are reproducible without image tools on CI.
//
// Usage: go run ./tools/genicons
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const src = "docs/design/favicon.png"

// hicolor standard sizes we ship; 128 also goes to the pixmaps fallback.
var sizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

func main() {
	f, err := os.Open(src)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	logo, err := png.Decode(f)
	if err != nil {
		fatal(fmt.Errorf("%s: %w", src, err))
	}

	for _, size := range sizes {
		write(logo, size, filepath.Join(
			"packaging", "linux", "icons", "hicolor",
			fmt.Sprintf("%dx%d", size, size), "apps", "roamming.png"))
	}
	// /usr/share/pixmaps is the legacy fallback some environments still
	// resolve Icon=roamming from; any square size is fine there.
	write(logo, 128, filepath.Join("packaging", "linux", "icons", "pixmaps", "roamming.png"))
}

func write(logo image.Image, size int, path string) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), logo, logo.Bounds(), draw.Over, nil)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fatal(err)
	}
	out, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	if err := png.Encode(out, dst); err != nil {
		fatal(err)
	}
	if err := out.Close(); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", path)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genicons:", err)
	os.Exit(1)
}
