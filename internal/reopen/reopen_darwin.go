//go:build darwin

// Package reopen restores the macOS launch path Fyne leaves unhooked:
// re-opening an already-running app bundle makes LaunchServices send a
// reopen event to the running instance instead of starting a second
// process, and neither GLFW nor Fyne implements the delegate method
// for it — so with the window hidden to the tray (and LSUIElement
// hiding any Dock icon), a second launch did nothing visible. This
// package adds the missing delegate method as an Objective-C category
// on GLFW's own application delegate (see reopen_darwin.m) and
// forwards the event to the Go callback registered with Listen.
package reopen

/*
The Objective-C half of this bridge lives in reopen_darwin.m; the Go
toolchain compiles that file and links it against AppKit. This
preamble stays free of definitions because the file uses //export.

The -undefined dynamic_lookup below lets the category reference
GLFW's delegate class even when glfw is not part of the link
(standalone `go test ./internal/reopen`): the reference resolves via
dynamic lookup instead of failing the link. The real app always links
glfw, so there the class resolves normally; and if it were ever
renamed upstream, the runtime would simply drop the category — the
app keeps working, just without the reopen restore.

#cgo LDFLAGS: -Wl,-undefined,dynamic_lookup
*/
import "C"

import "sync"

var (
	mu   sync.Mutex
	next func() // the Listen handler; may be nil
)

// Listen sets fn as the handler for macOS reopen events. It runs on
// the main thread; marshal onto the Fyne thread with fyne.Do. Pass
// nil to unregister.
func Listen(fn func()) {
	mu.Lock()
	next = fn
	mu.Unlock()
}

// roammingReopenActivated is called by reopen_darwin.m whenever the
// running instance is re-opened (Finder, Spotlight, `open -a …`).
//
//export roammingReopenActivated
func roammingReopenActivated() {
	mu.Lock()
	fn := next
	mu.Unlock()
	if fn != nil {
		fn()
	}
}
