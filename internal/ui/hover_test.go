package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

// ctrlAltSpace is what a terminal sends for ctrl+alt+space: escape and NUL, which Bubble Tea reads as alt+ctrl+@.
func ctrlAltSpace() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlAt, Alt: true} }

func askDoc(model EditorModel) EditorModel {
	updated, cmd := model.Update(ctrlAltSpace())
	model = updated.(EditorModel)
	if cmd != nil {
		updated, _ = model.Update(cmd())
		model = updated.(EditorModel)
	}
	return model
}

func TestHoverShowsDocumentationWhenCtrlAltSpaceIsPressedInNormalMode(t *testing.T) {
	nav := &fakeNav{hoverText: "func Add(a, b int) int\n\nAdd adds two numbers."}
	model, _ := newEditorForTest(t, "Add(1, 2)\n")
	model = model.WithNavigator(nav, "")

	model = askDoc(model)

	view := model.View()
	for _, want := range []string{"func Add(a, b int) int", "Add adds two numbers."} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if len(nav.asked) != 1 || nav.asked[0] != "hover main.go 0:0" {
		t.Errorf("asked = %v, want one hover at 0:0", nav.asked)
	}
}

func TestHoverWorksInInsertModeAndAnyKeyDismissesIt(t *testing.T) {
	nav := &fakeNav{hoverText: "the docs"}
	model := completionEditor(t, nav, "x\n", "A")

	model = askDoc(model)
	if !strings.Contains(model.View(), "the docs") {
		t.Fatalf("no documentation shown:\n%s", model.View())
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(EditorModel)
	if strings.Contains(model.View(), "the docs") {
		t.Error("the documentation stays after a key press")
	}
	if got := model.Lines()[0]; got != "xy" {
		t.Errorf("the key that dismissed the documentation was lost: line is %q", got)
	}
}

func TestHoverSaysSoWhenThereIsNoDocumentation(t *testing.T) {
	nav := &fakeNav{}
	model, _ := newEditorForTest(t, "x\n")
	model = model.WithNavigator(nav, "")

	model = askDoc(model)

	if !strings.Contains(model.View(), "No documentation found") {
		t.Errorf("no message:\n%s", model.View())
	}
}

func TestHoverWrapsLongDocumentationWithoutCuttingMarks(t *testing.T) {
	nav := &fakeNav{hoverText: strings.Repeat("word ", 60) + "\n" + strings.Repeat("line\n", 40)}
	model, _ := newEditorForTest(t, "x\n")
	model = model.WithNavigator(nav, "")

	model = askDoc(model)

	rows := model.hoverRows(40)
	if len(rows) > hoverCap(model.height) {
		t.Errorf("%d rows, more than the cap %d", len(rows), hoverCap(model.height))
	}
	for _, row := range rows {
		if strings.Contains(row, "…") {
			t.Errorf("a row is cut with an ellipsis: %q", row)
		}
	}
	if !strings.Contains(strings.Join(rows, " "), strings.TrimSpace(strings.Repeat("word ", 60))[:30]) {
		t.Error("the first line of the documentation was lost")
	}
}

func TestHoverShowsDocumentationWhenThePointerRestsOnASymbol(t *testing.T) {
	nav := &fakeNav{hoverText: "the docs of Add"}
	model, _ := newEditorForTest(t, "x := Add(1, 2)\n")
	model = model.WithNavigator(nav, "")
	x := model.numberWidth() + 5 // on the A of Add

	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: x, Y: 1})
	model = updated.(EditorModel)
	if cmd == nil {
		t.Fatal("no wait started for the resting pointer")
	}
	updated, cmd = model.Update(cmd()) // the delay is over
	model = updated.(EditorModel)
	updated, _ = model.Update(cmd()) // the answer
	model = updated.(EditorModel)

	if !strings.Contains(model.View(), "the docs of Add") {
		t.Errorf("no documentation:\n%s", model.View())
	}
	if len(nav.asked) != 1 || nav.asked[0] != "hover main.go 0:5" {
		t.Errorf("asked = %v, want one hover at 0:5", nav.asked)
	}
	if row, col := model.Cursor(); row != 0 || col != 0 {
		t.Errorf("the pointer moved the cursor to %d:%d", row, col)
	}
}

