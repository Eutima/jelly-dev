package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTermSelection_TextCoversFromAnchorToEnd_InReadingOrder(t *testing.T) {
	cells := [][]string{{"a", "b", "c", " "}, {"d", "e", "f", " "}, {"g", "h", "i", " "}}
	sel := termSelection{anchorX: 1, anchorY: 0, curX: 1, curY: 2, moved: true}
	if got := sel.text(cells); got != "bc\ndef\ngh" {
		t.Fatalf("forward = %q", got)
	}
	back := termSelection{anchorX: 1, anchorY: 2, curX: 1, curY: 0, moved: true}
	if got := back.text(cells); got != "bc\ndef\ngh" {
		t.Fatalf("backward = %q", got)
	}
	if got := (termSelection{anchorX: 0, anchorY: 0, curX: 0, curY: 0}).text(cells); got != "" {
		t.Fatalf("a click selects nothing: %q", got)
	}
}

func TestTerminalPanel_TheWheelScrollsTheHistoryAndTypingBringsTheLiveScreenBack(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "i=1; while [ $i -le 80 ]; do echo row-$i; i=$((i+1)); done; cat")
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT}) // starts the shell
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	session := model.activeTerm()
	if session == nil {
		t.Fatal("no shell")
	}
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(stripANSI(strings.Join(session.Lines(false), "\n")), "row-80"); {
		if time.Now().After(deadline) {
			t.Fatalf("the shell printed nothing:\n%s", strings.Join(session.Lines(false), "\n"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	top := model.editorHeight() + topBarHeight
	wheel := func(m ProjectModel, button tea.MouseButton) ProjectModel {
		return update(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: model.editorOffset() + 5, Y: top + 2})
	}
	model = wheel(model, tea.MouseButtonWheelUp)
	model = wheel(model, tea.MouseButtonWheelUp)
	if offset, _ := session.Scrolled(); offset != 2*terminalWheelLines {
		t.Fatalf("every turn of the wheel scrolls %d lines: %d", terminalWheelLines, offset)
	}
	view := stripANSI(model.View())
	if strings.Contains(view, "row-80") || !strings.Contains(view, "lines back") {
		t.Fatalf("the panel shows older lines and says how far back it is:\n%s", view)
	}
	model = wheel(model, tea.MouseButtonWheelDown)
	if offset, _ := session.Scrolled(); offset != terminalWheelLines {
		t.Fatalf("the wheel down comes back: %d", offset)
	}
	model = update(model, tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	if offset, _ := session.Scrolled(); offset <= terminalWheelLines {
		t.Fatalf("ctrl+pgup scrolls a page back: %d", offset)
	}
	model = update(model, tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	model = update(model, tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	if offset, _ := session.Scrolled(); offset != 0 {
		t.Fatalf("ctrl+pgdn comes back to the live screen: %d", offset)
	}
	model = wheel(model, tea.MouseButtonWheelUp)
	model = update(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // typing shows the live screen
	if offset, _ := session.Scrolled(); offset != 0 {
		t.Fatalf("typing brings the live screen back: %d", offset)
	}
}
