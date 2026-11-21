package source

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// powershellWrite sets the clipboard to the UTF-8 text on stdin.
	powershellWrite = "[Console]::InputEncoding=[System.Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"
)

// ErrNoClipboardWriter is returned when no tool that can write the clipboard is available.
var ErrNoClipboardWriter = errors.New("no clipboard tool found (install wl-clipboard, xclip or xsel)")

// inputRunner runs an external command with text on its standard input.
type inputRunner func(ctx context.Context, stdin, name string, args ...string) error

// WriteClipboard puts text on the system clipboard.
func WriteClipboard(text string) error {
	return writeClipboard(context.Background(), text, clipboardWriters(), execInputRunner, exec.LookPath)
}

func execInputRunner(ctx context.Context, stdin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, clipboardTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed command list, no user input
	cmd.Stdin = strings.NewReader(stdin)
	// xclip, xsel and wl-copy stay in the background to serve the clipboard: their output pipes must not be waited for.
	cmd.WaitDelay = clipboardWaitDelay
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", clipboardTimeout)
		}
		return err
	}
	return nil
}

// writeClipboard tries each available tool in order; the first one that works wins.
func writeClipboard(
	ctx context.Context,
	text string,
	candidates []clipboardCommand,
	run inputRunner,
	lookPath func(string) (string, error),
) error {
	var lastErr error
	for _, candidate := range candidates {
		if _, err := lookPath(candidate.name); err != nil {
			continue
		}
		if err := run(ctx, text, candidate.name, candidate.args...); err != nil {
			lastErr = fmt.Errorf("%s: %w", candidate.name, err)
			continue
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("write clipboard: %w", lastErr)
	}
	return ErrNoClipboardWriter
}

// clipboardWriters lists clipboard writers in the order they should be tried on this system.
func clipboardWriters() []clipboardCommand {
	powershell := clipboardCommand{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", powershellWrite}}
	unix := []clipboardCommand{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard", "-in"}},
		{"xsel", []string{"--clipboard", "--input"}},
	}
	switch {
	case runtime.GOOS == "darwin":
		return []clipboardCommand{{"pbcopy", nil}}
	case runtime.GOOS == "windows":
		return []clipboardCommand{powershell}
	case isWSL():
		return append([]clipboardCommand{powershell}, unix...)
	default:
		return unix
	}
}
