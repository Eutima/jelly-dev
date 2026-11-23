package ui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

func openMain(t *testing.T) (ProjectModel, *projectFixture, string) {
	t.Helper()
	model, fx := newProjectFixture(t, 100, sampleFiles)
	path := filepath.Join(fx.root, "src", "main.go")
	if err := model.openFile(path, true); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	return model, fx, path
}

func toggleBreakpointKey(model ProjectModel) ProjectModel {
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	return updated.(ProjectModel)
}

func TestDebugMarks_ToggleKeepsLinesSortedAndForgetsEmptyFiles(t *testing.T) {
	d := newDebugMarks()
	for _, line := range []int{9, 3, 5} {
		if !d.toggle("/a.go", line) {
			t.Fatalf("line %d must be set", line)
		}
	}
	if !reflect.DeepEqual(d.breakpoints["/a.go"], []int{3, 5, 9}) || !d.has("/a.go", 5) || d.has("/a.go", 4) || d.has("/b.go", 5) {
		t.Fatalf("lines: %v", d.breakpoints)
	}
	if d.toggle("/a.go", 5) || d.has("/a.go", 5) {
		t.Fatal("the second toggle removes it")
	}
	d.set("/a.go", nil)
	if _, there := d.breakpoints["/a.go"]; there {
		t.Fatal("a file without breakpoints is not kept")
	}
	d.set("/a.go", []int{4, 2, 4})
	if got := d.all()["/a.go"]; !reflect.DeepEqual(got, []int{2, 4}) {
		t.Fatalf("set sorts and drops doubles: %v", got)
	}
	var none *debugMarks
	if none.has("/a.go", 1) {
		t.Fatal("no marks, no breakpoints")
	}
}

func TestBreakpoints_F9SetsAndRemovesTheBreakpointOfTheCursorLineAndTheGutterShowsIt(t *testing.T) {
	model, _, path := openMain(t)
	model = toggleBreakpointKey(model) // the cursor is on line 1
	if !model.marks.has(path, 1) || !strings.Contains(model.status, "breakpoint set at main.go:1") {
		t.Fatalf("set: %v %q", model.marks.breakpoints, model.status)
	}
	if view := stripANSI(model.Editor().View()); !strings.Contains(view, breakpointMark+" ") {
		t.Fatalf("the marker column shows the breakpoint:\n%s", view)
	}
	model = toggleBreakpointKey(model)
	if model.marks.has(path, 1) || !strings.Contains(model.status, "breakpoint removed") {
		t.Fatalf("removed: %v %q", model.marks.breakpoints, model.status)
	}
	if view := stripANSI(model.Editor().View()); strings.Contains(view, breakpointMark) {
		t.Fatalf("no breakpoint, no marker:\n%s", view)
	}
}

func TestBreakpoints_ANewFileCannotHaveOneAndTheBreakCommandDoesTheSame(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model = toggleBreakpointKey(model) // the empty editor has no path
	if len(model.marks.breakpoints) != 0 || !model.warn {
		t.Fatalf("a file without a name gets no breakpoint: %v %q", model.marks.breakpoints, model.status)
	}
	model, _, path := openMain(t)
	updated, _ := model.runProjectCommand("break")
	if !updated.(ProjectModel).marks.has(path, 1) {
		t.Fatal(":break sets the breakpoint of the cursor line")
	}
}

func TestBreakpoints_ClickingTheMarkerColumnTogglesTheLineAndTheTextStillMovesTheCursor(t *testing.T) {
	model, _, path := openMain(t)
	click := func(m ProjectModel, x, y int) ProjectModel {
		updated, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y + topBarHeight})
		m = updated.(ProjectModel)
		for cmd != nil { // like the runtime: the message of the command comes back
			updated, cmd = m.Update(cmd())
			m = updated.(ProjectModel)
		}
		return m
	}
	model = click(model, model.editorOffset(), 3) // line 3 of main.go, first column: the marker cell
	if !model.marks.has(path, 3) {
		t.Fatalf("click on the marker cell: %v", model.marks.breakpoints)
	}
	if row, _ := model.Editor().Cursor(); row != 0 {
		t.Fatalf("a click on the marker leaves the cursor alone, row %d", row)
	}
	model = click(model, model.editorOffset()+breakpointColumn+2+3, 3) // in the text
	if row, _ := model.Editor().Cursor(); row != 2 || len(model.marks.breakpoints[path]) != 1 {
		t.Fatalf("a click on the text moves the cursor: row %d, %v", row, model.marks.breakpoints)
	}
	model = click(model, model.editorOffset(), 40) // below the text: nothing
	if len(model.marks.breakpoints[path]) != 1 {
		t.Fatalf("no line, no breakpoint: %v", model.marks.breakpoints)
	}
}

func TestBreakpoints_AreKeptInTheSessionAndComeBack(t *testing.T) {
	model, fx, path := openMain(t)
	model = toggleBreakpointKey(model)
	other := filepath.Join(fx.root, "README.md")
	model.marks.set(other, []int{1}) // a file that is not open
	session := model.session()
	if !reflect.DeepEqual(session.Breakpoints, map[string][]int{"src/main.go": {1}, "README.md": {1}}) {
		t.Fatalf("the session keeps the breakpoints of open and closed files: %+v", session.Breakpoints)
	}
	model.marks.toggle(path, 7) // later changes must not reach the session that was built before
	if !reflect.DeepEqual(session.Breakpoints["src/main.go"], []int{1}) {
		t.Fatalf("the session is a copy: %+v", session.Breakpoints)
	}
	restored, err := NewProjectModel(fx.root, "", session, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !restored.marks.has(path, 1) {
		t.Fatalf("restored: %v", restored.marks.breakpoints)
	}
	absolute, _ := NewProjectModel(fx.root, "", project.Session{Breakpoints: map[string][]int{other: {2}}}, ProjectHooks{})
	if !absolute.marks.has(other, 2) {
		t.Fatalf("an absolute path in the session works as well: %v", absolute.marks.breakpoints)
	}
}

func TestBreakpoints_TheMarkerColumnStaysWhenTheLineNumbersAreOff(t *testing.T) {
	model, _, path := openMain(t)
	model = toggleBreakpointKey(model)
	editor := model.Editor()
	editor.showNums = false
	if editor.numberWidth() != breakpointColumn {
		t.Fatalf("without numbers only the marker column is left: %d", editor.numberWidth())
	}
	if line, ok := editor.markerClick(1, 1); !ok || line != 1 || path == "" {
		t.Fatalf("the marker cell is still clickable: %d %v", line, ok)
	}
	if view := stripANSI(editor.View()); !strings.Contains(view, breakpointMark) {
		t.Fatalf("the marker is drawn without numbers:\n%s", view)
	}
}
