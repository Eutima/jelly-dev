package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

// refactorNav is a navigator that also renames: `main` in main.go (line 3) becomes the new name.
type refactorNav struct {
	fakeNav
	fileEdits lsp.WorkspaceEdit // what RenameFile adds
}

func (r *refactorNav) Rename(_ context.Context, path, _ string, _, _ int, newName string) (lsp.WorkspaceEdit, error) {
	edit := lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 9}}, NewText: newName}
	return lsp.WorkspaceEdit{Files: []lsp.FileEdit{{Path: path, Edits: []lsp.TextEdit{edit}}}}, nil
}

func (r *refactorNav) RenameFile(context.Context, string, string) (lsp.WorkspaceEdit, error) {
	return r.fileEdits, nil
}

// refactorFixture opens src/main.go with a renaming navigator.
func refactorFixture(t *testing.T) (ProjectModel, *projectFixture, *refactorNav) {
	t.Helper()
	model, fx := newProjectFixture(t, 100, sampleFiles)
	nav := &refactorNav{}
	model.hooks.Navigator = nav
	model.editor = model.editor.WithNavigator(nav, fx.root)
	if err := model.openFile(filepath.Join(fx.root, "src", "main.go"), false); err != nil {
		t.Fatal(err)
	}
	return model, fx, nav
}

// runRefactor runs a :refactor command, feeds the answer of the server back and returns the model.
func runRefactor(t *testing.T, model ProjectModel, command string) ProjectModel {
	t.Helper()
	updated, cmd := model.runProjectCommand(command)
	model = updated.(ProjectModel)
	if cmd != nil {
		updated, _ = model.Update(cmd())
		model = updated.(ProjectModel)
	}
	return model
}

func TestRefactor_RenamesAfterConfirmationAndUndoRestores(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	path := filepath.Join(fx.root, "src", "main.go")
	model = runRefactor(t, model, "refactor start")
	if model.dlg == nil || !strings.Contains(model.dlg.title, "rename to start") {
		t.Fatalf("the user must confirm first: %+v", model.dlg)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "func main()") {
		t.Fatal("nothing may be written before the confirmation")
	}
	model, _ = projectKeys(model, "y")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "func start()") || !strings.Contains(model.status, "undo") {
		t.Fatalf("file = %q, status = %q", data, model.status)
	}
	if got := strings.Join(model.editor.vim.Lines(), "\n"); !strings.Contains(got, "func start()") {
		t.Fatalf("the open tab must show the change: %q", got)
	}
	model = runRefactor(t, model, "refactor undo")
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "func main()") {
		t.Fatalf("undo must restore the file: %q", data)
	}
	if got := strings.Join(model.editor.vim.Lines(), "\n"); !strings.Contains(got, "func main()") {
		t.Fatalf("the open tab must show the undone text: %q", got)
	}
	if model = runRefactor(t, model, "refactor undo"); !strings.Contains(model.status, "no refactoring to undo") {
		t.Fatalf("status = %q", model.status)
	}
}

func TestRefactor_Cancel_WritesNothing(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	model = runRefactor(t, model, "refactor start")
	model, _ = projectKeys(model, "n")
	if data, _ := os.ReadFile(filepath.Join(fx.root, "src", "main.go")); !strings.Contains(string(data), "func main()") {
		t.Fatalf("cancel must not write: %q", data)
	}
}

func TestRefactor_Refuses_WhenATabHasUnsavedChanges(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc")
	model = runRefactor(t, model, "refactor start")
	if model.dlg != nil || !strings.Contains(stripANSI(model.View()), "save first") {
		t.Fatalf("dlg = %+v, status = %q", model.dlg, model.status)
	}
}

func TestRefactor_RenamesFileKeepingExtension_AndUpdatesTab(t *testing.T) {
	model, fx, nav := refactorFixture(t)
	other := filepath.Join(fx.root, "src", "util", "u.go")
	nav.fileEdits = lsp.WorkspaceEdit{Files: []lsp.FileEdit{{Path: other, Edits: []lsp.TextEdit{
		{Range: lsp.Range{End: lsp.Position{Character: 7}}, NewText: "package"},
	}}}}
	model = runRefactor(t, model, "refactor file entry")
	if model.dlg == nil || !strings.Contains(model.dlg.title, "entry.go") {
		t.Fatalf("dialog = %+v, status %q", model.dlg, model.status)
	}
	model, _ = projectKeys(model, "y")
	renamed := filepath.Join(fx.root, "src", "entry.go")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("file not renamed: %v (status %q)", err, model.status)
	}
	if model.editor.Path() != renamed {
		t.Fatalf("the tab must follow the file: %q", model.editor.Path())
	}
	model = runRefactor(t, model, "refactor undo")
	if _, err := os.Stat(filepath.Join(fx.root, "src", "main.go")); err != nil || model.editor.Path() != filepath.Join(fx.root, "src", "main.go") {
		t.Fatalf("undo must move it back: %v, tab %q", err, model.editor.Path())
	}
}

func TestRefactor_NoPromptAndNoLanguageServer_WhenNavigatorIsMissing(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if model = runRefactor(t, model, "refactor"); model.name != nil || !model.warn {
		t.Fatalf("without a language server no prompt may open: %q", model.status)
	}
	if model = runRefactor(t, model, "refactor two words"); !model.warn || !strings.Contains(model.status, "Usage") {
		t.Fatalf("a name with spaces must be refused: %q", model.status)
	}
	if model = runRefactor(t, model, "refactor name"); !strings.Contains(model.status, "No language server") {
		t.Fatalf("status = %q", model.status)
	}
}

