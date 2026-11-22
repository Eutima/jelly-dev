package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/project"
)

type projectFixture struct {
	root     string
	windows  []string
	sessions []project.Session
}

func newProjectFixture(t *testing.T, width int, files map[string]string) (ProjectModel, *projectFixture) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fx := &projectFixture{root: root}
	hooks := ProjectHooks{
		NewWindow:   func(dir string) error { fx.windows = append(fx.windows, dir); return nil },
		SaveSession: func(_ string, s project.Session) { fx.sessions = append(fx.sessions, s) },
	}
	model, err := NewProjectModel(root, "", project.Session{}, hooks)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 14})
	return updated.(ProjectModel), fx
}

func projectKeys(model ProjectModel, keys ...string) (ProjectModel, tea.Cmd) {
	var last tea.Cmd
	for _, key := range keys {
		var msg tea.KeyMsg
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+e":
			msg = tea.KeyMsg{Type: tea.KeyCtrlE}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
			if len([]rune(key)) > 1 { // a key with a name (ctrl+o, shift+tab): the message the terminal would send
				msg = keyMsg(key)
			}
		}
		updated, cmd := model.Update(msg)
		model = updated.(ProjectModel)
		// like the runtime: commands that carry a message are fed back
		for cmd != nil {
			result := cmd()
			if _, quit := result.(tea.QuitMsg); quit {
				last = cmd
				break
			}
			if result == nil {
				break
			}
			updated, cmd = model.Update(result)
			model = updated.(ProjectModel)
		}
	}
	return model, last
}

func typeCommand(model ProjectModel, command string) (ProjectModel, tea.Cmd) {
	keys := []string{":"}
	for _, r := range command {
		keys = append(keys, string(r))
	}
	return projectKeys(model, append(keys, "enter")...)
}

var sampleFiles = map[string]string{
	"README.md":     "# hi\n",
	"src/main.go":   "package main\n\nfunc main() {}\n",
	"src/util/u.go": "package util\n",
}

func TestProject_ShowsTreeAndOpensFileWithEnter(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	view := stripANSI(model.View())
	for _, want := range []string{"src/", "README.md"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tree lacks %q:\n%s", want, view)
		}
	}
	model, _ = projectKeys(model, "enter") // the first row is the directory src: it opens
	if !strings.Contains(stripANSI(model.View()), "main.go") {
		t.Fatalf("enter must open the directory:\n%s", model.View())
	}
	model, _ = projectKeys(model, "j", "j", "enter") // util/ is first, then main.go
	if model.focus != focusProjectEdit || filepath.Base(model.Editor().Path()) == "" {
		t.Fatalf("focus %d path %q", model.focus, model.Editor().Path())
	}
}

func TestProject_OpensFileAndSwitchesFocus(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter") // README.md is the last row
	if filepath.Base(model.Editor().Path()) != "README.md" || model.focus != focusProjectEdit {
		t.Fatalf("path %q focus %d", model.Editor().Path(), model.focus)
	}
	model, _ = projectKeys(model, "A", "!", "esc")
	if !model.Editor().Modified() || !strings.Contains(stripANSI(model.View()), "# hi!") {
		t.Fatalf("typing must go to the editor:\n%s", model.View())
	}
	model, _ = projectKeys(model, "ctrl+e")
	if model.focus != focusTree {
		t.Fatal("ctrl+e must move the focus to the tree")
	}
	model, _ = projectKeys(model, "k", "enter", "j", "j", "enter") // open src/, then main.go while README.md is modified
	if filepath.Base(model.Editor().Path()) != "main.go" || len(model.openEditors()) != 2 || model.dlg != nil {
		t.Fatalf("another file opens in a tab of its own:\n%s", model.View())
	}
	readme := model.tabIndex(filepath.Join(model.Root(), "README.md"))
	if readme < 0 || !model.openEditors()[readme].Modified() || !strings.Contains(stripANSI(model.View()), "README.md  ●+") {
		t.Fatalf("the modified README.md stays open in its tab:\n%s", model.View())
	}
	model, _ = projectKeys(model, "ctrl+e", "G", "enter") // README.md again: its tab is shown, not opened twice
	if filepath.Base(model.Editor().Path()) != "README.md" || len(model.openEditors()) != 2 || !model.Editor().Modified() {
		t.Fatalf("an open file is shown, not opened again: %d tabs", len(model.openEditors()))
	}
}

