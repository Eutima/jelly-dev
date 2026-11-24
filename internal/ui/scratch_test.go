package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newScratchFixture(t *testing.T) (ProjectModel, string) {
	t.Helper()
	model, _ := newProjectFixture(t, 100, sampleFiles)
	dir := filepath.Join(t.TempDir(), "scratches")
	model.hooks.ScratchDir = func() string { return dir }
	return model, dir
}

func TestScratch_CreatesTheTypedFileAndOpensItOutsideTheProject(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch")
	if model.scratch == nil {
		t.Fatal(":scratch must open the list")
	}
	model, _ = projectKeys(model, "t", "r", "y", ".", "p", "y", "enter")
	want := filepath.Join(dir, "try.py")
	if model.scratch != nil || model.Editor().Path() != want || model.focus != focusProjectEdit {
		t.Fatalf("path %q focus %d scratch %v, want %q in the editor", model.Editor().Path(), model.focus, model.scratch, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the new file must exist at once: %v", err)
	}
}

func TestScratch_ListsExistingFilesAndOpensTheChosenOne(t *testing.T) {
	model, dir := newScratchFixture(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "todo.md"), []byte("buy milk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model = runNote(model, "scratch")
	model, _ = projectKeys(model, "t", "o", "d", "o")
	if got := model.scratch.nameOf(model.scratch.picker.Chosen()); got != "todo.md" && got != "todo.txt" {
		t.Fatalf("first entry = %q", got)
	}
	// the typed text "todo" creates todo.txt (first); the existing todo.md is the second entry
	model, _ = projectKeys(model, "down", "enter")
	if model.Editor().Path() != filepath.Join(dir, "todo.md") {
		t.Fatalf("opened %q", model.Editor().Path())
	}
	if !strings.Contains(strings.Join(model.Editor().Lines(), "\n"), "buy milk") {
		t.Fatalf("text %q", model.Editor().Lines())
	}
}

func TestScratch_RefusesAFolderInTheName(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch ../x")
	if model.Editor().Path() != "" {
		t.Fatalf("opened %q", model.Editor().Path())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "x.txt")); err == nil {
		t.Fatal("a scratch file left the scratch folder")
	}
}

func TestScratch_EscClosesTheListWithoutCreatingAnything(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch")
	model, _ = projectKeys(model, "esc")
	if model.scratch != nil {
		t.Fatal("esc must close the list")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("nothing may be created")
	}
}

func TestScratch_PaletteOffersIt(t *testing.T) {
	model, _ := newScratchFixture(t)
	for _, action := range model.paletteActions() {
		if strings.Contains(action.label, "scratch") {
			return
		}
	}
	t.Fatal("the palette has no scratch entry")
}

func TestScratch_TypedCommandOpensTheList(t *testing.T) {
	model, _ := newScratchFixture(t)
	model, _ = projectKeys(model, "G", "enter", "esc") // README.md opens, the editor has the focus
	model, _ = typeCommand(model, "scratch")
	if model.scratch == nil {
		t.Fatal(":scratch typed in the window must open the list")
	}
	if !strings.Contains(model.View(), "Scratch files") {
		t.Fatalf("the dialog is not shown:\n%s", model.View())
	}
}

func TestScratch_AsksForTheFileType_WhenTheNewNameHasNoEnding(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch")
	model, _ = projectKeys(model, "t", "r", "y", "enter")
	if model.scratch == nil || model.scratch.stem != "try" {
		t.Fatalf("a name without an ending asks for the type: %+v", model.scratch)
	}
	if _, err := os.Stat(filepath.Join(dir, "try.txt")); err == nil {
		t.Fatal("nothing is created before the type is chosen")
	}
	model, _ = projectKeys(model, "enter") // the first type: .py
	if want := filepath.Join(dir, "try.py"); model.scratch != nil || model.Editor().Path() != want {
		t.Fatalf("path %q, want %q", model.Editor().Path(), want)
	}
}

func TestScratch_AsksForTheFileType_WhenCommandNameHasNoEnding(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch todo")
	if model.scratch == nil || model.scratch.stem != "todo" {
		t.Fatalf("scratch %+v", model.scratch)
	}
	model, _ = projectKeys(model, "esc")
	if model.scratch != nil {
		t.Fatal("esc cancels")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("cancelled: nothing created")
	}
}

func TestScratch_FiltersTheFileTypes_WhenTypingInTheTypeList(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch")
	model, _ = projectKeys(model, "enter") // New scratch file, no name typed
	if model.scratch == nil || model.scratch.stem == "" {
		t.Fatalf("the type list must open: %+v", model.scratch)
	}
	model, _ = projectKeys(model, "m", "d", "enter")
	path := model.Editor().Path()
	if model.scratch != nil || filepath.Dir(path) != dir || !strings.HasSuffix(path, ".md") {
		t.Fatalf("path %q scratch %v", path, model.scratch)
	}
}

func TestScratch_DeletesTheChosenFile_WhenDeletePressedAndConfirmed(t *testing.T) {
	model, dir := newScratchFixture(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "todo.md")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model = runNote(model, "scratch")
	model, _ = projectKeys(model, "down", "delete") // the first entry is "New scratch file"
	if model.scratch == nil || model.scratch.deleting != "todo.md" {
		t.Fatalf("delete asks first: %+v", model.scratch)
	}
	model, _ = projectKeys(model, "enter") // "No, keep it"
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the default answer keeps the file: %v", err)
	}
	model, _ = projectKeys(model, "down", "delete", "down", "enter")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the file must be gone")
	}
	if model.scratch == nil || model.scratch.deleting != "" {
		t.Fatal("the list opens again")
	}
}
