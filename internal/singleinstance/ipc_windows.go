//go:build windows

package singleinstance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// listen serves the activate named pipe; a client connection means
// "show your window". This is a minimal hand-rolled slice of what a
// named-pipe library would provide: stdlib net has no named-pipe
// support on Windows, and the hand-off only needs "a connect
// happened" — never a data connection.
func listen(path string) (<-chan struct{}, error) {
	name, err := pipeName(path)
	if err != nil {
		return nil, err
	}
	p16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	// Inbound-only: clients just dial. FIRST_PIPE_INSTANCE fails
	// loudly when another desktop session already owns the name
	// instead of silently serving (or racing) that instance's pipe.
	h, err := windows.CreateNamedPipe(p16,
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT,
		1, 4096, 4096, 0, nil)
	if err != nil {
		return nil, err
	}
	activated := make(chan struct{})
	go servePipe(h, activated)
	return activated, nil
}

// servePipe accepts clients one at a time until the process exits;
// every connection signals the primary to show its window. The handle
// is created with PIPE_WAIT, so ConnectNamedPipe blocks this goroutine
// until the next second launch dials.
func servePipe(h windows.Handle, activated chan<- struct{}) {
	defer windows.CloseHandle(h)
	for {
		// ERROR_PIPE_CONNECTED means a client connected between the
		// CreateNamedPipe above and this call — still a connection.
		err := windows.ConnectNamedPipe(h, nil)
		if err != nil && err != windows.ERROR_PIPE_CONNECTED {
			return
		}
		select {
		case activated <- struct{}{}:
		default: // coalesce: one show is enough
		}
		if err := windows.DisconnectNamedPipe(h); err != nil {
			return
		}
	}
}

// notify dials the primary's pipe; the dial is the message. A few
// retries cover a primary that is still starting up (pipe not yet
// created) and the instant while it serves the previous connection
// (ERROR_PIPE_BUSY with one pipe instance).
func notify(path string) error {
	name, err := pipeName(path)
	if err != nil {
		return err
	}
	p16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	var last error
	for range 5 {
		h, err := windows.CreateFile(p16, windows.GENERIC_WRITE,
			0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			_ = windows.CloseHandle(h)
			return nil
		}
		last = err
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("reach primary pipe %s: %w", name, last)
}

// pipeName maps the per-user activate path to a named-pipe address.
// Pipe names share one machine-wide namespace, so the path digest
// keeps simultaneous desktop sessions out of each other's way while
// the base keeps the name recognizable in pipe listings.
func pipeName(path string) (string, error) {
	sum := sha256.Sum256([]byte(path))
	return `\\.\pipe\` + filepath.Base(path) + "-" + hex.EncodeToString(sum[:4]), nil
}
