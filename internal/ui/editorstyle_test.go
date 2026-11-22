package ui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
)

// withEditorStyle installs a style for the editors made in the test and restores the old one afterwards.
func withEditorStyle(t *testing.T, style config.EditorStyle) {
	t.Helper()
	before := CurrentEditorStyle()
	UseEditorStyle(style)
	t.Cleanup(func() { UseEditorStyle(before) })
}

// producesMessage runs a command (and the commands of a batch) and reports whether one of the messages is wanted.
func producesMessage(cmd tea.Cmd, wanted func(tea.Msg) bool) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if wanted(msg) {
		return true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if producesMessage(c, wanted) {
				return true
			}
		}
	}
	return false
}

func TestEditorStyle_NewEditorsGetTheStyleInForce(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, fx := newProjectFixture(t, 120, sampleFiles)
	if err := model.openFile(filepath.Join(fx.root, "README.md"), false); err != nil {
		t.Fatal(err)
	}
	if model.Editor().Style() != config.EditorNano {
		t.Fatalf("style %q", model.Editor().Style())
	}
	withEditorStyle(t, config.EditorVim)
	other, _ := newProjectFixture(t, 120, sampleFiles)
	if other.Editor().Style() != config.EditorVim {
		t.Fatalf("style %q", other.Editor().Style())
	}
}

func TestEditorCommand_SwitchesEveryOpenFile_AndRemembersTheChoice(t *testing.T) {
	withEditorStyle(t, config.EditorVim)
	model, fx := newProjectFixture(t, 120, sampleFiles)
	for _, name := range []string{"README.md", filepath.Join("src", "main.go")} {
		if err := model.openFile(filepath.Join(fx.root, name), false); err != nil {
			t.Fatal(err)
		}
	}
	var saved config.EditorStyle
	model.hooks.SetEditor = func(style config.EditorStyle) { saved = style }
	model = runNote(model, "editor nano")
	if saved != config.EditorNano || CurrentEditorStyle() != config.EditorNano {
		t.Fatalf("saved %q, in force %q", saved, CurrentEditorStyle())
	}
	for i, e := range model.openEditors() {
		if e.Style() != config.EditorNano {
			t.Errorf("tab %d is still %q", i, e.Style())
		}
	}
	model = runNote(model, "editor vim")
	for i, e := range model.openEditors() {
		if e.Style() != config.EditorVim || e.vim.Mode().String() != "-- NORMAL --" {
			t.Errorf("tab %d: %q %v", i, e.Style(), e.vim.Mode())
		}
	}
}

func TestEditorCommand_SaysTheStyle_AndRefusesANameThatIsNone(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, _ := newProjectFixture(t, 120, sampleFiles)
	if got := runNote(model, "editor"); !strings.Contains(got.status, "nano style") {
		t.Fatalf("status %q", got.status)
	}
	got := runNote(model, "editor emacs")
	if !got.warn || CurrentEditorStyle() != config.EditorNano {
		t.Fatalf("an unknown style changes nothing: warn %v style %q", got.warn, CurrentEditorStyle())
	}
}

func TestEditorStyle_PaletteOffersBothStyles(t *testing.T) {
	model, _ := newProjectFixture(t, 120, sampleFiles)
	labels := map[string]bool{}
	for _, action := range model.paletteActions() {
		labels[action.label] = true
	}
	if !labels["Editor: nano style (no modes, keys shown at the bottom)"] || !labels["Editor: vim style (modal)"] {
		t.Fatalf("palette %v", labels)
	}
}

func TestNanoInTheProjectWindow_TheWindowKeysStillWork(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, fx := newProjectFixture(t, 120, sampleFiles)
	if err := model.openFile(filepath.Join(fx.root, "README.md"), false); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	model, _ = projectKeys(model, "h", "i")
	if got := strings.Join(model.Editor().vim.Lines(), "\n"); !strings.HasPrefix(got, "hi") {
		t.Fatalf("typing in the project window: %q", got)
	}
	model = sendKey(model, keyMsg("ctrl+f")) // the file search of the window
	if model.search == nil {
		t.Fatal("ctrl+f opens the search of the window also in the nano style")
	}
	model.search = nil
	model = sendKey(model, keyMsg("ctrl+a")) // the palette
	if model.palette == nil {
		t.Fatal("ctrl+a opens the palette")
	}
}

