// Package singleinstance keeps the app to one running instance per
// user: a second launch (desktop icon, xdg-open, a second autostart at
// login) asks the running instance to show its window and exits
// instead of starting a second tray icon. Fyne has no built-in support
// for this, so the primary is elected with a lock file held for the
// process lifetime and the hand-off rides a local unix socket (Linux,
// macOS, the BSDs) or a named pipe (Windows).
package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
)

// Acquire elects the primary instance. When it returns true the caller
// may continue to start the UI; activated receives a value whenever a
// second launch asks the running instance to come forward. When it
// returns false another instance is already running: Acquire told it to
// show its window and the caller should exit without touching the UI.
//
// err is non-nil when the election could not be carried out as designed
// (lock unreadable, or the primary could not be notified); callers
// typically log it and exit. A primary without a working activate
// socket still reports true: the single-instance decision stands and
// the focus hand-off is best effort.
func Acquire(appID string) (primary bool, activated <-chan struct{}, err error) {
	dir := runtimeDir()
	key := fmt.Sprintf("%s.%d", appID, os.Getuid())

	release, lerr := lock(filepath.Join(dir, key+".lock"))
	if lerr != nil {
		// Almost certainly the primary holds the lock: ask it to show.
		return false, nil, notify(filepath.Join(dir, key+".sock"))
	}
	// The lock lives in the file descriptor; keep it reachable for the
	// process lifetime so a GC finalizer cannot close the file early.
	keepAlive = release

	ch, lerr := listen(filepath.Join(dir, key+".sock"))
	if lerr != nil {
		return true, nil, nil
	}
	return true, ch, nil
}

// keepAlive pins the lock for the process lifetime.
var keepAlive func()

// runtimeDir is a per-user directory for the lock and the socket.
// Desktop Linux and the BSDs provide XDG_RUNTIME_DIR; the fallback
// temp dir is shared between users, hence the uid suffix on names.
func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}
