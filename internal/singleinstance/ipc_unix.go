//go:build unix

package singleinstance

import (
	"net"
	"os"
	"time"
)

// listen serves the activate socket; a connection means "show your
// window". A leftover socket from a crashed run is removed first.
func listen(path string) (<-chan struct{}, error) {
	_ = os.Remove(path) // stale socket from a crashed run
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	activated := make(chan struct{}, 1)
	go func() {
		defer ln.Close()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close() // the connect itself is the signal
			select {
			case activated <- struct{}{}:
			default: // coalesce: collapse to one pending show, keep it
			}
		}
	}()
	return activated, nil
}

// notify asks the primary instance to show its window. The dial is the
// message; a few retries cover a primary that is still starting up.
func notify(path string) error {
	var err error
	for range 5 {
		var c net.Conn
		c, err = net.DialTimeout("unix", path, time.Second)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return err
}
