package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
)

func editorChoiceKeys(model tea.Model, keys ...string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, key := range keys {
		model, cmd = model.Update(keyMsg(key))
	}
	return model, cmd
}

func newEditorChoice(current config.EditorStyle) (tea.Model, *EditorChoiceOutcome) {
	model, outcome := NewEditorChoiceModel(current)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return updated, outcome
}

func TestEditorChoice_StartsOnTheCurrentStyleAndChoosesWithEnter(t *testing.T) {
	model, outcome := newEditorChoice(config.EditorNano)
	view := stripANSI(model.View())
	if !strings.Contains(view, "(*) Nano-style") || !strings.Contains(view, "( ) Vim-style") {
		t.Fatalf("nano is selected first:\n%s", view)
	}
	for _, want := range []string{"beginner-friendly terminal text editor", "more advanced editor with different modes and keyboard commands"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the question describes both editors, lacks %q:\n%s", want, view)
		}
	}
	model, cmd := editorChoiceKeys(model, "down", "enter")
	if !outcome.Answered || outcome.Style != config.EditorVim || !isQuit(cmd) {
		t.Fatalf("outcome %+v", *outcome)
	}
	if model.View() != "" {
		t.Fatal("a finished question draws nothing")
	}
}

func TestEditorChoice_PreselectsTheStoredStyle_AndAbortsWithEscOrCtrlC(t *testing.T) {
	model, outcome := newEditorChoice(config.EditorVim)
	if view := stripANSI(model.View()); !strings.Contains(view, "(*) Vim-style") {
		t.Fatalf("vim is selected:\n%s", view)
	}
	_, cmd := editorChoiceKeys(model, "esc")
	if !outcome.Aborted || outcome.Answered || !isQuit(cmd) {
		t.Fatalf("outcome %+v", *outcome)
	}
	model, outcome = newEditorChoice(config.EditorNano)
	editorChoiceKeys(model, "ctrl+c")
	if !outcome.Aborted {
		t.Fatalf("outcome %+v", *outcome)
	}
}

func TestEditorChoice_ShortcutKeysAndClick(t *testing.T) {
	model, outcome := newEditorChoice(config.EditorNano)
	editorChoiceKeys(model, "2", "enter")
	if outcome.Style != config.EditorVim {
		t.Fatalf("2 selects the second entry: %+v", *outcome)
	}
	model, outcome = newEditorChoice(config.EditorVim)
	_, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: editorChoiceTop})
	if outcome.Style != config.EditorNano || !isQuit(cmd) {
		t.Fatalf("a click on the first entry chooses it: %+v", *outcome)
	}
}
