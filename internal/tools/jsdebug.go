package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// installJsDebug downloads js-debug and writes the launcher js-debug-adapter, which starts its debug server with Node.js (the server listens on
// the port and address it is given). An update unpacks next to the old install and moves it into place when it is complete.
func (e Env) installJsDebug(ctx context.Context, t Tool) error {
	node, err := e.need(t, "Node.js (it runs js-debug)", "node", "nodejs")
	if err != nil {
		return err
	}
	target := filepath.Join(e.DataDir(), "js-debug")
	holder, err := os.CreateTemp("", "jd-js-debug-*.tar.gz") // a name of its own: another window may install at the same time
	if err != nil {
		return err
	}
	archive := holder.Name()
	holder.Close()
	defer os.Remove(archive)
	if err := e.Fetch(ctx, jsDebugURL, archive); err != nil {
		return fmt.Errorf("download js-debug: %w", err)
	}
	if err := verifySHA256(archive, jsDebugSHA256); err != nil {
		return fmt.Errorf("js-debug %s: %w", jsDebugVersion, err)
	}
	if err := os.MkdirAll(e.DataDir(), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(e.DataDir(), "js-debug.new-") // a name of its own: another window may install at the same time
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := extractTarGz(archive, staging); err != nil {
		return fmt.Errorf("unpack js-debug: %w", err)
	}
	script := filepath.Join(target, "js-debug", "src", "dapDebugServer.js")
	if _, err := os.Stat(filepath.Join(staging, "js-debug", "src", "dapDebugServer.js")); err != nil {
		return fmt.Errorf("js-debug: %s was not in the download", script)
	}
	previous := target + ".old"
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	moved := false
	if err := renameFile(target, previous); err == nil {
		moved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := renameFile(staging, target); err != nil {
		if moved {
			_ = renameFile(previous, target)
		}
		return err
	}
	_ = os.RemoveAll(previous)
	return e.writeLauncher("js-debug-adapter",
		"#!/bin/sh\nexec "+shQuote(node)+" "+shQuote(script)+" \"$@\"\n",
		"@"+cmdQuote(node)+" "+cmdQuote(script)+" %*\r\n")
}
