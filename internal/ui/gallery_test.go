package ui

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/nathabonfim59/roamming/internal/roam"
	"github.com/nathabonfim59/roamming/internal/session"
)

// The gallery tests render the UI screens to docs/gallery for the
// README. They are skipped unless ROAMMING_CAPTURE_GALLERY is set, and
// never touch the network or the real token store: the App struct is
// assembled by hand and driven through internal methods, bypassing the
// connect flow entirely.

func galleryDir(t *testing.T) string {
	t.Helper()
	out := filepath.Join("..", "..", "docs", "gallery")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

func capturePNG(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newGalleryApp builds the App the same way New does, minus the parts
// a screenshot cannot use: no tray, no token-store lookup, no
// background tick loop.
func newGalleryApp() *App {
	fa := test.NewApp()
	a := &App{fa: fa, auth: roam.NewAuth()}
	a.win = fa.NewWindow("Roam Activity")
	a.win.Resize(fyne.NewSize(620, 940))
	a.buildConnectScreen()
	a.buildActivityScreen()
	a.stack = container.NewStack(a.connectBox, a.activityBox)
	a.win.SetContent(container.NewScroll(a.stack))
	a.win.Show()
	a.showConnect("") // hide the activity screen behind the stack
	return a
}

func TestCaptureGalleryScreens(t *testing.T) {
	if os.Getenv("ROAMMING_CAPTURE_GALLERY") == "" {
		t.Skip("set ROAMMING_CAPTURE_GALLERY=1 to regenerate docs/gallery")
	}
	out := galleryDir(t)

	a := newGalleryApp()

	// Connect screen: fresh install, nothing filled in (light theme).
	test.ApplyTheme(t, theme.LightTheme())
	capturePNG(t, out, "connect.png", a.win.Canvas().Capture())

	// Activity editor: filled in like a real running session. The test
	// renderer's bundled fonts cannot draw color emoji, so the sample
	// status stays emoji-free (the preset pre-fills one; clear it).
	a.presetSelect.SetSelected("Focusing")
	a.emojiEntry.SetText("")
	a.subtitleEntry.SetText("Shipping the Roam activity app")
	a.dndCheck.SetChecked(true)
	a.timerSelect.SetSelected("1 hour")
	a.accountLabel.SetText("Connected as you @ Roam")
	a.showActivity()

	now := time.Now()
	a.applyState(session.State{
		Phase:           session.PhaseRunning,
		Display:         roam.Display{Title: "Heads-down focus time", Subtitle: "Shipping the Roam activity app"},
		DND:             true,
		StartedAt:       now.Add(-18 * time.Minute),
		ServerExpiresAt: now.Add(50 * time.Minute),
		EndsAt:          now.Add(42 * time.Minute),
	})
	a.liveLabel.SetText("Heads-down focus time · DND (expires 15:52)\nAway from keyboard (expires 15:07)\n")
	capturePNG(t, out, "activity.png", a.win.Canvas().Capture())

	// Same screen in the dark theme.
	test.ApplyTheme(t, theme.DarkTheme())
	capturePNG(t, out, "activity-dark.png", a.win.Canvas().Capture())
}

func TestCaptureGalleryTray(t *testing.T) {
	if os.Getenv("ROAMMING_CAPTURE_GALLERY") == "" {
		t.Skip("set ROAMMING_CAPTURE_GALLERY=1 to regenerate docs/gallery")
	}
	out := galleryDir(t)

	const pad = 10
	states := []trayStatus{trayIdle, trayActive, trayError}
	img := image.NewRGBA(image.Rect(0, 0, len(states)*iconSize+(len(states)+1)*pad, iconSize+2*pad))
	for i, s := range states {
		src, err := png.Decode(bytes.NewReader(trayIcon(s).Content()))
		if err != nil {
			t.Fatal(err)
		}
		x := pad + i*(iconSize+pad)
		draw.Draw(img, image.Rect(x, pad, x+iconSize, pad+iconSize), src, src.Bounds().Min, draw.Src)
	}
	capturePNG(t, out, "tray-states.png", img)
}
