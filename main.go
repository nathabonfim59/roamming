// Command roamming is a system-tray desktop app that sets your own
// external activity indicator in Roam (https://ro.am) through the
// user.activity API, using only your personal OAuth grant
// (scopes: user:write.activity, user:read.activity).
package main

import (
	"fyne.io/fyne/v2/app"

	"github.com/nathabonfim59/roamming/internal/ui"
)

// appID namespaces Fyne preferences/storage and the tray icon.
const appID = "io.github.nathabonfim59.roamming"

func main() {
	a := app.NewWithID(appID)
	u := ui.New(a)
	u.Show()
	a.Run()
}
