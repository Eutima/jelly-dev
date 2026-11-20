package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/eutima/jelly-diff/internal/ui"
)

const (
	busyDelay = 300 * time.Millisecond // quick operations never show the animation
	clearLine = "\r\x1b[K"
)

// busyFunc runs fn and shows a waiting animation while it takes long.
type busyFunc func(message string, fn func() error) error

// noBusy runs fn without any animation.
func noBusy(_ string, fn func() error) error { return fn() }

// newBusy returns a busyFunc that draws the jelly wave on w, or a plain one when w is not a usable terminal.
func newBusy(w io.Writer, enabled bool) busyFunc {
	if !enabled {
		return noBusy
	}
	return func(message string, fn func() error) error {
		return runBusy(w, message, busyDelay, ui.JellyInterval, fn)
	}
}

// runBusy runs fn; if it takes longer than delay, a jelly wave with message is drawn on w until it ends.
func runBusy(w io.Writer, message string, delay, interval time.Duration, fn func() error) error {
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for frame := 0; ; frame++ {
			fmt.Fprintf(w, "\r%s %s", ui.JellyLine(frame), message)
			select {
			case <-done:
				fmt.Fprint(w, clearLine)
				return
			case <-ticker.C:
			}
		}
	}()
	err := fn()
	close(done)
	<-finished
	return err
}

// stderrIsTerminal reports whether stderr is an interactive terminal that understands escape sequences.
func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	term := os.Getenv("TERM")
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && term != "" && term != "dumb"
}
