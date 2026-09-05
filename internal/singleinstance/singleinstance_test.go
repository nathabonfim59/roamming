//go:build unix || windows

package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireElectsSinglePrimary pins the contract: the first Acquire
// wins and receives activate signals; a second Acquire (what a second
// launch does) reports not-primary after notifying the primary.
func TestAcquireElectsSinglePrimary(t *testing.T) {
	// Short, pid-unique name: macOS unix sockets cap sun_path at 104
	// bytes, and a long temp dir plus a long base name eats the rest.
	id := fmt.Sprintf("singleinstance.test.%d", os.Getpid())
	dir := runtimeDir()
	uid := os.Getuid()
	t.Cleanup(func() {
		// Acquire suffixed the names with the uid; mirror that here.
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%s.%d.lock", id, uid)))
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%s.%d.sock", id, uid)))
	})

	primary, activated, err := Acquire(id)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !primary {
		t.Fatal("first Acquire reported not-primary")
	}

	second, _, err := Acquire(id)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second {
		t.Fatal("second Acquire reported primary")
	}

	select {
	case _, ok := <-activated:
		if !ok {
			t.Fatal("activated channel closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("primary was not notified of the second launch")
	}
}
