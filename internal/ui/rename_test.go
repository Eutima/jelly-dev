package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRename_ScratchFileKeepsItsFolderAndFollowsTheTab(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch try.py")
	model = runNote(model, "rename better.py")
	want := filepath.Join(dir, "better.py")
	if model.Editor().Path() != want {
		t.Fatalf("shown %q, want %q", model.Editor().Path(), want)
	}
	if _, err := os.Stat(filepath.Join(dir, "try.py")); err == nil {
		t.Fatal("the old name must be gone")
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestRename_KeepsTheExtensionAndRefusesFoldersAndExistingNames(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch a.py")
	model = runNote(model, "scratch b.py")
	model = runNote(model, "rename a.py") // taken
	if model.Editor().Path() != filepath.Join(dir, "b.py") {
		t.Fatalf("renamed onto an existing file: %q", model.Editor().Path())
	}
	model = runNote(model, "rename ../x")
	if model.Editor().Path() != filepath.Join(dir, "b.py") {
		t.Fatalf("a folder in the name: %q", model.Editor().Path())
	}
	model = runNote(model, "rename c")
	if model.Editor().Path() != filepath.Join(dir, "c.py") {
		t.Fatalf("shown %q, want c.py", model.Editor().Path())
	}
}

func TestRename_PromptIsPrefilledAndRenamesOnEnter(t *testing.T) {
	model, dir := newScratchFixture(t)
	model = runNote(model, "scratch old.txt")
	model = runNote(model, "rename")
	if model.name == nil || string(model.name.input) != "old.txt" {
		t.Fatalf("prompt %+v", model.name)
	}
	model, _ = projectKeys(model, "backspace", "backspace", "backspace", "m", "d", "enter")
	if model.name != nil || model.Editor().Path() != filepath.Join(dir, "old.md") {
		t.Fatalf("shown %q prompt %v", model.Editor().Path(), model.name)
	}
}

func TestRename_ProjectFileAsksBeforeRenaming(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter") // README.md
	updated, cmd := model.runProjectCommand("rename GUIDE")
	model = settleProject(updated.(ProjectModel), cmd)
	if model.dlg == nil {
		t.Fatalf("a project file is renamed through the refactoring, which asks first (notice %q)", model.editor.status)
	}
}

func TestRename_TreeKeyRenamesTheSelectedFile(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "r")
	if model.name == nil || model.name.rename == "" {
		t.Fatal("r on a file in the tree must open the rename prompt")
	}
}
