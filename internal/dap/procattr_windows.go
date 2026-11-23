//go:build windows

package dap

import "os/exec"

// detach does nothing: Windows has no controlling terminal to protect.
func detach(*exec.Cmd) {}

// killAdapter ends the adapter.
func killAdapter(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
