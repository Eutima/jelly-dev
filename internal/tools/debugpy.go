package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// debugpyVersion is the release of debugpy (the debug adapter of Python) that is installed; raise it on purpose, after trying a new one.
const debugpyVersion = "1.8.22"

// installDebugpy installs debugpy into a Python environment of its own below the data folder and writes the launcher debugpy-adapter. The
// adapter runs from that environment, but debugs with the interpreter of the project (the adapter hands its own debugpy to it), so the
// project needs no debugpy. An update builds the new environment next to the old one, which is put back when it fails.
func (e Env) installDebugpy(ctx context.Context, t Tool) error {
	python, err := e.need(t, "Python 3 (it runs the debug adapter)", "python3", "python", "py")
	if err != nil {
		return err
	}
	target := filepath.Join(e.DataDir(), "debugpy")
	previous := target + ".old"
	if err := os.MkdirAll(e.DataDir(), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	moved := false
	if err := renameFile(target, previous); err == nil {
		moved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	restore := func() {
		_ = os.RemoveAll(target)
		if moved {
			_ = renameFile(previous, target)
		}
	}
	if err := e.Run(ctx, nil, python, "-m", "venv", target); err != nil {
		restore()
		return fmt.Errorf("create the Python environment of the debugger (the venv module is missing? Debian and Ubuntu: apt install python3-venv): %w", err)
	}
	interpreter := filepath.Join(target, "bin", "python")
	if e.GOOS == "windows" {
		interpreter = filepath.Join(target, "Scripts", "python.exe")
	}
	if err := e.Run(ctx, nil, interpreter, "-m", "pip", "install", "--disable-pip-version-check", "debugpy=="+debugpyVersion); err != nil {
		restore()
		return err
	}
	_ = os.RemoveAll(previous)
	return e.writeLauncher("debugpy-adapter",
		"#!/bin/sh\nexec "+shQuote(interpreter)+" -m debugpy.adapter \"$@\"\n",
		"@"+cmdQuote(interpreter)+" -m debugpy.adapter %*\r\n")
}