func TestProject_TabsSwitchCloseAndComeBack(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter") // README.md
	model, _ = projectKeys(model, "x")          // modified
	model, _ = projectKeys(model, "ctrl+e", "k", "enter", "j", "j", "enter")
	if len(model.openEditors()) != 2 {
		t.Fatalf("%d tabs", len(model.openEditors()))
	}
	first := strings.Split(stripANSI(model.View()), "\n")[1]
	if !strings.Contains(first, "README.md [+]") || !strings.Contains(first, "main.go") {
		t.Fatalf("the tab bar shows the files: %q", first)
	}
	model, _ = projectKeys(model, "g", "T") // back to the first tab
	if filepath.Base(model.Editor().Path()) != "README.md" {
		t.Fatalf("gT must show the previous tab: %q", model.Editor().Path())
	}
	updated, _ := model.runProjectCommand("tabclose") // modified: asks
	model = updated.(ProjectModel)
	if model.dlg == nil {
		t.Fatal("closing a tab with changes must ask")
	}
	model, _ = projectKeys(model, "d")
	if len(model.openEditors()) != 1 || filepath.Base(model.Editor().Path()) != "main.go" {
		t.Fatalf("discard closes the tab: %d tabs, %q", len(model.openEditors()), model.Editor().Path())
	}
	session := model.session()
	if session.Active != "src/main.go" || len(session.Files) != 1 {
		t.Fatalf("session %+v", session)
	}
}

func TestProject_OpensWindowForDirectory_WhenWPressedOrCommandTyped(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "w") // the selected row is src/
	if len(fx.windows) != 1 || fx.windows[0] != filepath.Join(fx.root, "src") {
		t.Fatalf("windows %v", fx.windows)
	}
	model, _ = projectKeys(model, "G", "enter") // README.md, editor focus
	model, _ = typeCommand(model, "window")
	model, _ = typeCommand(model, "window src/util")
	if len(fx.windows) != 3 || fx.windows[1] != fx.root || fx.windows[2] != filepath.Join(fx.root, "src", "util") {
		t.Fatalf("windows %v", fx.windows)
	}
	model, _ = typeCommand(model, "window nowhere")
	if len(fx.windows) != 3 || !strings.Contains(stripANSI(model.View()), "E344") {
		t.Fatalf("a missing folder must be refused:\n%s", model.View())
	}
}

func TestProject_ReportsWindowFailure(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.NewWindow = func(string) error { return errors.New("no terminal found") }
	model, _ = typeCommand(inEditor(model), "window") // the project itself: nothing else to fall back to
	if !strings.Contains(stripANSI(model.View()), "no terminal found") {
		t.Fatalf("failure must be shown:\n%s", model.View())
	}
}

func TestProject_OpensHere_WhenNoWindowCanBeOpened(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.NewWindow = func(string) error { return errors.New("no terminal found") } // a container has none
	model, _ = projectKeys(model, "w")                                                    // on the src folder
	if filepath.Base(model.Root()) != "src" || !strings.Contains(stripANSI(model.View()), "No new window possible") {
		t.Fatalf("without a terminal the project opens in this window: %q\n%s", model.Root(), model.View())
	}
}

func TestProject_StoresSessionOnQuit(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter", "j", ":", "q", "enter")
	if len(fx.sessions) != 1 || fx.sessions[0].Active != "README.md" {
		t.Fatalf("sessions %+v", fx.sessions)
	}
	if _, cmd := projectKeys(model, "ctrl+e", "q"); cmd == nil {
		t.Fatal("q in the tree must quit")
	}
	if len(fx.sessions) != 2 {
		t.Fatalf("sessions %+v", fx.sessions)
	}
}

func TestProject_RestoresFileAndCursorFromSession(t *testing.T) {
	_, fx := newProjectFixture(t, 100, sampleFiles)
	session := project.Session{Active: "src/main.go", Cursors: map[string]project.Cursor{"src/main.go": {Row: 2, Col: 5}}}
	model, err := NewProjectModel(fx.root, "", session, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if row, col := model.Editor().Cursor(); filepath.Base(model.Editor().Path()) != "main.go" || row != 2 || col != 5 || model.focus != focusProjectEdit {
		t.Fatalf("path %q cursor %d,%d focus %d", model.Editor().Path(), row, col, model.focus)
	}
}

func TestProject_FitsScreenAtAllSizes(t *testing.T) {
	for _, width := range []int{20, 49, 50, 80, 200} {
		model, _ := newProjectFixture(t, width, sampleFiles)
		for _, keys := range [][]string{nil, {"G", "enter"}, {"ctrl+e"}} {
			model, _ = projectKeys(model, keys...)
			lines := strings.Split(model.View(), "\n")
			if len(lines) != 14 {
				t.Fatalf("width %d: %d lines", width, len(lines))
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("width %d line %d is %d wide", width, i, got)
				}
			}
		}
	}
}

func TestProject_ClicksSelectTreeRowsAndEditor(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	click := func(m ProjectModel, x, y int) ProjectModel {
		updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y + topBarHeight})
		return updated.(ProjectModel)
	}
	model = click(model, 3, 2) // second row: README.md
	if filepath.Base(model.Editor().Path()) != "README.md" || model.focus != focusProjectEdit {
		t.Fatalf("path %q focus %d", model.Editor().Path(), model.focus)
	}
	x := model.editorOffset() + 2 + 2 // text column 2 of the first line (line numbers take two columns)
	model = click(model, x, 1)
	if _, col := model.Editor().Cursor(); col != 2 {
		t.Fatalf("editor click must place the cursor, col %d", col)
	}
}
