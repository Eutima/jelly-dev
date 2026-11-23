//go:build !windows

package dap

import (
	"os/exec"
	"syscall"
)

// detach starts the adapter in a session of its own, so it has no controlling terminal. The terminal belongs to jd: debugpy's launcher opens
// /dev/tty and makes the process group of the program the foreground group, so that jd, back in the background, is stopped by the terminal
// (SIGTTIN) as soon as the program ends and jd reads a key.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// killAdapter ends the adapter and the processes that stayed in its group (a program that the adapter put in a group of its own, as debugpy
// does, is ended by the adapter when it disconnects).
func killAdapter(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the group of the adapter: its own session leader
	_ = cmd.Process.Kill()
}
