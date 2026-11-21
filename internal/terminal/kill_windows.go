//go:build windows

package terminal

import "os/exec"

// endProcesses ends the shell (embedded terminals are not supported on Windows yet, so this is never reached in practice).
func endProcesses(cmd *exec.Cmd, _ <-chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
