package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/refactor"
)

// Refactorer renames through the language servers; the navigator of the window may implement it (without it :refactor says so).
type Refactorer interface {
	// Rename returns the edits that rename the symbol at a position (zero-based row, rune column) of text.
	Rename(ctx context.Context, path, text string, row, col int, newName string) (lsp.WorkspaceEdit, error)
	// RenameFile returns the edits that keep the code working when a file moves (none when the server does not know the file type).
	RenameFile(ctx context.Context, oldPath, newPath string) (lsp.WorkspaceEdit, error)
}

const refactorUsage = "Usage: :refactor <new name> (symbol under the cursor) | :refactor file <new name> | :refactor undo"

// refactorResultMsg is the answer of the language server to a rename.
type refactorResultMsg struct {
	label string
	edit  lsp.WorkspaceEdit
	err   error
	note  string // shown with the question, e.g. that the server had no import updates
}

func (m ProjectModel) refactorer() Refactorer {
	if m.hooks.Navigator == nil {
		return nil
	}
	r, _ := m.hooks.Navigator.(Refactorer)
	return r
}

// refactorCommand handles :refactor <name>, :refactor file <name> and :refactor undo.
func (m ProjectModel) refactorCommand(argument string) (tea.Model, tea.Cmd) {
	verb, rest, _ := strings.Cut(argument, " ")
	rest = strings.TrimSpace(rest)
	switch {
	case argument == "":
		m.say(refactorUsage, false)
		return m, nil
	case argument == "undo":
		return m.undoRefactor()
	case verb == "file":
		return m.startFileRefactor(rest)
	case strings.ContainsAny(argument, " \t"):
		m.say(refactorUsage, true)
		return m, nil
	}
	return m.startRename(argument)
}

// refactorBlocked says why a refactoring cannot start, true when it cannot: it changes files on disk, so unsaved changes would be
// lost or clash with it.
func (m *ProjectModel) refactorBlocked() bool {
	if i := m.firstModified(); i >= 0 {
		m.say("E37: unsaved changes in "+m.tabNames()[i]+": save first (:wa)", true)
		return true
	}
	return false
}

// startRename asks the language server to rename the symbol under the cursor.
func (m ProjectModel) startRename(newName string) (tea.Model, tea.Cmd) {
	refactorer := m.refactorer()
	path := m.editor.Path()
	switch {
	case refactorer == nil:
		m.say("E1: No language server available in this editor", true)
		return m, nil
	case path == "":
		m.say("E32: No file name: open the file with the symbol first", true)
		return m, nil
	case m.refactorBlocked():
		return m, nil
	}
	row, col := m.editor.vim.Cursor()
	text := strings.Join(m.editor.vim.Lines(), "\n")
	m.say("Asking the language server to rename to "+newName+"...", false)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		edit, err := refactorer.Rename(ctx, path, text, row, col, newName)
		return refactorResultMsg{label: "rename to " + newName, edit: edit, err: err}
	}
}

// startFileRefactor renames the file of the shown tab; the language server may add the edits of the imports.
func (m ProjectModel) startFileRefactor(name string) (tea.Model, tea.Cmd) {
	path := m.editor.Path()
	switch {
	case path == "":
		m.say("E32: No file name: nothing to rename", true)
		return m, nil
	case name == "":
		m.say(refactorUsage, true)
		return m, nil
	case m.refactorBlocked():
		return m, nil
	}
	if filepath.Ext(name) == "" {
		name += filepath.Ext(path) // `:refactor file util` keeps the extension
	}
	target, folder, err := m.resolveNewPath(filepath.Dir(path), name)
	if err != nil || folder {
		m.say("Cannot rename: "+errorText(err, "a file name is needed"), true)
		return m, nil
	}
	label := "rename file " + filepath.Base(path) + " to " + filepath.Base(target)
	refactorer := m.refactorer()
	m.say("Preparing "+label+"...", false)
	return m, func() tea.Msg {
		edit := lsp.WorkspaceEdit{}
		note := ""
		if refactorer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
			defer cancel()
			switch found, err := refactorer.RenameFile(ctx, path, target); {
			case err == nil:
				edit = found
			default: // no server or one that fails: the file is still renamed
				note = " (the imports are not updated: " + err.Error() + ")"
			}
		}
		edit.Renames = append(edit.Renames, lsp.FileRename{Old: path, New: target})
		return refactorResultMsg{label: label, edit: edit, note: note}
	}
}

