package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eutima/jelly-dev/internal/source"
)

const (
	noteSettingsFile = "notes.json"
	// DefaultNote is the note that `:note` opens without a name.
	DefaultNote = "notes"
	noteExt     = ".md"
)

type noteSettings struct {
	Root string `json:"root"`
}

// NoteRoot returns the project whose notes are shown, "" when none was chosen (the notes then belong to the open project).
func NoteRoot(configDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(configDir, noteSettingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", noteSettingsFile, err)
	}
	var settings noteSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("read %s: %w", noteSettingsFile, err)
	}
	return settings.Root, nil
}

// SetNoteRoot stores the project whose notes are shown in every window.
func SetNoteRoot(configDir, root string) error {
	if err := os.MkdirAll(configDir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", configDir, err)
	}
	data, err := json.MarshalIndent(noteSettings{Root: clean(root)}, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(filepath.Join(configDir, noteSettingsFile), append(data, '\n'))
}

// NotePath returns the file of a note of the project in root: <root>/.jd/notes/<name>.md. A name is a plain file name;
// one with a folder in it is refused, so a note can never leave the notes folder.
func NotePath(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultNote
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%q is not a note name (no folders)", name)
	}
	if !strings.HasSuffix(name, noteExt) {
		name += noteExt
	}
	return filepath.Join(root, ".jd", "notes", name), nil
}

// NoteOwner returns the project a note file belongs to (the folder that has .jd/notes/ above the file); ok is false for any other file.
func NoteOwner(path string) (root string, ok bool) {
	dir := filepath.Dir(path)
	if filepath.Base(dir) != "notes" {
		return "", false
	}
	jd := filepath.Dir(dir)
	if filepath.Base(jd) != ".jd" {
		return "", false
	}
	return filepath.Dir(jd), true
}
