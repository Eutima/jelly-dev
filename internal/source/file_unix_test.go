//go:build unix

package source

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadTextFile_RejectsWithoutBlocking_WhenPathIsFifo(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo not available: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadTextFile(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadTextFile blocked on a FIFO")
	}
}