func TestNanoInTheProjectWindow_CtrlDAsksBeforeClosingAndCtrlXClosesTheTab(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, fx := newProjectFixture(t, 120, sampleFiles)
	for _, name := range []string{"README.md", filepath.Join("src", "main.go")} {
		if err := model.openFile(filepath.Join(fx.root, name), false); err != nil {
			t.Fatal(err)
		}
	}
	model.focus = focusProjectEdit
	_, cmd := model.Update(keyMsg("ctrl+d"))
	if cmd == nil {
		t.Fatal("ctrl+d asks the window to close the tab")
	}
	if !producesMessage(cmd, func(msg tea.Msg) bool { _, ok := msg.(CloseRequestMsg); return ok }) {
		t.Fatal("ctrl+d: the window is asked to close the tab")
	}
	tabs := len(model.openEditors())
	model, _ = projectKeys(model, "ctrl+x")
	if got := len(model.openEditors()); got != tabs-1 {
		t.Fatalf("ctrl+x closes the shown tab of an unchanged file: %d tabs, was %d", got, tabs)
	}
}

// ---- the merge and the git-diff editor in the nano style

func nanoPressMerge(model MergeModel, keys ...string) MergeModel {
	for _, key := range keys {
		updated, _ := model.Update(keyMsg(key))
		model = updated.(MergeModel)
	}
	return model
}

func TestNano_MergeEditorEditsApplyWithCtrlSAndLeaveWithCtrlX(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, s, outcome := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "e")
	if !model.editing || model.nano == nil {
		t.Fatal("the editor opens in the nano style")
	}
	model = nanoPressMerge(model, "ctrl+k")           // cut the line "a"
	model = nanoPressMerge(model, "x", "y", "ctrl+s") // type into the next line and apply
	want := split("xyb\nc\nd\ne\nf\n")
	if !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("result %q, want %q", model.Result(), want)
	}
	model = nanoPressMerge(model, "ctrl+x") // applied: nothing to ask
	if model.editing {
		t.Fatal("ctrl+x leaves the editor")
	}
	if model = mergePress(model, "s"); !outcome.Saved || !reflect.DeepEqual(s.saved[0], want) {
		t.Fatalf("saved %v", s.saved)
	}
}

func TestNano_MergeEditorAsksBeforeLeavingWithUnappliedEdits(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "e")
	model = nanoPressMerge(model, "z", "ctrl+x")
	if !model.editing || !strings.Contains(stripANSI(model.View()), "Save modified buffer?") {
		t.Fatalf("a changed editor asks:\n%s", stripANSI(model.View()))
	}
	model = nanoPressMerge(model, "n")
	if model.editing || !reflect.DeepEqual(model.Result(), split(editBase)) {
		t.Fatalf("n leaves without applying: editing %v result %q", model.editing, model.Result())
	}
}

func TestNano_GitEditorSavesWithCtrlSAndLeavesWithCtrlX(t *testing.T) {
	withEditorStyle(t, config.EditorNano)
	model, src := newEditModel(t, "a\nb\n", "a\nB\n")
	model = gitPress(model, "e")
	if model.editor == nil || model.editor.nano == nil {
		t.Fatal("the editor opens in the nano style")
	}
	press := func(keys ...string) {
		for _, key := range keys {
			updated, _ := model.Update(keyMsg(key))
			model = updated.(GitModel)
		}
	}
	press("x", "ctrl+s")
	if src.disk["a.txt"] != "xa\nB\n" && src.disk["a.txt"] != "a\nxB\n" {
		t.Fatalf("file = %q", src.disk["a.txt"])
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "[ctrl+x] Exit") || !strings.Contains(view, "(alt+u undoes)") {
		t.Fatalf("the hints and the message:\n%s", view)
	}
	press("right") // a key press clears the message: the status line is the nano one
	if view := stripANSI(model.View()); !strings.Contains(view, "NANO") {
		t.Fatalf("the status:\n%s", view)
	}
	press("ctrl+x")
	if model.editor != nil {
		t.Fatal("ctrl+x leaves the editor")
	}
}
