package dap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Adapter is a debug adapter jd knows how to start and the files it debugs.
type Adapter struct {
	Name       string
	Command    []string // started in the project folder
	Listens    bool     // the adapter opens a TCP port and prints its address ("... listening at: 127.0.0.1:4711") instead of using stdio
	Extensions []string // file types it debugs, lower case with the dot
	// Launch returns the launch request for debugging a file of a project.
	Launch func(file, root string) map[string]any
}

// DefaultAdapters are the adapters jd ships with.
func DefaultAdapters() []Adapter {
	return []Adapter{
		{
			Name: "delve", Command: []string{"dlv", "dap", "--listen=127.0.0.1:0"}, Listens: true, Extensions: []string{".go"},
			Launch: func(file, root string) map[string]any {
				// a test file is debugged as a test (its package), anything else as the program of its folder
				mode := "debug"
				if strings.HasSuffix(file, "_test.go") {
					mode = "test"
				}
				// "remote": the output of the program comes as DAP output events (it would go to the terminal of the adapter, which jd does not show)
				return map[string]any{"request": "launch", "mode": mode, "program": filepath.Dir(file), "cwd": root, "outputMode": "remote"}
			},
		},
		{
			Name: "debugpy", Command: []string{"debugpy-adapter"}, Extensions: []string{".py"},
			Launch: pythonLaunch,
		},
	}
}

// ErrNoAdapter is returned for files that no adapter debugs.
var ErrNoAdapter = errors.New("no debug adapter for this file type")

// NotInstalledError is returned when the adapter of a file is not on the PATH.
type NotInstalledError struct{ Adapter, Command string }

func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("debug adapter %s not found: %q is not on the PATH (jd install-lsp debug installs it)", e.Adapter, e.Command)
}

// AdapterFor returns the adapter that debugs a file.
func AdapterFor(adapters []Adapter, file string) (Adapter, bool) {
	ext := strings.ToLower(filepath.Ext(file))
	for _, adapter := range adapters {
		for _, candidate := range adapter.Extensions {
			if candidate == ext {
				return adapter, true
			}
		}
	}
	return Adapter{}, false
}

// Debug starts the adapter for a file and runs the program under it, with the breakpoints (path to lines). It returns the running session and
// which breakpoints the adapter accepted. lookPath finds the adapter command (exec.LookPath).
func Debug(ctx context.Context, adapters []Adapter, lookPath func(string) (string, error), file, root string, breakpoints map[string][]int) (*Session, map[string][]Breakpoint, error) {
	adapter, ok := AdapterFor(adapters, file)
	if !ok {
		return nil, nil, ErrNoAdapter
	}
	if len(adapter.Command) == 0 {
		return nil, nil, fmt.Errorf("debug adapter %s has no command", adapter.Name)
	}
	if _, err := lookPath(adapter.Command[0]); err != nil {
		return nil, nil, &NotInstalledError{Adapter: adapter.Name, Command: adapter.Command[0]}
	}
	var client *Client
	var err error
	if adapter.Listens {
		client, err = StartListening(adapter.Command, root)
	} else {
		client, err = Start(adapter.Command, root)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", adapter.Name, err)
	}
	session := NewSession(client)
	verified, err := session.Start(ctx, adapter.Launch(file, root), breakpoints)
	if err != nil {
		_ = session.Stop(context.Background())
		return nil, nil, err
	}
	return session, verified, nil
}
