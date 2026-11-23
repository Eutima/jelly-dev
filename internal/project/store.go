// Package project keeps the list of recently opened projects and what was open in each of them.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/eutima/jelly-dev/internal/source"
)

const (
	fileName    = "projects.json"
	maxProjects = 20
	dirMode     = 0o755
	// EnvConfigDir overrides the directory the project list is kept in.
	EnvConfigDir = "JD_CONFIG_DIR"
)

// Cursor is a position in a file.
type Cursor struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// Session is what was open in a project when its window was closed.
type Session struct {
	Files   []string          `json:"files,omitempty"`
	Active  string            `json:"active,omitempty"`
	Cursors map[string]Cursor `json:"cursors,omitempty"`
	// Breakpoints are the lines (1-based) of the debugger breakpoints by file, relative to the project.
	Breakpoints map[string][]int `json:"breakpoints,omitempty"`
}

// Entry is one project of the list.
type Entry struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
	Session    Session   `json:"session"`
}

// Store is the list of projects, newest first.
type Store struct {
	path    string
	Entries []Entry `json:"projects"`
	now     func() time.Time
}

// ConfigDir returns the directory of the project list: $JD_CONFIG_DIR, else <user config dir>/jd.
func ConfigDir(getenv func(string) string) (string, error) {
	if dir := getenv(EnvConfigDir); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil { // no $HOME and no $XDG_CONFIG_HOME (a container started as a bare user): keep the list in the temp dir
		return privateTempDir()
	}
	return filepath.Join(base, "jd"), nil
}

// Open reads the project list in dir; a missing file is an empty list.
func Open(dir string) (*Store, error) {
	store := &Store{path: filepath.Join(dir, fileName), now: time.Now}
	data, err := os.ReadFile(store.path)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", store.path, err)
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("read %s: %w", store.path, err)
	}
	return store, nil
}

// Save writes the list atomically.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(s.path, append(data, '\n'))
}

func clean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func (s *Store) find(path string) int {
	for i, entry := range s.Entries {
		if entry.Path == path {
			return i
		}
	}
	return -1
}

// Touch moves a project to the front of the list (adding it) and records the time. The path is made absolute.
func (s *Store) Touch(path string) {
	path = clean(path)
	entry := Entry{Path: path}
	if i := s.find(path); i >= 0 {
		entry = s.Entries[i]
		s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
	}
	entry.LastOpened = s.now()
	s.Entries = append([]Entry{entry}, s.Entries...)
	if len(s.Entries) > maxProjects {
		s.Entries = s.Entries[:maxProjects]
	}
}

// Remove drops a project from the list.
func (s *Store) Remove(path string) {
	if i := s.find(clean(path)); i >= 0 {
		s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
	}
}

// Recent returns the projects whose directory still exists, newest first.
func (s *Store) Recent() []Entry {
	var out []Entry
	for _, entry := range s.Entries {
		if info, err := os.Stat(entry.Path); err == nil && info.IsDir() {
			out = append(out, entry)
		}
	}
	return out
}

// Latest returns the most recently opened project that still exists.
func (s *Store) Latest() (Entry, bool) {
	recent := s.Recent()
	if len(recent) == 0 {
		return Entry{}, false
	}
	return recent[0], true
}

// Session returns what was open in a project.
func (s *Store) Session(path string) Session {
	if i := s.find(clean(path)); i >= 0 {
		return s.Entries[i].Session
	}
	return Session{}
}

// SetSession stores what is open in a project (the project is added when it is not in the list yet).
func (s *Store) SetSession(path string, session Session) {
	path = clean(path)
	i := s.find(path)
	if i < 0 {
		s.Touch(path)
		i = 0
	}
	s.Entries[i].Session = session
}

// Root returns the project directory for a path: the directory itself, or for a file the nearest directory above
// it that holds a .git entry (else the file's directory).
func Root(path string) (string, error) {
	abs := clean(path)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return abs, nil
	}
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return filepath.Dir(abs), nil
}
