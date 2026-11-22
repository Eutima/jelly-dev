package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestRefactor_UsageAndMissingNavigator(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if model = runRefactor(t, model, "refactor"); !strings.Contains(model.status, "Usage") {
		t.Fatalf("status = %q", model.status)
	}
	if model = runRefactor(t, model, "refactor two words"); !model.warn {
		t.Fatalf("a name with spaces must be refused: %q", model.status)
	}
	if model = runRefactor(t, model, "refactor name"); !strings.Contains(model.status, "No language server") && !strings.Contains(model.status, "No file name") {
		t.Fatalf("status = %q", model.status)
	}
}
