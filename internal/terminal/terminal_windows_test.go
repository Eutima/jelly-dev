//go:build windows

package terminal

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

func startWindows(t *testing.T, args ...string) *Session {
	t.Helper()
	s, err := Start(exec.Command("cmd.exe", args...), t.TempDir(), 60, 8)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("this Windows has no ConPTY")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestConPTY_ShowsTheOutputOfAProgramAndNoticesItsEnd(t *testing.T) {
	s := startWindows(t, "/c", "echo hello-jd")
	waitFor(t, s, "hello-jd")
	deadline := time.After(10 * time.Second)
	for !s.Done() {
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatal("the end of the shell was not noticed")
		}
	}
}

func TestConPTY_TakesTypedInputResizesAndCloses(t *testing.T) {
	s := startWindows(t)
	s.Write([]byte("echo typed-in-jd\r"))
	waitFor(t, s, "typed-in-jd")
	s.Resize(80, 12)
	if lines := s.Lines(false); len(lines) != 12 {
		t.Fatalf("the screen must have 12 lines after the resize, has %d", len(lines))
	}
	s.Close()
	deadline := time.After(10 * time.Second)
	for !s.Done() {
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatal("closing the session must end the shell")
		}
	}
}
