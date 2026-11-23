// Package terminal runs a shell in a pseudo terminal and keeps its screen, so a window can show it as a panel.
package terminal

import (
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// writeWait is how long a write to the input pipe of the screen may take (see bounded).
const writeWait = 500 * time.Millisecond

// ErrUnsupported is returned where no pseudo terminal can be created (a Windows older than 10 version 1809 has no ConPTY).
var ErrUnsupported = errors.New("embedded terminals are not supported on this system")

// process is a shell running in a pseudo terminal: a unix pty, or a ConPTY on Windows.
type process interface {
	io.ReadWriter
	// Resize changes the size of the pseudo terminal.
	Resize(width, height int) error
	// Wait blocks until the shell has ended and everything of it is released. It is called after Read has failed.
	Wait() error
	// Close ends the shell and the programs it started. done is closed when the shell has ended.
	Close(done <-chan struct{})
}

// Session is a running shell and the screen it draws on.
type Session struct {
	screen *vt.SafeEmulator
	proc   process
	name   string

	updates chan struct{} // receives a signal after the screen changed (at most one pending)
	done    chan struct{} // closed when the shell has ended

	// screenMu keeps the output of the shell (pump) and the readers of the screen (Lines, Resize) apart: the emulator hands out
	// pointers into its live buffer, so its own lock does not cover copying a cell
	screenMu   sync.Mutex
	offset     int // how many lines the view is scrolled back into the history, 0: the live screen (guarded by screenMu)
	closeOnce  sync.Once
	answerDone chan struct{} // closed when the goroutine that forwards the screen's answers has ended
	stopping   atomic.Bool   // Close was called: the goroutine that forwards the answers of the screen ends
}

// Start runs command in dir inside a pseudo terminal of width x height cells.
func Start(command *exec.Cmd, dir string, width, height int) (*Session, error) {
	width, height = max(width, 1), max(height, 1)
	proc, err := startProcess(command, dir, width, height)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(command.Path), ".exe")
	s := &Session{screen: vt.NewSafeEmulator(width, height), proc: proc, name: name, updates: make(chan struct{}, 1), done: make(chan struct{}), answerDone: make(chan struct{})}
	go s.pump()
	go s.answer()
	return s, nil
}

// pump copies what the shell prints onto the screen.
func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			s.screenMu.Lock()
			before := s.screen.ScrollbackLen()
			_, _ = s.screen.Write(buf[:n])
			// a view that is scrolled back stays on the lines it shows while new ones come (once the history is full, 10000 lines, the oldest
			// ones are dropped and the length no longer grows: the emulator does not say how many went, so the view then drifts slowly)
			if s.offset > 0 {
				s.offset = min(s.offset+max(s.screen.ScrollbackLen()-before, 0), s.screen.ScrollbackLen())
			}
			s.screenMu.Unlock()
			select {
			case s.updates <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
	_ = s.proc.Wait()
	close(s.done)
	go func() { _, _ = s.screen.InputPipe().Write([]byte{0}) }() // wakes answer, which ends now; it is always still reading
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// answer sends the replies of the screen (cursor position reports and the like) to the shell.
// It goes on reading after the session was closed, and drops what it reads, until the shell has ended: the pipe between the screen
// and this reader is synchronous, so a write of the screen (an answer to a query) would block for ever without a reader. When the
// shell has ended, pump wakes the reader with one byte and it returns. (The emulator is never closed: its Close writes a flag that
// its Read reads in this goroutine, a data race.)
func (s *Session) answer() {
	defer close(s.answerDone)
	buf := make([]byte, 4096)
	for {
		n, err := s.screen.Read(buf)
		if err != nil {
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
		if n > 0 && !s.stopping.Load() {
			_, _ = s.proc.Write(buf[:n])
		}
	}
}

// Name is the program the session runs, for example "zsh".
func (s *Session) Name() string { return s.name }

// Updates signals after the screen changed or the shell ended.
func (s *Session) Updates() <-chan struct{} { return s.updates }

// Done reports whether the shell has ended.
func (s *Session) Done() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Write sends typed bytes to the shell.
func (s *Session) Write(data []byte) {
	_, _ = s.proc.Write(data)
}

// Paste sends text to the shell the way a terminal does (bracketed when the shell asked for it).
func (s *Session) Paste(text string) {
	if s.ended() {
		return
	}
	s.bounded(func() { s.screen.Paste(text) })
}

// Arrows sends the arrow key up or down n times the way the screen encodes it for the program: with the plain or the application cursor-key
// sequences, as the program asked (vim and less often ask for the application ones).
func (s *Session) Arrows(up bool, n int) {
	if s.ended() {
		return
	}
	code := uv.KeyDown
	if up {
		code = uv.KeyUp
	}
	for range n {
		s.bounded(func() { s.screen.SendKey(uv.KeyPressEvent{Code: code}) })
	}
}

// ended reports whether the session was closed or its shell has ended: nothing is sent to it any more.
func (s *Session) ended() bool { return s.stopping.Load() || s.Done() }

// bounded runs a write to the screen's input pipe and waits for it at most a moment. That pipe is synchronous and only the answer
// goroutine reads it, and that one ends when the shell has ended: a write in that very moment would never finish and would freeze
// the window that waits for it. The write is then left behind (one goroutine) instead.
func (s *Session) bounded(write func()) {
	finished := make(chan struct{})
	go func() {
		write()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(writeWait):
	}
}

// Resize changes the size of the screen and the shell's window.
func (s *Session) Resize(width, height int) {
	width, height = max(width, 1), max(height, 1)
	if width == s.screen.Width() && height == s.screen.Height() {
		return
	}
	if s.ended() { // nothing to resize any more (and the screen's in-band resize report needs a reader)
		return
	}
	s.screenMu.Lock()
	s.bounded(func() { s.screen.Resize(width, height) })
	s.offset = min(s.offset, s.screen.ScrollbackLen())
	s.screenMu.Unlock()
	_ = s.proc.Resize(width, height)
}

// Close ends the shell.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.proc.Close(s.done)
		s.stopping.Store(true) // the answers of the screen are not sent to a closed shell any more (answer drains them)
	})
}