func TestRefactor_OpensPromptWithTheSymbol_WhenNoNameIsGiven(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	path := filepath.Join(fx.root, "src", "main.go")
	model.editor.vim.SetCursor(2, 6) // on `main` in `func main()`
	model = runRefactor(t, model, "refactor")
	if model.name == nil || model.name.symbol != "main" || string(model.name.input) != "main" {
		t.Fatalf("prompt = %+v, status %q", model.name, model.status)
	}
	for range 4 {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		model = updated.(ProjectModel)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("start")})
	model = updated.(ProjectModel)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ProjectModel)
	if model.name != nil || cmd == nil {
		t.Fatalf("enter must start the rename: prompt %+v", model.name)
	}
	updated, _ = model.Update(cmd())
	model = updated.(ProjectModel)
	if model.dlg == nil || !strings.Contains(model.dlg.title, "rename to start") {
		t.Fatalf("the user must confirm first: %+v", model.dlg)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "func main()") {
		t.Fatal("nothing may be written before the confirmation")
	}
}

func TestWordAt_ReturnsTheIdentifier_WhenTheCursorIsOnOne(t *testing.T) {
	line := []rune("func main_x(a int)")
	if got := wordAt(line, 7); got != "main_x" {
		t.Fatalf("got %q", got)
	}
	if got := wordAt(line, 4); got != "" {
		t.Fatalf("a space is no symbol: %q", got)
	}
	if got := wordAt(line, len(line)); got != "" {
		t.Fatalf("past the line end is no symbol: %q", got)
	}
	if got := wordAt([]rune("größe = 1"), 2); got != "größe" {
		t.Fatalf("non-ASCII: got %q", got)
	}
}

func TestRefactor_FileWithoutName_OpensTheRenamePrompt(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	model = runRefactor(t, model, "refactor file")
	if model.name == nil || model.name.rename != filepath.Join(fx.root, "src", "main.go") || string(model.name.input) != "main.go" {
		t.Fatalf("prompt = %+v, status %q", model.name, model.status)
	}
}

func TestRefactor_NoPrompt_WhenNoSymbolIsUnderTheCursor(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.editor.vim.SetCursor(2, 4) // the space in `func main()`
	model = runRefactor(t, model, "refactor")
	if model.name != nil || !model.warn || !strings.Contains(model.status, "No symbol") {
		t.Fatalf("prompt = %+v, status %q", model.name, model.status)
	}
}

func TestRefactor_NoPrompt_WhenATabHasUnsavedChanges(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc")
	model.editor.vim.SetCursor(2, 6)
	model = runRefactor(t, model, "refactor")
	if model.name != nil || !model.editor.warn || !strings.Contains(model.editor.status, "E37") {
		t.Fatalf("prompt = %+v, status = %q", model.name, model.editor.status)
	}
}

func TestRefactor_SymbolPrompt_StaysOpen_WhenTheNameHasSpaces(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.editor.vim.SetCursor(2, 6)
	model = runRefactor(t, model, "refactor")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" two")})
	updated, cmd := updated.(ProjectModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ProjectModel)
	if model.name == nil || cmd != nil {
		t.Fatalf("the prompt must stay open to fix the name: %+v", model.name)
	}
}

func TestRefactor_SymbolPrompt_Closes_WhenTheNameIsUnchanged(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.editor.vim.SetCursor(2, 6)
	model = runRefactor(t, model, "refactor")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ProjectModel)
	if model.name != nil || model.dlg != nil || model.warn {
		t.Fatalf("an unchanged name starts nothing: %+v", model.name)
	}
}

func TestRefactor_FileWithoutName_SaysSaveFirst_WhenATabHasUnsavedChanges(t *testing.T) {
	model, _, _ := refactorFixture(t)
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc")
	model = runRefactor(t, model, "refactor file")
	if model.name != nil || !model.editor.warn || !strings.Contains(model.editor.status, "E37") {
		t.Fatalf("prompt = %+v, status = %q", model.name, model.editor.status)
	}
}

func TestRefactor_FileRenameOfEmptyName_IsRefused(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	model.focus = focusProjectEdit
	updated, cmd := model.startFileRefactorOf(filepath.Join(fx.root, "src", "main.go"), "")
	model = updated.(ProjectModel)
	if cmd != nil || !model.editor.warn || !strings.Contains(model.editor.status, "type a name") {
		t.Fatalf("status = %q", model.editor.status)
	}
}

func TestRename_ProjectFileFromTheTree_SaysSaveFirst_WhenATabHasUnsavedChanges(t *testing.T) {
	model, fx, _ := refactorFixture(t)
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc")
	model.focus = focusTree
	updated, cmd := model.renameFile(filepath.Join(fx.root, "src", "main.go"), "entry.go")
	model = updated.(ProjectModel)
	if cmd != nil || model.dlg != nil || !model.warn || !strings.Contains(model.status, "E37") {
		t.Fatalf("status = %q, dlg = %+v", model.status, model.dlg)
	}
	for file, want := range map[string]bool{"main.go": true, "entry.go": false} {
		if _, err := os.Stat(filepath.Join(fx.root, "src", file)); (err == nil) != want {
			t.Fatalf("%s must exist = %v, err = %v", file, want, err)
		}
	}
}