func TestHoverIsDroppedWhenThePointerMovesOn(t *testing.T) {
	nav := &fakeNav{hoverText: "stale"}
	model, _ := newEditorForTest(t, "x := Add(1, 2)\n")
	model = model.WithNavigator(nav, "")
	x := model.numberWidth() + 5

	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: x, Y: 1})
	model = updated.(EditorModel)
	dwell := cmd()
	updated, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: x + 20, Y: 1}) // moved away
	model = updated.(EditorModel)
	_, cmd = model.Update(dwell)

	if cmd != nil {
		t.Error("a question was asked although the pointer had moved on")
	}
}

func TestHoverIgnoresThePointerOnWhitespace(t *testing.T) {
	nav := &fakeNav{hoverText: "docs"}
	model, _ := newEditorForTest(t, "x := Add(1, 2)\n")
	model = model.WithNavigator(nav, "")

	_, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: model.numberWidth() + 40, Y: 1})

	if cmd != nil {
		t.Error("the pointer far right of the text started a question")
	}
}

func TestHoverWorksAtTheEndOfATabIndentedLine(t *testing.T) {
	nav := &fakeNav{hoverText: "docs"}
	model, _ := newEditorForTest(t, "\tfoo()\n")
	model = model.WithNavigator(nav, "")
	x := model.numberWidth() + tabWidth + 2 // on the last o of foo, after the tab

	_, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: x, Y: 1})

	if cmd == nil {
		t.Error("no hover on an identifier near the end of a tab-indented line")
	}
}

func TestHoverKeepsTheIndentationOfCodeInTheDocumentation(t *testing.T) {
	nav := &fakeNav{hoverText: "func F(\n    a int,\n        b int,\n)"}
	model, _ := newEditorForTest(t, "F()\n")
	model = askDoc(model.WithNavigator(nav, ""))
	view := stripANSI(strings.Join(model.hoverRows(60), "\n"))
	if !strings.Contains(view, " func F(") || !strings.Contains(view, "     a int,") || !strings.Contains(view, "         b int,") {
		t.Errorf("the indentation is lost:\n%s", view)
	}
}

func TestHoverKeyIsIgnoredWhileACommandIsTyped(t *testing.T) {
	nav := &fakeNav{hoverText: "docs"}
	model, _ := newEditorForTest(t, "x\n")
	model = model.WithNavigator(nav, "")
	model, _ = typeEditor(model, ":")
	if updated, cmd := model.Update(ctrlAltSpace()); cmd != nil || len(nav.asked) != 0 {
		t.Errorf("hover asked while a command line is open (%v, %v)", cmd, updated != nil)
	}
}

func TestHoverShowsTheServerErrorsAndStaysQuietForThePointer(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&lsp.NotInstalledError{Server: "gopls", Command: "gopls"}, "gopls"},
		{lsp.ErrNoServer, "No language server for this file type"},
		{errors.New("boom"), "Language server: boom"},
	} {
		nav := &fakeNav{err: tc.err}
		model, _ := newEditorForTest(t, "x\n")
		model = askDoc(model.WithNavigator(nav, ""))
		if view := stripANSI(model.View()); !strings.Contains(view, tc.want) {
			t.Errorf("%v: the message %q is missing:\n%s", tc.err, tc.want, view)
		}
	}
	// the pointer rests on a word of a file the server cannot handle: no message
	nav := &fakeNav{err: lsp.ErrNoServer}
	model, _ := newEditorForTest(t, "x := Add(1, 2)\n")
	model = model.WithNavigator(nav, "")
	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: model.numberWidth() + 5, Y: 1})
	model = updated.(EditorModel)
	updated, cmd = model.Update(cmd())
	model = updated.(EditorModel)
	updated, _ = model.Update(cmd())
	if view := stripANSI(updated.(EditorModel).View()); strings.Contains(view, "No language server") {
		t.Errorf("a resting pointer must not nag:\n%s", view)
	}
}

func TestHoverKeepsTheCursorLineInView(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	nav := &fakeNav{hoverText: strings.Repeat("documentation\n", 20)}
	model, _ := newEditorForTest(t, strings.Join(lines, "\n")+"\n") // 12 lines high
	model = model.WithNavigator(nav, "")
	model, _ = typeEditor(model, "9j")
	model = askDoc(model)
	row, _ := model.Cursor()
	if row < model.top || row >= model.top+model.textHeight() {
		t.Errorf("the cursor line %d is out of view (top %d, %d lines high)", row, model.top, model.textHeight())
	}
}
