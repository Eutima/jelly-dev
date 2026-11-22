package ui

import (
	"github.com/eutima/jelly-dev/internal/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runNote runs an ex command of the project window, as `:` does once the editor handed it over.
func runNote(model ProjectModel, command string) ProjectModel {
	updated, _ := model.runProjectCommand(command)
	return updated.(ProjectModel)
}

func TestNote_OpensNoteInProjectAndSavesIt_WhenNoNoteProjectChosen(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model = runNote(model, "note ideas")
	want := filepath.Join(fx.root, ".jd", "notes", "ideas.md")
	if model.Editor().Path() != want || model.focus != focusProjectEdit {
		t.Fatalf("note path %q focus %d, want %q in the editor", model.Editor().Path(), model.focus, want)
	}
	model, _ = projectKeys(model, "i", "h", "i", "esc")
	model, _ = typeCommand(model, "w")
	data, err := os.ReadFile(want)
	if err != nil || !strings.HasPrefix(string(data), "hi") {
		t.Fatalf("saved note = %q, %v", data, err)
	}
}

func TestNote_OpensNoteOfOtherProject_WhenNoteProjectIsChosen(t *testing.T) {
	other := t.TempDir()
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.NoteRoot = func() string { return other }
	model = runNote(model, "note")
	if model.Editor().Path() != filepath.Join(other, ".jd", "notes", "notes.md") {
		t.Fatalf("note path %q", model.Editor().Path())
	}
}

func TestNote_Refuses_WhenNameHasAFolder(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model = runNote(model, "note ../x")
	if model.Editor().Path() != "" {
		t.Fatalf("opened %q", model.Editor().Path())
	}
	if _, err := os.Stat(filepath.Join(fx.root, ".jd", "x.md")); err == nil {
		t.Fatal("a note left the notes folder")
	}
}

func TestNote_ProjectCommandOpensPickerThatStoresChoice(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	var stored string
	model.hooks.SetNoteRoot = func(root string) { stored = root }
	model = runNote(model, "note project")
	if model.picker == nil || !model.pickingNotes {
		t.Fatal(":note project must open the project list for the notes")
	}
	other := t.TempDir()
	updated, _ := model.chooseNoteProject(other)
	got := updated.(ProjectModel)
	if stored != other || got.Editor().Path() != filepath.Join(other, ".jd", "notes", "notes.md") {
		t.Fatalf("stored %q, editor %q", stored, got.Editor().Path())
	}
}

func TestCopyPath_CopiesRelativeOrAbsolutePath_OfTheShownFile(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	var copied string
	model.hooks.WriteClipboard = func(text string) error { copied = text; return nil }
	model = runNote(model, "copypath")
	if copied != "" || !model.warn {
		t.Fatalf("without a file nothing is copied: %q", copied)
	}
	if err := model.openFile(filepath.Join(fx.root, "src", "main.go"), false); err != nil {
		t.Fatal(err)
	}
	model = runNote(model, "copypath")
	if copied != filepath.Join("src", "main.go") {
		t.Fatalf("relative path = %q", copied)
	}
	model = runNote(model, "copypath abs")
	if copied != filepath.Join(fx.root, "src", "main.go") {
		t.Fatalf("absolute path = %q", copied)
	}
}

func TestNote_ProjectChoiceDoesNotAskForANewWindow(t *testing.T) {
	other := t.TempDir()
	model, _ := newProjectFixture(t, 100, sampleFiles) // the fixture can open windows: a normal choice would ask
	model.hooks.Recent = func() []project.Entry { return []project.Entry{{Path: other}} }
	var stored string
	model.hooks.SetNoteRoot = func(root string) { stored = root }
	model = runNote(model, "note project")
	if view := stripANSI(model.View()); !strings.Contains(view, "Project of the notes") {
		t.Fatalf("the list must say what it chooses:\n%s", view)
	}
	model, _ = projectKeys(model, "enter")
	if model.picker != nil || stored != other {
		t.Fatalf("enter must choose the project without a question: picker %v, stored %q", model.picker, stored)
	}
	if got := model.Editor().Path(); got != filepath.Join(other, ".jd", "notes", "notes.md") {
		t.Fatalf("the note of the chosen project must open: %q", got)
	}
}

func TestNote_TopBarNamesTheProjectOfTheShownNote(t *testing.T) {
	other := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if view := stripANSI(model.View()); strings.Contains(view, "Note of") {
		t.Fatalf("no note is shown:\n%s", view)
	}
	model.hooks.NoteRoot = func() string { return other }
	model = runNote(model, "note")
	first := strings.SplitN(stripANSI(model.View()), "\n", 2)[0]
	if !strings.Contains(first, "Note of shop") {
		t.Fatalf("the top bar must name the project: %q", first)
	}
}

func TestTabs_ShowEachFileOnce_AndNameSameNamedNotesByProject(t *testing.T) {
	other := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	model, fx := newProjectFixture(t, 120, sampleFiles)
	model = runNote(model, "note") // the note of the open project
	model.hooks.NoteRoot = func() string { return other }
	model = runNote(model, "note") // the note of another project: the same file name
	names := model.tabNames()
	if len(names) != 2 || names[0] == names[1] || !strings.Contains(strings.Join(names, " "), "shop/notes.md") {
		t.Fatalf("tab names = %q", names)
	}
	model = runNote(model, "tabs")
	view := stripANSI(model.View())
	if strings.Contains(view, "..") {
		t.Fatalf("no row of ../ in the tab list:\n%s", view)
	}
	if strings.Count(view, ".jd/notes/notes.md") != 1 || strings.Count(view, fx.root) > 0 {
		t.Fatalf("each file must appear once:\n%s", view)
	}
}

func TestCloseAll_ClosesEveryTabAndFocusesTheTree(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	for _, name := range []string{"README.md", filepath.Join("src", "main.go")} {
		if err := model.openFile(filepath.Join(fx.root, name), false); err != nil {
			t.Fatal(err)
		}
	}
	model.focus = focusProjectEdit
	model = runNote(model, "closeall")
	if got := len(model.openEditors()); got != 1 || model.Editor().Path() != "" || model.focus != focusTree {
		t.Fatalf("tabs %d, path %q, focus %d: want one empty buffer and the tree", got, model.Editor().Path(), model.focus)
	}
}

func TestCloseAll_AsksAboutUnsavedChanges_AndKeepsTabsOnCancel(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	if err := model.openFile(filepath.Join(fx.root, "README.md"), false); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc")
	model = runNote(model, "closeall")
	if model.dlg == nil {
		t.Fatal("unsaved changes must be asked about")
	}
	model, _ = projectKeys(model, "esc")
	if model.Editor().Path() == "" {
		t.Fatal("cancel must keep the tab")
	}
}

func TestCloseOther_KeepsTheShownTabOnly(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	for _, name := range []string{"README.md", filepath.Join("src", "main.go"), filepath.Join("src", "util", "u.go")} {
		if err := model.openFile(filepath.Join(fx.root, name), false); err != nil {
			t.Fatal(err)
		}
	}
	model.selectTab(1) // README.md is the first tab, main.go the second
	keep := model.Editor().Path()
	model = runNote(model, "closeother")
	if got := model.openEditors(); len(got) != 1 || got[0].Path() != keep || model.Editor().Path() != keep {
		t.Fatalf("tabs %d, shown %q, want only %q", len(got), model.Editor().Path(), keep)
	}
}

func TestCloseOther_AsksAboutUnsavedChangesOfTheOthers_AndKeepsTheShownTab(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	for _, name := range []string{"README.md", filepath.Join("src", "main.go")} {
		if err := model.openFile(filepath.Join(fx.root, name), false); err != nil {
			t.Fatal(err)
		}
	}
	model.selectTab(0)
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "i", "x", "esc") // the first tab has changes
	model.selectTab(1)
	keep := model.Editor().Path()
	model = runNote(model, "closeother")
	if model.dlg == nil {
		t.Fatal("the changes of the other tab must be asked about")
	}
	model, _ = projectKeys(model, "d") // discard
	if got := model.openEditors(); len(got) != 1 || got[0].Path() != keep {
		t.Fatalf("after discarding: %d tabs, shown %q, want %q", len(got), model.Editor().Path(), keep)
	}
}
