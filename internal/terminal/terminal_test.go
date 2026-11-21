package terminal

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

func plain(lines []string) string { return ansi.ReplaceAllString(strings.Join(lines, "\n"), "") }

func start(t *testing.T, script string) *Session {
	t.Helper()
	s, err := Start(exec.Command("/bin/sh", "-c", script), t.TempDir(), 40, 5)
	if errors.Is(err, ErrUnsupported) || runtime.GOOS == "windows" {
		t.Skip("no pseudo terminal here")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func waitFor(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !strings.Contains(plain(s.Lines(false)), want) {
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatalf("screen never showed %q:\n%s", want, plain(s.Lines(false)))
		}
	}
}

func TestSession_ShowsOutputInTheProjectDirectory(t *testing.T) {
	s := start(t, "echo hello-$((20+22)); pwd")
	waitFor(t, s, "hello-42")
	waitFor(t, s, "/")
}

func TestSession_PassesInputAndReportsEnd(t *testing.T) {
	s := start(t, "read word; echo got-$word")
	s.Write([]byte("abc\r"))
	waitFor(t, s, "got-abc")
	deadline := time.After(5 * time.Second)
	for !s.Done() {
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatal("the end of the shell was not reported")
		}
	}
}

func TestSession_ResizeAndCursor(t *testing.T) {
	s := start(t, "sleep 5")
	s.Resize(20, 3)
	lines := s.Lines(true)
	if len(lines) != 3 {
		t.Fatalf("%d lines after resize", len(lines))
	}
}

func TestSession_CloseEndsTheProgramsTheShellStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no process groups")
	}
	pidFile := t.TempDir() + "/pid"
	s := start(t, "sleep 300 & echo $! > "+pidFile+"; wait")
	var pid string
	deadline := time.After(5 * time.Second)
	for pid == "" {
		if data, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(data)) != "" {
			pid = strings.TrimSpace(string(data))
			break
		}
		select {
		case <-deadline:
			t.Fatal("the child never started")
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.Close()
	for {
		if err := exec.Command("kill", "-0", pid).Run(); err != nil {
			return // gone
		}
		select {
		case <-deadline:
			t.Fatalf("the program %s of the closed shell is still running", pid)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Closing right after the start used to race: the emulator's Close wrote a flag that its Read read in the goroutine that forwards
// the answers of the screen. Run with -race. The goroutine of every closed session must end, also when the screen has a reply to
// write while the session is closed (the pipe between the screen and the reader is synchronous).
func TestSession_CloseEndsTheAnswerGoroutineAlsoWithAReplyPending(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo terminal")
	}
	var sessions []*Session
	for i := 0; i < 10; i++ {
		s := start(t, "sleep 30")
		s.Close()
		sessions = append(sessions, s)
	}
	for i := 0; i < 10; i++ {
		s := start(t, `printf '\033[6n\033[6n\033[6n'; sleep 30`) // a cursor position query: the screen answers it
		time.Sleep(30 * time.Millisecond)
		s.Close()
		sessions = append(sessions, s)
	}
	for i, s := range sessions {
		select {
		case <-s.answerDone:
		case <-time.After(8 * time.Second):
			t.Fatalf("the answer goroutine of closed session %d is still running", i)
		}
	}
}

// After the shell has ended nobody reads the pipe of the screen any more: a paste or a resize then must neither freeze the
// caller nor fail.
func TestSession_PasteAndResizeAfterTheShellEndedDoNotBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pseudo terminal")
	}
	s := start(t, "true")
	deadline := time.After(5 * time.Second)
	for !s.Done() {
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatal("the shell never ended")
		}
	}
	finished := make(chan struct{})
	go func() {
		s.Paste("late text")
		s.Resize(30, 7)
		s.Resize(20, 5)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("paste or resize after the end of the shell blocks")
	}
}
