package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDiffPalette_OpensListsActionsAndRunsOne_WhenCtrlAPressed(t *testing.T) {
	updated, _ := NewDiffModel("a", "b", []string{"x", "y", "z"}, []string{"X", "y", "Z"}).Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model := updated.(DiffModel)
	opened, _ := model.Update(keyMsg("ctrl+a"))
	model = opened.(DiffModel)
	view := stripANSI(model.View())
	if model.palette == nil || !strings.Contains(view, "Actions (ctrl+a)") || !strings.Contains(view, "Next change") || !strings.Contains(view, "Switch between the split") {
		t.Fatalf("the palette must list the actions:\n%s", view)
	}
	if strings.Contains(view, "Revert the selected change") {
		t.Fatalf("a diff of two texts cannot be reverted:\n%s", view)
	}
	chosen, _ := model.Update(keyMsg("enter")) // the first entry: next change
	model = chosen.(DiffModel)
	if model.palette != nil || model.current != 0 {
		t.Fatalf("enter must run the action and close the palette: palette %v, current %d", model.palette, model.current)
	}
	again, _ := model.Update(keyMsg("ctrl+a"))
	closed, _ := again.(DiffModel).Update(keyMsg("ctrl+a"))
	if closed.(DiffModel).palette != nil {
		t.Fatal("the key that opens the palette must close it")
	}
	escaped, _ := again.(DiffModel).Update(keyMsg("esc"))
	if escaped.(DiffModel).palette != nil {
		t.Fatal("esc must close the palette without quitting")
	}
}

func TestDiffPalette_WorksInTheGitView_AndQuitsFromIt(t *testing.T) {
	model, _ := newGitTestModel(120, 24)
	opened, _ := model.Update(keyMsg("f1"))
	model = opened.(GitModel)
	view := stripANSI(model.View())
	if model.palette == nil || !strings.Contains(view, "Actions (") || !strings.Contains(view, "Quit") {
		t.Fatalf("git view palette:\n%s", view)
	}
	typed, _ := model.Update(keyMsg("q"))
	typed, _ = typed.Update(keyMsg("u"))
	_, cmd := typed.Update(keyMsg("enter")) // typing "qu" leaves Quit
	if cmd == nil {
		t.Fatal("choosing Quit must end the program")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.Quit, got %T", cmd())
	}
}
