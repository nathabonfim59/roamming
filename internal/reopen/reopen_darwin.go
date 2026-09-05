//go:build darwin

// Package reopen restores the macOS launch path Fyne leaves unhooked:
// re-opening an already-running app bundle makes LaunchServices send a
// reopen event to the running instance instead of starting a second
// process, and neither GLFW nor Fyne implements the delegate method
// for it — so with the window hidden to the tray (and LSUIElement
// hiding any Dock icon), a second launch did nothing visible. This
// package adds the delegate method to GLFW's application delegate and
// forwards the event to the Go callback registered with Listen.
package reopen

// The Objective-C half of the bridge lives in reopen_darwin.m, which
// the toolchain compiles and links against AppKit. It resolves glfw's
// delegate class by name at runtime, so there is deliberately no cgo
// preamble here: nothing for cgo to paste, nothing for the linker or
// dyld to resolve, on every platform that never builds this file.
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
	C.roamAttachReopen()
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
