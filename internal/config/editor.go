package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eutima/jelly-dev/internal/source"
)

// EnvEditor chooses the editor style for one run (nano or vim); it wins over the saved choice.
const EnvEditor = "JD_EDITOR"

const editorFile = "editor.json"

// EditorStyle is how the editor is operated.
type EditorStyle string

// The editor styles.
const (
	EditorNano EditorStyle = "nano" // modeless, with its keys shown at the bottom
	EditorVim  EditorStyle = "vim"  // modal
)

// EditorStyles lists the styles, the default first.
func EditorStyles() []EditorStyle { return []EditorStyle{EditorNano, EditorVim} }

// ParseEditorStyle reads a style name (nano, vim, also vi, case does not matter).
func ParseEditorStyle(name string) (EditorStyle, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "nano":
		return EditorNano, nil
	case "vim", "vi":
		return EditorVim, nil
	}
	return "", fmt.Errorf("unknown editor %q (nano or vim)", name)
}

// LoadEditorStyle returns the style the user chose; ok is false when nothing was chosen yet (or the file is unreadable).
func LoadEditorStyle(configDir string) (style EditorStyle, ok bool) {
	data, err := os.ReadFile(filepath.Join(configDir, editorFile))
	if err != nil {
		return "", false
	}
	var state struct {
		Editor string `json:"editor"`
	}
	if json.Unmarshal(data, &state) != nil {
		return "", false
	}
	style, err = ParseEditorStyle(state.Editor)
	return style, err == nil
}

// SaveEditorStyle remembers the style the user chose.
func SaveEditorStyle(configDir string, style EditorStyle) error {
	if _, err := ParseEditorStyle(string(style)); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{"editor": string(style)}, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(filepath.Join(configDir, editorFile), append(data, '\n'))
}

// ResolveEditorStyle is the style in force: JD_EDITOR, else the saved choice, else (nothing chosen) vim for someone who finished the first-start
// setup before the choice existed and nano for everyone else. An invalid JD_EDITOR is an error.
func ResolveEditorStyle(configDir string, getenv func(string) string, setupDone bool) (EditorStyle, error) {
	if name := getenv(EnvEditor); name != "" {
		style, err := ParseEditorStyle(name)
		if err != nil {
			return "", fmt.Errorf("%s: %w", EnvEditor, err)
		}
		return style, nil
	}
	if style, ok := LoadEditorStyle(configDir); ok {
		return style, nil
	}
	if setupDone {
		return EditorVim, nil
	}
	return EditorNano, nil
}
