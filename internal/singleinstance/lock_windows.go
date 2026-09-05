//go:build windows

package singleinstance

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// lock opens path without share flags for the process lifetime. When
// another process holds it open the CreateFile call fails, which is
// how the second launch recognizes the primary.
func lock(path string) (func(), error) {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode lock path %s: %w", path, err)
	}
	h, err := windows.CreateFile(p16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // no sharing: holding this handle is the whole election
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w (is roamming already running?)", path, err)
	}
	return func() { _ = windows.CloseHandle(h) }, nil
}
