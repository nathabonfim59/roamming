//go:build !darwin

package reopen

// Listen does nothing off macOS; reopen events are a Cocoa concept.
// (Second launches there go through the singleinstance hand-off.)
func Listen(func()) {}
