// Package terminal runs a shell in a pseudo terminal and keeps its screen, so a window can show it as a panel.
package terminal

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// ErrUnsupported is returned where no pseudo terminal can be created (Windows).
var ErrUnsupported = errors.New("embedded terminals are not supported on this system")

// Session is a running shell and the screen it draws on.
type Session struct {
	screen *vt.SafeEmulator
	pty    *os.File
	cmd    *exec.Cmd
	name   string

	updates chan struct{} // receives a signal after the screen changed (at most one pending)
	done    chan struct{} // closed when the shell has ended

	closeOnce sync.Once
}

// Start runs command in dir inside a pseudo terminal of width x height cells.
func Start(command *exec.Cmd, dir string, width, height int) (*Session, error) {
	width, height = max(width, 1), max(height, 1)
	command.Dir = dir
	command.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
	if err != nil {
		if errors.Is(err, pty.ErrUnsupported) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	s := &Session{screen: vt.NewSafeEmulator(width, height), pty: file, cmd: command, name: filepath.Base(command.Path), updates: make(chan struct{}, 1), done: make(chan struct{})}
	go s.pump()
	go s.answer()
	return s, nil
}

// pump copies what the shell prints onto the screen.
func (s *Session) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			_, _ = s.screen.Write(buf[:n])
			select {
			case s.updates <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
	_ = s.cmd.Wait()
	close(s.done)
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// answer sends the replies of the screen (cursor position reports and the like) to the shell.
func (s *Session) answer() {
	_, _ = io.Copy(s.pty, s.screen)
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
	_, _ = s.pty.Write(data)
}

// Paste sends text to the shell the way a terminal does (bracketed when the shell asked for it).
func (s *Session) Paste(text string) { s.screen.Paste(text) }

// Resize changes the size of the screen and the shell's window.
func (s *Session) Resize(width, height int) {
	width, height = max(width, 1), max(height, 1)
	if width == s.screen.Width() && height == s.screen.Height() {
		return
	}
	s.screen.Resize(width, height)
	_ = pty.Setsize(s.pty, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
}

// Close ends the shell.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		_ = s.pty.Close()
		endProcesses(s.cmd, s.done)
		_ = s.screen.Close()
	})
}

// Lines renders the screen as width x height styled lines; when showCursor is set the cursor cell is drawn reversed.
func (s *Session) Lines(showCursor bool) []string {
	width, height := s.screen.Width(), s.screen.Height()
	cursor := s.screen.CursorPosition()
	hidden := s.screen.CursorHidden()
	lines := make([]string, height)
	for y := range height {
		row := make(uv.Line, width)
		for x := range width {
			if cell := s.screen.CellAt(x, y); cell != nil {
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
