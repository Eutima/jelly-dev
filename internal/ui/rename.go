package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// renameCommand handles `:rename [name]` (also `:mv`): the file of the shown tab gets another name in its folder. Without a name a prompt
// opens with the current one.
func (m ProjectModel) renameCommand(argument string) (tea.Model, tea.Cmd) {
	path := m.editor.Path()
	if path == "" {
		m.say("E32: No file name: nothing to rename", true)
		return m, nil
	}
	return m.renameFile(path, argument)
}

// renameFile renames the file at path: with a name at once, without one the prompt opens first.
func (m ProjectModel) renameFile(path, name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.name = &namePrompt{dir: filepath.Dir(path), home: filepath.Dir(path), input: []rune(filepath.Base(path)), rename: path}
		return m, nil
	}
	if m.insideProject(filepath.Dir(path)) == nil { // a file of the project: the language server fixes the imports, :refactor undo reverts it
		m.editor.notice("", false)               // drops the file info of the editor, which would show beside the project status
		return m.startFileRefactorOf(path, name) // checks all tabs for unsaved changes
	}
	if i := m.tabIndex(path); i >= 0 && m.openEditors()[i].Modified() {
		m.say("E37: unsaved changes: save first (:w)", true)
		return m, nil
	}
	target, err := siblingPath(path, name)
	if err == nil {
		err = moveFile(path, target)
	}
	if err != nil {
		m.say("Cannot rename: "+err.Error(), true)
		return m, nil
	}
	m = m.followRename(path, target)
	m.say("Renamed to "+target, false)
	return m, nil
}

// siblingPath returns the path of a file called name next to path; a name is a plain file name, and one without an extension keeps the
// extension of the old file.
func siblingPath(path, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%q is not a file name (no folders)", name)
	}
	if filepath.Ext(name) == "" {
		name += filepath.Ext(path)
	}
	return filepath.Join(filepath.Dir(path), name), nil
}

// moveFile renames a file and refuses to replace another one.
func moveFile(from, to string) error {
	if from == to {
		return errors.New("that is the name it has")
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s exists", filepath.Base(to))
	}
	return os.Rename(from, to)
}

// followRename shows the renamed file in the tab of the old one and updates the tree.
func (m ProjectModel) followRename(from, to string) ProjectModel {
	if i := m.tabIndex(from); i >= 0 {
		wasShown := from == m.editor.Path()
		m.closeTab(i)
		if wasShown {
			_ = m.openFile(to, false)
		}
	}
	m.tree.Refresh()
	return m
}

// finishRename runs the rename of the open prompt.
func (m ProjectModel) finishRename(prompt namePrompt) (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(string(prompt.input))
	if _, err := siblingPath(prompt.rename, name); err != nil {
		m.notice("Cannot rename: "+err.Error(), true) // the prompt stays open to fix the name
		return m, nil
	}
	m.name = nil
	if name == filepath.Base(prompt.rename) {
		return m, nil
	}
	return m.renameFile(prompt.rename, name)
}
