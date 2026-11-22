package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/source"
)

// writableSource is a fakeSource whose new side is an in-memory working tree.
type writableSource struct {
	fakeSource
	disk   map[string]string
	writes int
}

func (w *writableSource) Load(ctx context.Context, file source.ChangedFile) (string, string, error) {
	return w.texts[file.Path][0], w.disk[file.Path], nil
}

func (w *writableSource) Editable(source.ChangedFile) bool { return true }

func (w *writableSource) WriteWorktree(_ context.Context, file source.ChangedFile, expected, text string) error {
	if w.disk[file.Path] != expected {
		return source.ErrChangedOnDisk
	}
	w.disk[file.Path] = text
	w.writes++
	return nil
}

func newEditModel(t *testing.T, oldText, newText string) (GitModel, *writableSource) {
	t.Helper()
	src := &writableSource{
		fakeSource: fakeSource{texts: map[string][2]string{"a.txt": {oldText, newText}}},
		disk:       map[string]string{"a.txt": newText},
	}
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}
	updated, _ := quietGit(files, src).Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model := settle(updated.(GitModel))
	return gitPress(gitPress(model, "enter"), "down"), src // the diff has the focus, the first change is selected
}

func TestGitEdit_RevertsSelectedChange_WhenRPressed(t *testing.T) {
	model, src := newEditModel(t, "a\nb\nc\nd\ne\n", "a\nB\nc\nd\nE\n")
	model = gitPress(model, "r")
	if src.disk["a.txt"] != "a\nb\nc\nd\nE\n" {
		t.Fatalf("file = %q", src.disk["a.txt"])
	}
	if model.diff.HunkCount() != 1 {
		t.Fatalf("the reverted change must be gone: %d changes", model.diff.HunkCount())
	}
}

func TestGitEdit_RevertsAddedAndDeletedLines(t *testing.T) {
	model, src := newEditModel(t, "a\nb\nc\n", "a\nx\ny\nb\n")
	model = gitPress(model, "r") // the insertion of x, y
	model = gitPress(model, "down")
	if src.disk["a.txt"] != "a\nb\n" {
		t.Fatalf("after reverting the insertion: %q", src.disk["a.txt"])
	}
	model = gitPress(model, "r") // the deleted c comes back
	if src.disk["a.txt"] != "a\nb\nc\n" {
		t.Fatalf("after reverting the deletion: %q", src.disk["a.txt"])
	}
	if model.diff.HunkCount() != 0 {
		t.Fatalf("%d changes left", model.diff.HunkCount())
	}
}

func TestGitEdit_UndoRestoresFile_WhenUPressed(t *testing.T) {
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	model = gitPress(model, "r")
	model = gitPress(model, "u")
	if src.disk["a.txt"] != "a\nB\n" || model.diff.HunkCount() != 1 {
		t.Fatalf("file = %q, %d changes", src.disk["a.txt"], model.diff.HunkCount())
	}
	model = gitPress(model, "u")
	if !strings.Contains(model.View(), "Nothing to undo") {
		t.Fatal("a second undo must say there is nothing to undo")
	}
}

func TestGitEdit_KeepsCrlfAndMissingFinalNewline(t *testing.T) {
	model, src := newEditModel(t, "a\r\nb", "a\r\nB")
	gitPress(model, "r")
	if src.disk["a.txt"] != "a\r\nb" {
		t.Fatalf("file = %q", src.disk["a.txt"])
	}
}

func TestGitEdit_Refuses_WhenFileChangedOnDisk(t *testing.T) {
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	src.disk["a.txt"] = "changed by someone else\n"
	model = gitPress(model, "r")
	if src.disk["a.txt"] != "changed by someone else\n" || src.writes != 0 {
		t.Fatalf("the file must not be overwritten: %q", src.disk["a.txt"])
	}
	if !strings.Contains(model.View(), "changed on disk") {
		t.Fatal("the refusal must be shown")
	}
}

func TestGitEdit_NeedsASelectedChange_ForRevert(t *testing.T) {
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	model.diff.current = -1
	model = gitPress(model, "r")
	if src.writes != 0 || !strings.Contains(model.View(), "Select a change") {
		t.Fatal("revert without a selected change must say so and write nothing")
	}
}

func TestGitEdit_ReadOnlySource_OffersNothing(t *testing.T) {
	model, _ := newGitTestModel(100, 20)
	model = gitPress(gitPress(model, "enter"), "down")
	model = gitPress(model, "r")
	if strings.Contains(model.View(), "[r]") || !strings.Contains(model.View(), "cannot be changed") {
		t.Fatalf("a read-only comparison must not offer revert:\n%s", model.View())
	}
}

func typeKeys(model GitModel, keys ...string) GitModel {
	for _, key := range keys {
		model = gitPress(model, key)
	}
	return model
}

func TestGitEdit_EditorSavesToFile_WhenWritten(t *testing.T) {
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	model = gitPress(model, "e")
	if model.editor == nil {
		t.Fatal("e must open the editor")
	}
	// the cursor is on the changed line: replace it with "X"
	model = typeKeys(model, "c", "c", "X", "esc", ":", "w", "enter")
	if src.disk["a.txt"] != "a\nX\n" {
		t.Fatalf("file = %q", src.disk["a.txt"])
	}
	model = typeKeys(model, ":", "q", "enter")
	if model.editor != nil {
		t.Fatal(":q must leave the editor after saving")
	}
	model = gitPress(model, "u")
	if src.disk["a.txt"] != "a\nB\n" {
		t.Fatalf("undo must restore the file: %q", src.disk["a.txt"])
	}
}

func TestGitEdit_EditorRefusesToLeave_WhenUnsaved(t *testing.T) {
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	model = typeKeys(gitPress(model, "e"), "x", ":", "q", "enter")
	if model.editor == nil {
		t.Fatal(":q must refuse while there are unsaved changes")
	}
	model = typeKeys(model, ":", "q", "!", "enter")
	if model.editor != nil || src.writes != 0 {
		t.Fatalf(":q! must leave without writing (editor open: %v, writes: %d)", model.editor != nil, src.writes)
	}
}
