//go:build !windows

package terminal

import (
	"os/exec"
	"syscall"
	"time"
)

// killGrace is how long the processes of a closed shell get to end after the hangup before they are killed.
const killGrace = 2 * time.Second

// endProcesses hangs up the shell's whole process group, so the programs it started (a running npm, an editor) end with it,
// and kills what is still there after a grace period. The shell leads its own group because the pseudo terminal starts it in
// a session of its own.
func endProcesses(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	go func() {
		select {
		case <-done:
		case <-time.After(killGrace):
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL) // anything that ignored the hangup; fails quietly when the group is gone
	}()
}