func errorText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

// refactorResult asks the user to confirm what the language server wants to change.
func (m ProjectModel) refactorResult(msg refactorResultMsg) (tea.Model, tea.Cmd) {
	var missing *lsp.NotInstalledError
	switch {
	case errors.As(msg.err, &missing):
		m.say(msg.err.Error(), true)
		return m, nil
	case errors.Is(msg.err, lsp.ErrNoServer):
		m.say("No language server for this file type (add one in lsp.json)", true)
		return m, nil
	case errors.Is(msg.err, lsp.ErrCannotRename):
		m.say("The language server cannot rename symbols", true)
		return m, nil
	case msg.err != nil:
		m.say("Language server: "+msg.err.Error(), true)
		return m, nil
	case msg.edit.Empty():
		m.say("Nothing to rename here", true)
		return m, nil
	case m.refactorBlocked(): // something was edited while the server worked
		return m, nil
	}
	edit, label := msg.edit, msg.label
	summary := fmt.Sprintf("%d edit(s) in %d file(s)", edit.EditCount(), len(edit.Files))
	if len(edit.Renames) > 0 {
		summary += fmt.Sprintf(", %d file(s) renamed", len(edit.Renames))
	}
	m.say("", false)
	m.dlg = &unsavedDialog{
		title:   "Refactor: " + label + " (" + summary + ")?",
		hint:    " y applies, n or esc cancels; :refactor undo reverts it" + msg.note,
		buttons: closeButtons,
		then:    func(next ProjectModel) (tea.Model, tea.Cmd) { return next.applyRefactor(label, edit) },
	}
	return m, nil
}

// applyRefactor writes the edit to the files and shows the result in the open tabs.
func (m ProjectModel) applyRefactor(label string, edit lsp.WorkspaceEdit) (tea.Model, tea.Cmd) {
	if m.refactorBlocked() {
		return m, nil
	}
	result, err := refactor.Open(m.root).Apply(label, edit)
	if err != nil {
		m.say("Refactoring failed: "+err.Error(), true)
		return m, nil
	}
	m = m.refreshAfterRefactor(result.Moved, false)
	m.say(fmt.Sprintf("✓ %s: %d edit(s) in %d file(s), %d renamed - :refactor undo reverts", label, result.Edits, result.Files, result.Moves), false)
	return m, nil
}

// undoRefactor reverts the newest refactoring.
func (m ProjectModel) undoRefactor() (tea.Model, tea.Cmd) {
	if m.refactorBlocked() {
		return m, nil
	}
	result, err := refactor.Open(m.root).Undo()
	switch {
	case errors.Is(err, refactor.ErrNothingToUndo):
		m.say("There is no refactoring to undo", true)
		return m, nil
	case err != nil:
		m.say("Cannot undo: "+err.Error(), true)
		return m, nil
	}
	m = m.refreshAfterRefactor(result.Moved, true)
	m.say(fmt.Sprintf("✓ Undone: %s (%d file(s) restored, %d renamed back)", result.Label, result.Files, result.Moves), false)
	return m, nil
}

// refreshAfterRefactor brings the tabs and the tree in line with the files: renamed files are reopened under their new name (their old
// one when undone), the others are read again.
func (m ProjectModel) refreshAfterRefactor(moved []refactor.Move, undone bool) ProjectModel {
	shown := m.editor.Path()
	for _, move := range moved {
		from, to := move.Old, move.New
		if undone {
			from, to = move.New, move.Old
		}
		if i := m.tabIndex(from); i >= 0 {
			wasShown := from == shown
			m.closeTab(i)
			if wasShown {
				_ = m.openFile(to, false)
			}
		}
	}
	m.reloadChanged()
	m.tree.Refresh()
	return m
}
