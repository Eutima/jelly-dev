package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/editor"
	"github.com/eutima/jelly-dev/internal/lsp"
)

// openedEditor is a project window with README.md ("# hi") open in the editor.
func openedEditor(t *testing.T) ProjectModel {
	t.Helper()
	model, _ := newProjectFixture(t, 120, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	if model.Editor().Path() == "" || model.Editor().Lines()[0] != "# hi" {
		t.Fatalf("setup: %q %v", model.Editor().Path(), model.Editor().Lines())
	}
	return model
}

func TestVimRemap_InTheProjectEditorACommandRunsOnItsNewKeyAndTheOldOneDoesNothing(t *testing.T) {
	remap(t, "vim.insert = f3\nvim.delete = x\nvim.deletechar = f4\nvim.top = - t\n")
	model := openedEditor(t)
	model, _ = projectKeys(model, "i")
	if model.Editor().vim.Mode().String() != "-- NORMAL --" {
		t.Fatal("i does nothing any more")
	}
	model, _ = projectKeys(model, "f3", "Z", "esc")
	if got := model.Editor().Lines()[0]; got != "Z# hi" {
		t.Fatalf("f3 inserts: %q", got)
	}
	model, _ = projectKeys(model, "f4") // the character under the cursor
	if got := model.Editor().Lines()[0]; got != "# hi" {
		t.Fatalf("f4 deletes a character: %q", got)
	}
	model, _ = projectKeys(model, "d", "d")
	if got := model.Editor().Lines()[0]; got != "# hi" {
		t.Fatalf("dd does nothing: %q", got)
	}
	model, _ = projectKeys(model, "x", "x") // the delete operator twice: the line
	if lines := model.Editor().Lines(); len(lines) > 0 && lines[0] != "" {
		t.Fatalf("xx deletes the line: %q", lines)
	}
}

func TestVimRemap_AKeySequenceRunsItsCommandAndShowsWhileItWaits(t *testing.T) {
	remap(t, "vim.bottom = - e\nvim.top = - t\n")
	model, _ := newProjectFixture(t, 120, map[string]string{"a.txt": "one\ntwo\nthree\n"})
	model, _ = projectKeys(model, "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	model, _ = projectKeys(model, "-")
	if view := stripANSI(model.View()); !strings.Contains(view, " -") || model.Editor().resolver.Pending() != "-" {
		t.Fatalf("the half typed sequence shows in the status line: %q", model.Editor().resolver.Pending())
	}
	model, _ = projectKeys(model, "e")
	if row, _ := model.Editor().Cursor(); row != 2 {
		t.Fatalf("- e goes to the last line: %d", row)
	}
	model, _ = projectKeys(model, "G") // the old key of vim.bottom
	model, _ = projectKeys(model, "-", "t")
	if row, _ := model.Editor().Cursor(); row != 0 {
		t.Fatalf("- t goes to the first line: %d", row)
	}
	model, _ = projectKeys(model, "g", "g", "-", "e", "g", "g")
	if row, _ := model.Editor().Cursor(); row != 2 {
		t.Fatalf("g g does nothing any more (it is shadowed, vim.top moved): %d", row)
	}
}

func TestVimRemap_TheEditorLevelSequencesAreRemappedToo(t *testing.T) {
	remap(t, "vim.definition = - d\n")
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	main := root + "/main.go"
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: main, Row: 2, Col: 5, Text: "func main() {"}}}
	model, _ = projectKeys(model, "j", "j", "j", "l")
	model, _ = projectKeys(model, "g", "d")
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
		t.Fatalf("g d is no definition any more: %d,%d", row, col)
	}
	model, _ = projectKeys(model, "-", "d")
	if row, col := model.Editor().Cursor(); row != 2 || col != 5 {
		t.Fatalf("- d goes to the definition: %d,%d", row, col)
	}
}

