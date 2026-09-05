// Command roamming is a system-tray desktop app that sets your own
// external activity indicator in Roam (https://ro.am) through the
// user.activity API, using only your personal OAuth grant
// (scopes: user:write.activity, user:read.activity).
package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/nathabonfim59/roamming/internal/reopen"
	"github.com/nathabonfim59/roamming/internal/singleinstance"
	"github.com/nathabonfim59/roamming/internal/ui"
)

// appID namespaces Fyne preferences/storage and the tray icon.
const appID = "io.github.nathabonfim59.roamming"

func main() {
	// A second launch must not start a second tray icon (or fight over
	// the OAuth callback port): hand the request to the running
	// instance, which shows its window, and exit.
	primary, activated, err := singleinstance.Acquire(appID)
	if err != nil {
		log.Println("single-instance check:", err)
	}
	if !primary {
		return
	}

	a := app.NewWithID(appID)
	u := ui.New(a)
	u.Show()

	// A second launch replays the tray "Show window" action. fyne.Do
	// marshals onto the UI thread; the queue also survives a signal
	// arriving while a.Run() is still starting up.
	if activated != nil {
		go func() {
			for range activated {
				fyne.Do(u.Activate)
			}
		}()
	}
	// macOS only: re-opening the running bundle (Finder, Spotlight,
	// `open -a`) restores the window; LaunchServices starts no second
	// process, so the singleinstance hand-off never fires there.
	reopen.Listen(func() { fyne.Do(u.Activate) })

	a.Run()
}
