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

func TestSession_ScrollsBackIntoTheHistoryAndBackToTheLiveScreen(t *testing.T) {
	s := start(t, "i=1; while [ $i -le 60 ]; do echo line-$i; i=$((i+1)); done; sleep 30")
	waitFor(t, s, "line-60")
	if offset, history := s.Scrolled(); offset != 0 || history < 50 {
		t.Fatalf("the live screen, and the lines that left it are kept: offset %d history %d", offset, history)
	}
	if strings.Contains(plain(s.Lines(false)), "line-1\n") {
		t.Fatal("the first lines are above the screen")
	}
	if got := s.ScrollBy(30); got != 30 {
		t.Fatalf("scrolled back 30 lines: %d", got)
	}
	view := plain(s.Lines(true))
	if strings.Contains(view, "line-60") || !strings.Contains(view, "line-") {
		t.Fatalf("the view shows older lines, not the live ones:\n%s", view)
	}
	cells := s.Cells()
	if len(cells) != 5 || strings.TrimSpace(strings.Join(cells[0], "")) == "" {
		t.Fatalf("the cells (the selection works on them) follow the view: %q", cells[0])
	}
	_, history := s.Scrolled()
	if got := s.ScrollBy(10000); got != history {
		t.Fatalf("scrolling stops at the oldest line: %d of %d", got, history)
	}
	if view := plain(s.Lines(false)); !strings.Contains(view, "line-1") {
		t.Fatalf("the oldest lines are there:\n%s", view)
	}
	s.ScrollBy(-10000)
	if offset, _ := s.Scrolled(); offset != 0 {
		t.Fatalf("back at the live screen: %d", offset)
	}
	s.ScrollBy(20)
	s.ScrollToLive()
	if offset, _ := s.Scrolled(); offset != 0 || !strings.Contains(plain(s.Lines(false)), "line-60") {
		t.Fatalf("ScrollToLive shows the live screen: %d", offset)
	}
}

func TestSession_AViewInTheHistoryStaysWhereItIsWhileNewLinesCome(t *testing.T) {
	s := start(t, "i=1; while [ $i -le 40 ]; do echo line-$i; i=$((i+1)); done; read go; i=41; while [ $i -le 60 ]; do echo line-$i; i=$((i+1)); done; sleep 30")
	waitFor(t, s, "line-40")
	s.ScrollBy(10)
	_, historyBefore := s.Scrolled()
	shown := plain(s.Lines(false))
	s.Write([]byte("\r")) // the shell goes on: twenty more lines
	deadline := time.After(5 * time.Second)
	for {
		if _, history := s.Scrolled(); history >= historyBefore+15 {
			break
		}
		select {
		case <-s.Updates():
		case <-deadline:
			t.Fatal("the shell printed no more lines")
		}
	}
	offset, history := s.Scrolled()
	if offset < 10+15 || offset > history {
		t.Fatalf("new lines push the view further back, so that it stays on the same lines: offset %d history %d", offset, history)
	}
	if now := plain(s.Lines(false)); now != shown {
		t.Fatalf("the view must not move:\n%s\nwas\n%s", now, shown)
	}
}

func TestSession_AProgramOnTheAlternateScreenHasNoHistoryAndGetsTheWheelAsArrows(t *testing.T) {
	// the program switches to the alternate screen and asks for the application cursor keys (vim does), then prints what it reads
	script := `printf '\033[?1049h\033[?1h'; echo alt-screen; stty raw -echo; dd bs=1 count=6 2>/dev/null | od -An -c; sleep 30`
	s := start(t, script)
	waitFor(t, s, "alt-screen")
	if !s.AltScreen() {
		t.Fatal("the program is on the alternate screen")
	}
	if got := s.ScrollBy(5); got != 0 {
		t.Fatalf("no history there: %d", got)
	}
	if offset, history := s.Scrolled(); offset != 0 || history != 0 {
		t.Fatalf("Scrolled on the alternate screen: %d %d", offset, history)
	}
	s.Arrows(true, 2) // two times up: in application mode that is ESC O A
	waitFor(t, s, "033   O   A")
}
