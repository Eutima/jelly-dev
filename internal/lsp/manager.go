package lsp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// NotInstalledError is returned when the server for a file is not on the PATH.
type NotInstalledError struct{ Server, Command string }

func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("language server %s not found: %q is not on the PATH (run the jd installer, or set the server in lsp.json)", e.Server, e.Command)
}

// ErrNoServer is returned for files no configured server handles.
var ErrNoServer = errors.New("no language server for this file type")

// Manager starts the language servers a project needs, when they are first needed, and routes requests to them.
type Manager struct {
	root     string
	servers  []ServerSpec
	lookPath func(string) (string, error)
	start    func(ctx context.Context, name string, command []string, root string) (*Client, error)
	python   func(root string) string // the Python interpreter of a project (see pythonFinder)

	mu            sync.Mutex
	clients       map[string]*Client  // by server name and root
	chosen        map[string][]string // the formatter the user picked, by file type (see formatters.go)
	formatterFile string              // where the choices are kept; empty: not stored
}

// NewManager returns a manager for the project in root.
func NewManager(root string, servers []ServerSpec) *Manager {
	return &Manager{root: root, servers: servers, lookPath: exec.LookPath, start: Start, python: newPythonFinder().Find, clients: map[string]*Client{}, chosen: map[string][]string{}}
}

// client returns the running server for a file, starting it first when needed.
func (m *Manager) client(ctx context.Context, path string) (*Client, string, error) {
	spec, languageID, ok := specFor(m.servers, path)
	if !ok {
		return nil, "", ErrNoServer
	}
	if _, err := m.lookPath(spec.Command[0]); err != nil {
		return nil, "", &NotInstalledError{Server: spec.Name, Command: spec.Command[0]}
	}
	root := rootFor(spec, m.root, path)
	key := spec.Name + "\x00" + root
	if c := m.running(key); c != nil {
		return c, languageID, nil
	}
	// the interpreter is looked for before the lock is taken: asking uv can take seconds, and every request waits for the lock
	python := m.pythonFor(spec, root)
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[key]; c != nil { // somebody else started it in the meantime
		select {
		case <-c.done:
			delete(m.clients, key)
		default:
			return c, languageID, nil
		}
	}
	c, err := m.start(ctx, spec.Name, spec.Command, root)
	if err != nil {
		return nil, "", err
	}
	if python != "" {
		if err := c.Configure(map[string]any{"python": map[string]any{"pythonPath": python}}); err != nil {
			c.Close()
			return nil, "", fmt.Errorf("%s: tell the server about %s: %w", spec.Name, python, err)
		}
	}
	m.clients[key] = c
	if spec.Name == "typescript-language-server" && !hasProjectFile(root) {
		m.preload(c, spec, root)
	}
	return c, languageID, nil
}

const (
	maxPreloadFiles = 300
	maxPreloadBytes = 512 << 10
)

// hasProjectFile reports whether a folder has the tsconfig.json or jsconfig.json that tells the TypeScript server which files
// belong together.
func hasProjectFile(root string) bool {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

// preload opens the files of the server's language below root. Without tsconfig.json or jsconfig.json the TypeScript server
// only knows the files that are open, so references and definitions would never leave the current file. Dependency and build
// folders and hidden folders are skipped, and at most maxPreloadFiles files are opened.
func (m *Manager) preload(c *Client, spec ServerSpec, root string) {
	skip := map[string]bool{"node_modules": true, "dist": true, "build": true, "out": true, "coverage": true}
	opened := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case entry.IsDir():
			if path != root && (skip[entry.Name()] || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		case opened >= maxPreloadFiles:
			return filepath.SkipAll
		}
		found, languageID, ok := specFor(m.servers, path)
		if !ok || found.Name != spec.Name {
			return nil
		}
		if info, err := entry.Info(); err != nil || !info.Mode().IsRegular() || info.Size() > maxPreloadBytes {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 -- a file of the user's own project
		if err != nil {
			return nil
		}
		if c.Sync(path, languageID, string(data)) == nil {
			opened++
		}
		return nil
	})
}

// Definition returns the places where the symbol at a position (zero-based line, rune column) of text is defined.
// text is what the editor holds, so the server sees unsaved changes.
func (m *Manager) Definition(ctx context.Context, path, text string, row, col int) ([]Place, error) {
	return m.query(ctx, path, text, row, col, (*Client).Definition)
}

// References returns the uses of the symbol at a position.
func (m *Manager) References(ctx context.Context, path, text string, row, col int) ([]Place, error) {
	return m.query(ctx, path, text, row, col, (*Client).References)
}

func (m *Manager) query(ctx context.Context, path, text string, row, col int,
	ask func(*Client, context.Context, string, Position) ([]Location, error),
) ([]Place, error) {
	c, languageID, err := m.client(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := c.Sync(path, languageID, text); err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	line := ""
	if row >= 0 && row < len(lines) {
		line = lines[row]
	}
	locs, err := ask(c, ctx, path, Position{Line: row, Character: UTF16Column([]rune(line), col)})
	if err != nil {
		return nil, err
	}
	return places(locs, path, text), nil
}

// places turns server locations into places: file paths, rune columns and the text of each line. Locations
// in other schemes (jar:, ...) are dropped.
func places(locs []Location, openPath, openText string) []Place {
	cache := map[string][]string{openPath: strings.Split(openText, "\n")}
	var out []Place
	for _, loc := range locs {
		path, ok := URIToPath(loc.URI)
		if !ok {
			continue
		}
		lines, cached := cache[path]
		if !cached {
			if data, err := os.ReadFile(path); err == nil {
				lines = strings.Split(string(data), "\n")
			}
			cache[path] = lines
		}
		line := ""
		if row := loc.Range.Start.Line; row >= 0 && row < len(lines) {
			line = strings.TrimSuffix(lines[row], "\r")
		}
		out = append(out, Place{Path: path, Row: loc.Range.Start.Line, Col: RuneColumn(line, loc.Range.Start.Character), Text: line})
	}
	return out
}

// Close stops all servers.
func (m *Manager) Close() {
	m.mu.Lock()
	clients := m.clients
	m.clients = map[string]*Client{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()
}

// running returns the server for a key when it is started and still alive.
func (m *Manager) running(key string) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.clients[key]
	if c == nil {
		return nil
	}
	select {
	case <-c.done:
		delete(m.clients, key) // the server stopped: it is started again
		return nil
	default:
		return c
	}
}

// pythonFor returns the Python interpreter pyright has to resolve the imports of the project with ("" for every other server and
// when there is none: pyright then uses the Python on the PATH).
func (m *Manager) pythonFor(spec ServerSpec, root string) string {
	if spec.Name != "pyright" || m.python == nil {
		return ""
	}
	python := m.python(root)
	if python == "" && root != m.root {
		python = m.python(m.root) // the environment sits at the top of a project with several Python folders
	}
	return python
}