// ScrollBy scrolls the view into the history of the shell (positive: back in time) or towards the live screen (negative), and returns how far
// back it is now. A program on the alternate screen (vim, less) has no history: there the view does not move.
func (s *Session) ScrollBy(lines int) int {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	if s.screen.IsAltScreen() {
		s.offset = 0
		return 0
	}
	s.offset = min(max(s.offset+lines, 0), s.screen.ScrollbackLen())
	return s.offset
}

// ScrollToLive brings the view back to the live screen (typing does that).
func (s *Session) ScrollToLive() {
	s.screenMu.Lock()
	s.offset = 0
	s.screenMu.Unlock()
}

// Scrolled returns how many lines the view is scrolled back, and how many lines of history there are.
func (s *Session) Scrolled() (offset, history int) {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	if s.screen.IsAltScreen() {
		return 0, 0
	}
	return s.offset, s.screen.ScrollbackLen()
}

// AltScreen tells whether a program that draws on the alternate screen (vim, less, htop) is running: it keeps no history, so a wheel turn
// is sent to it as the arrow keys.
func (s *Session) AltScreen() bool {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	return s.screen.IsAltScreen()
}

// cellAt is the cell of the view at column x and row y: a line of the history while the view is scrolled back, else of the live screen.
// Call with screenMu held.
func (s *Session) cellAt(x, y int) *uv.Cell {
	if s.offset == 0 || s.screen.IsAltScreen() {
		return s.screen.CellAt(x, y)
	}
	history := s.screen.ScrollbackLen()
	line := history - min(s.offset, history) + y // the first row of the view is this line of history plus the screen
	if line < history {
		return s.screen.ScrollbackCellAt(x, line)
	}
	return s.screen.CellAt(x, line-history)
}

// Lines renders the screen as width x height styled lines; when showCursor is set the cursor cell is drawn reversed (not while the view is
// scrolled back: the cursor is on the live screen).
func (s *Session) Lines(showCursor bool) []string {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	width, height := s.screen.Width(), s.screen.Height()
	cursor := s.screen.CursorPosition()
	hidden := s.screen.CursorHidden() || (s.offset > 0 && !s.screen.IsAltScreen())
	lines := make([]string, height)
	for y := range height {
		row := make(uv.Line, width)
		for x := range width {
			if cell := s.cellAt(x, y); cell != nil {
				row[x] = *cell
			}
			if showCursor && !hidden && x == cursor.X && y == cursor.Y {
				if row[x].Content == "" {
					row[x].Content = " "
				}
				row[x].Style.Attrs ^= uv.AttrReverse
			}
		}
		text := strings.TrimRight(row.Render(), "\n")
		if gap := width - lipgloss.Width(text); gap > 0 {
			text += strings.Repeat(" ", gap)
		}
		lines[y] = text + "\x1b[0m"
	}
	return lines
}

// Cells returns the text of the screen cell by cell: one string per column, "" for the right half of a wide character.
// The selection of text works on it.
func (s *Session) Cells() [][]string {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	width, height := s.screen.Width(), s.screen.Height()
	rows := make([][]string, height)
	for y := range height {
		row := make([]string, width)
		for x := range width {
			if cell := s.cellAt(x, y); cell != nil && cell.Width > 0 {
				row[x] = cell.Content
				if row[x] == "" {
					row[x] = " "
				}
			} else if cell == nil {
				row[x] = " "
			}
		}
		rows[y] = row
	}
	return rows
}
