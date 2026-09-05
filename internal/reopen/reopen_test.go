package reopen

import "testing"

// TestListenRegistersAndClears keeps the package (and with it the
// darwin cgo + Objective-C bridge) compiled and loaded on every
// platform's CI run, including macOS.
func TestListenRegistersAndClears(t *testing.T) {
	Listen(func() {})
	Listen(nil)
}