func TestVimRemap_TheStandaloneEditorFooterShowsTheKeysInForce(t *testing.T) {
	remap(t, "vim.definition = - d\nvim.references = - r\n")
	model := NewEditorModel(editor.Document{Lines: []string{"x"}, LineEnding: "\n"}).WithNavigator(&fakeNav{}, t.TempDir())
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	if view := stripANSI(updated.View()); !strings.Contains(view, "[-d] def") || !strings.Contains(view, "[-r] refs") {
		t.Fatalf("the footer names the new sequences:\n%s", view)
	}
}

func TestVimRemap_InsertModeKeysAreRemapped(t *testing.T) {
	remap(t, "insert.deletetostart = ctrl+x\n")
	model := openedEditor(t)
	model, _ = projectKeys(model, "A", "a", "b")
	model, _ = projectKeys(model, "ctrl+u")
	if got := model.Editor().Lines()[0]; got != "# hiab" {
		t.Fatalf("the old key does nothing: %q", got)
	}
	model, _ = projectKeys(model, "ctrl+x")
	if got := model.Editor().Lines()[0]; got != "" {
		t.Fatalf("the new key deletes to the start: %q", got)
	}
}

func TestVimRemap_TheGitDiffEditorUsesTheBindingsAndShowsThem(t *testing.T) {
	remap(t, "vim.insert = f3\nvim.visual = f5\n")
	model, _ := newEditModel(t, "a\nb\nc\n", "a\nB\nc\n")
	model = gitKey(model, "e")
	if model.editor == nil {
		t.Fatal("e opens the editor")
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "[f3] insert") || !strings.Contains(view, "[f5/V] visual") {
		t.Fatalf("the editor footer names the new keys:\n%s", view)
	}
	model = gitKey(model, "i")
	if model.editor.vim.Mode().String() != "-- NORMAL --" {
		t.Fatal("i does nothing any more")
	}
	model = gitKey(model, "f3")
	if model.editor.vim.Mode().String() != "-- INSERT --" {
		t.Fatal("f3 inserts")
	}
}

func TestVimRemap_TheMergeEditorUsesTheBindingsAndShowsThem(t *testing.T) {
	remap(t, "vim.insert = f3\nvim.append = f4\nvim.visualline = f6\n")
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n", "n", "e")
	if !model.editing {
		t.Fatal("e opens the editor")
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "[f3] insert") || !strings.Contains(view, "[v/f6] visual") {
		t.Fatalf("the editor footer names the new keys:\n%s", view)
	}
	model = pressKeyName(model, "i").(MergeModel)
	if model.vim.Mode().String() != "-- NORMAL --" {
		t.Fatal("i does nothing any more")
	}
	model = pressKeyName(model, "f3").(MergeModel)
	if model.vim.Mode().String() != "-- INSERT --" {
		t.Fatal("f3 inserts")
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(MergeModel).vim.Mode().String(); got != "-- NORMAL --" {
		t.Fatalf("esc leaves insert mode: %s", got)
	}
}

func TestVim_AnAltKeyNobodyBoundIsIgnoredAndLeavesAPendingOperatorAlone(t *testing.T) {
	model := openedEditor(t)
	model, _ = projectKeys(model, "d")
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if model.Editor().vim.Pending() != "d" {
		t.Fatalf("alt+x must not cancel the operator: %q", model.Editor().vim.Pending())
	}
	model, _ = projectKeys(model, "d")
	if lines := model.Editor().Lines(); len(lines) > 0 && lines[0] != "" {
		t.Fatalf("dd still deletes the line: %q", lines)
	}
}

func TestVim_AHalfTypedSequenceDoesNotFollowAFocusSwitchOrATabChange(t *testing.T) {
	remap(t, "vim.top = - t\n")
	model, _ := newProjectFixture(t, 120, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	model, _ = projectKeys(model, "-")
	if model.Editor().resolver.Pending() == "" {
		t.Fatal("setup: the sequence waits")
	}
	model, _ = projectKeys(model, "ctrl+e") // to the tree
	if model.Editor().resolver.Pending() != "" {
		t.Fatal("a focus switch drops the half typed sequence")
	}
}
