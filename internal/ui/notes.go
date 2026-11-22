package ui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

const noteDirMode = 0o755

// noteRoot is the project whose notes are shown: the chosen one (it may be another project than the open one), else the open one.
func (m ProjectModel) noteRoot() string {
	if m.hooks.NoteRoot != nil {
		if root := m.hooks.NoteRoot(); root != "" && isDir(root) {
			return root
		}
	}
	return m.root
}

// noteCommand handles `:note [name]` (opens a note in an editor tab) and `:note project` (chooses the project of the notes).
func (m ProjectModel) noteCommand(argument string) (tea.Model, tea.Cmd) {
	if argument == "project" {
		model, cmd := m.openPickerTitled("Project of the notes")
		picking := model.(ProjectModel)
		picking.pickingNotes = true
		return picking, cmd
	}
	return m.openNote(argument)
}

// openNote opens a note of the notes project in a tab; a note that does not exist yet is created when it is saved.
func (m ProjectModel) openNote(name string) (tea.Model, tea.Cmd) {
	return m.openNoteIn(m.noteRoot(), name)
}

// openNoteIn opens a note of the project in root.
func (m ProjectModel) openNoteIn(root, name string) (tea.Model, tea.Cmd) {
	path, err := project.NotePath(root, name)
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), noteDirMode); err != nil {
		m.say(fmt.Sprintf("cannot create the notes folder: %v", err), true)
		return m, nil
	}
	if err := m.openFile(path, false); err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	m.focus = focusProjectEdit
	if root != m.root {
		m.editor.notice("Note of the project "+filepath.Base(root)+" (:note project changes it)", false)
	}
	return m.resizeEditor(), nil
}

// chooseNoteProject stores the project of the notes and opens its notes.
func (m ProjectModel) chooseNoteProject(root string) (tea.Model, tea.Cmd) {
	if !isDir(root) {
		m.say(fmt.Sprintf("E344: Can't find directory \"%s\"", root), true)
		return m, nil
	}
	if m.hooks.SetNoteRoot != nil {
		m.hooks.SetNoteRoot(root)
	}
	return m.openNoteIn(root, "")
}

// copyPath puts the path of the file in the shown tab on the clipboard: relative to the project, or with the argument "abs" the
// absolute path.
func (m ProjectModel) copyPath(argument string) (tea.Model, tea.Cmd) {
	path := m.editor.Path()
	switch {
	case path == "":
		m.say("E32: No file name", true)
		return m, nil
	case m.hooks.WriteClipboard == nil:
		m.say("No clipboard available", true)
		return m, nil
	case argument != "" && argument != "abs" && argument != "absolute":
		m.say("Usage: :copypath [abs]", true)
		return m, nil
	}
	text := m.relative(path)
	if argument != "" {
		text = path
	}
	if err := m.hooks.WriteClipboard(text); err != nil {
		m.say("Cannot copy: "+err.Error(), true)
		return m, nil
	}
	m.say("Copied "+text, false)
	return m, nil
}
