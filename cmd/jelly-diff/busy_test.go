package main

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer lets the animation goroutine and the test read and write safely.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func TestRunBusy_DrawsNothing_WhenOperationIsQuick(t *testing.T) {
	var out lockedBuffer
	err := runBusy(&out, "working", time.Hour, time.Millisecond, func() error { return nil })
	if err != nil || out.String() != "" {
		t.Fatalf("err %v output %q", err, out.String())
	}
}

func TestRunBusy_DrawsFramesAndClearsLine_WhenOperationIsSlow(t *testing.T) {
	var out lockedBuffer
	err := runBusy(&out, "working", time.Millisecond, time.Millisecond, func() error {
		// Block until at least two frames were drawn, so the test does not depend on timing.
		deadline := time.Now().Add(5 * time.Second)
		for strings.Count(out.String(), "working") < 2 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return nil
	})
	text := out.String()
	if err != nil || strings.Count(text, "working") < 2 || !strings.HasSuffix(text, clearLine) {
		t.Fatalf("err %v output %q", err, text)
	}
}

func TestRunBusy_ReturnsError_WhenOperationFails(t *testing.T) {
	boom := errors.New("boom")
	var out lockedBuffer
	if err := runBusy(&out, "working", time.Hour, time.Millisecond, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

func TestNewBusy_RunsWithoutAnimation_WhenDisabled(t *testing.T) {
	var out lockedBuffer
	called := false
	err := newBusy(&out, false)("x", func() error { called = true; return nil })
	if err != nil || !called || out.String() != "" {
		t.Fatalf("err %v called %v output %q", err, called, out.String())
	}
}
