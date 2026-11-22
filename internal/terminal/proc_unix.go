//go:build !windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// unixProc is a shell on a unix pseudo terminal.
type unixProc struct {
	file *os.File
	cmd  *exec.Cmd
}

// startProcess runs command in dir inside a pseudo terminal of width x height cells.
func startProcess(command *exec.Cmd, dir string, width, height int) (process, error) {
	command.Dir = dir
	command.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
	if err != nil {
		if errors.Is(err, pty.ErrUnsupported) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	return &unixProc{file: file, cmd: command}, nil
}

func (p *unixProc) Read(b []byte) (int, error)  { return p.file.Read(b) }
func (p *unixProc) Write(b []byte) (int, error) { return p.file.Write(b) }
func (p *unixProc) Wait() error                 { return p.cmd.Wait() }

func (p *unixProc) Resize(width, height int) error {
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
}

// Close hangs up the shell's whole process group, so the programs it started (a running npm, an editor) end with it, and kills
// what is still there after a grace period. The shell leads its own group because the pseudo terminal starts it in a session of
// its own.
func (p *unixProc) Close(done <-chan struct{}) {
	_ = p.file.Close()
	if p.cmd.Process == nil {
		return
	}
	pid := p.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	go func() {
		select {
		case <-done:
		case <-time.After(killGrace):
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL) // anything that ignored the hangup; fails quietly when the group is gone
	}()
}

// killGrace is how long the processes of a closed shell get to end after the hangup before they are killed.
const killGrace = 2 * time.Second
