package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	clipboardTimeout = 5 * time.Second
	// clipboardWaitDelay bounds how long we wait for output pipes after the timeout killed the tool.
	clipboardWaitDelay = time.Second
	// waylandEmptyMessage is printed by wl-paste (exit status 1) when nothing was copied.
	waylandEmptyMessage = "nothing is copied"
	// powershellRead reads the clipboard as UTF-8 text without trailing newline changes.
	powershellRead = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Get-Clipboard -Raw"
)

var (
	// ErrNoClipboardTool is returned when no clipboard reader is available on this system.
	ErrNoClipboardTool = errors.New("no clipboard tool found (install wl-clipboard, xclip or xsel)")
	// ErrEmptyClipboard is returned when the clipboard holds no text.
	ErrEmptyClipboard = errors.New("clipboard is empty")
)

// commandRunner runs an external command and returns its stdout.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// clipboardCommand is one way to read the clipboard.
type clipboardCommand struct {
	name string
	args []string
}

// ReadClipboard returns the text on the system clipboard.
func ReadClipboard() (string, error) {
	return readClipboard(context.Background(), clipboardCandidates(), execRunner, exec.LookPath)
}

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, clipboardTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed command list, no user input
	cmd.WaitDelay = clipboardWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("timed out after %s", clipboardTimeout)
		}
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// readClipboard tries each available candidate in order and returns the first successful result.
// A tool that fails falls through to the next one; an empty clipboard is final.
func readClipboard(
	ctx context.Context,
	candidates []clipboardCommand,
	run commandRunner,
	lookPath func(string) (string, error),
) (string, error) {
	var lastErr error
	for _, candidate := range candidates {
		if _, err := lookPath(candidate.name); err != nil {
			continue
		}
		data, err := run(ctx, candidate.name, candidate.args...)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), waylandEmptyMessage) {
				return "", ErrEmptyClipboard
			}
			lastErr = fmt.Errorf("%s: %w", candidate.name, err)
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return "", fmt.Errorf("clipboard: %w", ErrBinary)
		}
		text := strings.TrimPrefix(string(data), "\ufeff")
		if strings.TrimSpace(text) == "" {
			return "", ErrEmptyClipboard
		}
		return text, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("read clipboard: %w", lastErr)
	}
	return "", ErrNoClipboardTool
}

// clipboardCandidates lists clipboard readers in the order they should be tried on this system.
func clipboardCandidates() []clipboardCommand {
	powershell := clipboardCommand{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", powershellRead}}
	unix := []clipboardCommand{
		{"wl-paste", []string{"--no-newline"}},
		{"xclip", []string{"-selection", "clipboard", "-out"}},
		{"xsel", []string{"--clipboard", "--output"}},
	}
	switch {
	case runtime.GOOS == "darwin":
		return []clipboardCommand{{"pbpaste", nil}}
	case runtime.GOOS == "windows":
		return []clipboardCommand{powershell}
	case isWSL():
		// The Windows clipboard is the one the user copied to; prefer it over a Linux X server.
		return append([]clipboardCommand{powershell}, unix...)
	default:
		return unix
	}
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
}
