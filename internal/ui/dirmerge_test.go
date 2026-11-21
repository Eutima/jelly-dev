package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/merge"
	"github.com/eutima/jelly-dev/internal/source"
)

func dirMergeTestModel(t *testing.T, s *saver) DirMergeModel {
	t.Helper()
	files := []source.ChangedFile{
		{Status: source.StatusModified, Path: "a.txt"},
		{Status: source.StatusAdded, Path: "new.txt"},
		{Status: source.StatusDeleted, Path: "old.txt"},
		{Status: source.StatusModified, Path: "broken.bin"},
	}
	open := func(file source.ChangedFile) (MergeModel, *MergeOutcome, error) {
		if file.Path == "broken.bin" {
			return MergeModel{}, nil, errors.New("binary file")
		}
		chunks := merge.TwoWay(split("1\nold\n3\n"), split("1\nnew\n3\n"))
		model, outcome := NewMergeModel(MergeTitles{Name: file.Path, Ours: "a", Result: "a", Theirs: "b"}, chunks, s.save)
		return model, outcome, nil
	}
	updated, _ := NewDirMergeModel("dirA", "dirB", files, open).Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return updated.(DirMergeModel)
}

func dirPress(model DirMergeModel, keys ...tea.KeyMsg) DirMergeModel {
	for _, key := range keys {
		updated, _ := model.Update(key)
		model = updated.(DirMergeModel)
	}
	return model
}

func runes(text string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)} }

var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

func TestDirMergeView_ListsFilesWithStatusAndSide_WhenSized(t *testing.T) {
	view := dirMergeTestModel(t, &saver{}).View()
	for _, want := range []string{"Merge dirA ← dirB", "4 files, 0 merged", "a.txt", "new.txt  (only in b)", "old.txt  (only in a)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestDirMergeModel_OpensMergeViewAndMarksFileMerged_WhenSavedAndLeft(t *testing.T) {
	s := &saver{}
	model := dirMergeTestModel(t, s)
	model = dirPress(model, enterKey)
	if !model.hasChild || !strings.Contains(model.View(), "Jelly Dev") {
		t.Fatalf("the merge view must be shown:\n%s", model.View())
	}
	model = dirPress(model, runes("n"), runes("t"), runes("s"), runes("q"))
	if model.hasChild || model.MergedCount() != 1 || !model.merged[0] || len(s.saved) != 1 {
		t.Fatalf("hasChild %v merged %v saved %v", model.hasChild, model.merged, s.saved)
	}
	if view := model.View(); !strings.Contains(view, "1 merged") || !strings.Contains(view, mergedMark+" a.txt saved") {
		t.Fatalf("merged marker expected:\n%s", view)
	}
}

func TestDirMergeModel_ProtectsUnsavedResultAndReturnsToList_WhenQuittingTheMergeView(t *testing.T) {
	model := dirPress(dirMergeTestModel(t, &saver{}), enterKey, runes("n"), runes("t"), runes("q"))
	if !model.hasChild {
		t.Fatal("the first q must only warn about the unsaved result")
	}
	model = dirPress(model, runes("q"))
	if model.hasChild || model.MergedCount() != 0 {
		t.Fatalf("second q must discard and return to the list, merged %v", model.merged)
	}
}

func TestDirMergeModel_ShowsError_WhenFileCannotBeOpened(t *testing.T) {
	model := dirPress(dirMergeTestModel(t, &saver{}), runes("G"), enterKey)
	if model.hasChild || !strings.Contains(model.View(), "Cannot open broken.bin: binary file") {
		t.Fatalf("error expected:\n%s", model.View())
	}
	if model = dirPress(model, runes("k")); model.selected != 2 {
		t.Fatalf("the list must stay usable, selected %d", model.selected)
	}
}

func TestDirMergeModel_QuitsProgram_WhenQPressedInList(t *testing.T) {
	_, cmd := dirMergeTestModel(t, &saver{}).Update(runes("q"))
	if cmd == nil {
		t.Fatal("q in the list must quit")
	}
}

func TestDirMergeModel_AppliesTheme_WhenThemed(t *testing.T) {
	model := dirMergeTestModel(t, &saver{})
	themed := Themed(model, LightTheme()).(DirMergeModel)
	if themed.theme.Equal.GetForeground() == model.theme.Equal.GetForeground() {
		t.Fatal("theme not applied")
	}
}

func TestDirMergeView_StripsControlCharactersAndFitsWidth_WhenStatusContainsFileName(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "evil\x1b]0;pwned\x07name" + strings.Repeat("x", 200)}}
	open := func(source.ChangedFile) (MergeModel, *MergeOutcome, error) {
		return MergeModel{}, nil, errors.New("bad \x1b[31m file")
	}
	updated, _ := NewDirMergeModel("a", "b", files, open).Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	model := dirPress(updated.(DirMergeModel), enterKey)
	view := model.View()
	if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\x07") {
		t.Fatalf("escape sequence in view: %q", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 60 {
			t.Fatalf("line %d is %d wide", i, got)
		}
	}
}
