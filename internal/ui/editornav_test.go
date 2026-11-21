package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// fakeNav answers navigation requests from tables and records what it was asked.
type fakeNav struct {
	definitions map[string][]lsp.Place // by "row:col"
	references  []lsp.Place
	err         error
	formatted   string // what Format returns; the text itself when empty
	formatWait  func() // called while a format request runs (to change the text under its feet)
	completions []lsp.CompletionItem
	completeAt  [][2]int
	asked       []string
	texts       []string
}

func (f *fakeNav) Definition(_ context.Context, path, text string, row, col int) ([]lsp.Place, error) {
	f.asked = append(f.asked, "def "+filepath.Base(path))
	f.texts = append(f.texts, text)
	if f.err != nil {
		return nil, f.err
	}
	return f.definitions[strings.Join([]string{itoa(row), itoa(col)}, ":")], nil
}

func (f *fakeNav) References(_ context.Context, path, text string, row, col int) ([]lsp.Place, error) {
	f.asked = append(f.asked, "ref "+filepath.Base(path))
	return f.references, f.err
}

func (f *fakeNav) Format(_ context.Context, path, text string) (string, error) {
	f.asked = append(f.asked, "fmt "+filepath.Base(path))
	if f.formatWait != nil {
		f.formatWait()
	}
	if f.err != nil {
		return "", f.err
	}
	if f.formatted != "" {
		return f.formatted, nil
	}
	return text, nil
}

func (f *fakeNav) Complete(_ context.Context, path, _ string, row, col int) ([]lsp.CompletionItem, error) {
	f.asked = append(f.asked, "complete "+filepath.Base(path))
	f.completeAt = append(f.completeAt, [2]int{row, col})
	return f.completions, f.err
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n))) }

// navProject opens a project with two Go files and a fake navigator.
func navProject(t *testing.T, nav *fakeNav) (ProjectModel, string) {
	t.Helper()
	files := map[string]string{
		"main.go": "package main\n\nfunc main() {\n\thelper()\n}\n",
		"util.go": "package main\n\n// helper does it\nfunc helper() {}\n",
	}
	model, fx := newProjectFixture(t, 120, files)
	model.hooks.Navigator = nav
	model.editor = model.editor.WithNavigator(nav, fx.root)
	model, _ = projectKeys(model, "enter") // main.go (the tree is sorted: main.go, util.go)
	return model, fx.root
}

func TestEditor_JumpsToDefinitionInSameFileAndBack(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	main := filepath.Join(root, "main.go")
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: main, Row: 2, Col: 5, Text: "func main() {"}}}
	model, _ = projectKeys(model, "j", "j", "j", "l") // the call helper() on line 4
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
		t.Fatalf("setup: cursor %d,%d", row, col)
	}
	model, _ = projectKeys(model, "g", "d")
	if row, col := model.Editor().Cursor(); row != 2 || col != 5 {
		t.Fatalf("gd must jump to the definition, cursor %d,%d", row, col)
	}
	if len(nav.asked) != 1 || !strings.Contains(nav.texts[0], "helper()") {
		t.Fatalf("the editor text must be sent: %v", nav.asked)
	}
	model, _ = projectKeys(model, "ctrl+o")
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
		t.Fatalf("ctrl+o must return to where gd started, cursor %d,%d", row, col)
	}
	if model, _ = projectKeys(model, "ctrl+o"); !strings.Contains(stripANSI(model.View()), "Jump list is empty") {
		t.Fatalf("an empty jump list must be explained:\n%s", model.View())
	}
}

func TestProject_JumpsToDefinitionInOtherFileAndBack(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	util := filepath.Join(root, "util.go")
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: util, Row: 3, Col: 5, Text: "func helper() {}"}}}
	model, _ = projectKeys(model, "j", "j", "j", "l", "g", "d")
	if model.Editor().Path() != util {
		t.Fatalf("gd must open the file of the definition: %q", model.Editor().Path())
	}
	if row, col := model.Editor().Cursor(); row != 3 || col != 5 || model.focus != focusProjectEdit {
		t.Fatalf("cursor %d,%d focus %d", row, col, model.focus)
	}
	model, _ = projectKeys(model, "ctrl+o")
	if model.Editor().Path() != filepath.Join(root, "main.go") {
		t.Fatalf("ctrl+o must return to the first file: %q", model.Editor().Path())
	}
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
		t.Fatalf("cursor %d,%d", row, col)
	}
}

func TestProject_JumpOpensTabAndKeepsUnsavedChanges(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: filepath.Join(root, "util.go"), Row: 3, Col: 5}}}
	model, _ = projectKeys(model, "j", "j", "j", "l")
	model, _ = projectKeys(model, "x") // unsaved change
	model, _ = projectKeys(model, "g", "d")
	if filepath.Base(model.Editor().Path()) != "util.go" || len(model.openEditors()) != 2 || !model.openEditors()[0].Modified() {
		t.Fatalf("the jump must open a tab and keep the changes of the first:\n%s", model.View())
	}
	if len(model.Editor().jumps) != 1 {
		t.Fatalf("the jump list goes along: %v", model.Editor().jumps)
	}
}

func TestEditor_ListsReferencesAndJumpsToChosenOne(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	main, util := filepath.Join(root, "main.go"), filepath.Join(root, "util.go")
	nav.references = []lsp.Place{
		{Path: main, Row: 3, Col: 1, Text: "\thelper()"},
		{Path: util, Row: 3, Col: 5, Text: "func helper() {}"},
		{Path: util, Row: 2, Col: 3, Text: "// helper does it"},
	}
	model, _ = projectKeys(model, "j", "j", "j", "l", "g", "r")
	view := stripANSI(model.View())
	for _, want := range []string{"3 references", "main.go:4", "util.go:4", "util.go:3", "helper()"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list lacks %q:\n%s", want, view)
		}
	}
	model, _ = projectKeys(model, "j", "enter")
	if model.Editor().Path() != util {
		t.Fatalf("enter must open the chosen reference: %q", model.Editor().Path())
	}
	if row, col := model.Editor().Cursor(); row != 3 || col != 5 || model.Editor().list != nil {
		t.Fatalf("cursor %d,%d list %v", row, col, model.Editor().list)
	}
}

func TestEditor_ClosesListWithEscapeAndClickOpensEntry(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	main := filepath.Join(root, "main.go")
	nav.references = []lsp.Place{{Path: main, Row: 0, Col: 0, Text: "package main"}, {Path: main, Row: 3, Col: 1, Text: "\thelper()"}}
	model, _ = projectKeys(model, "g", "r")
	model, _ = projectKeys(model, "esc")
	if model.Editor().list != nil {
		t.Fatal("esc must close the list")
	}
	model, _ = projectKeys(model, "g", "r")
	// click the second entry: title line + first entry come before it
	editor := model.Editor()
	y := topBarHeight + 1 + editor.textHeight() + 2
	updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: model.editorOffset() + 3, Y: y})
	model = updated.(ProjectModel)
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 || model.Editor().list != nil {
		t.Fatalf("a click must open the entry: cursor %d,%d", row, col)
	}
}

func TestEditor_ExplainsNavigationProblems(t *testing.T) {
	tests := []struct {
		name string
		nav  *fakeNav
		want string
	}{
		{"missing server", &fakeNav{err: &lsp.NotInstalledError{Server: "gopls", Command: "gopls"}}, "gopls not found"},
		{"unknown file type", &fakeNav{err: lsp.ErrNoServer}, "No language server for this file type"},
		{"server failure", &fakeNav{err: errors.New("boom")}, "Language server: boom"},
		{"nothing found", &fakeNav{}, "No definition found"},
	}
	for _, tt := range tests {
		model, _ := navProject(t, tt.nav)
		model, _ = projectKeys(model, "g", "d")
		if view := stripANSI(model.View()); !strings.Contains(view, tt.want) {
			t.Errorf("%s: view lacks %q:\n%s", tt.name, tt.want, view)
		}
	}
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter", "g", "d")
	if !strings.Contains(stripANSI(model.View()), "No language server available") {
		t.Fatalf("without a navigator gd must say so:\n%s", model.View())
	}
}

func TestEditor_NavigationFooterCommandsSendTheirKeys(t *testing.T) {
	nav := &fakeNav{}
	model, _ := navProject(t, nav)
	bar := model.Editor().footerBar()
	text := stripANSI(bar.render(300, model.theme.Footer))
	for command, want := range map[string][]string{"[gr]": {"g", "r"}, "[gd]": {"g", "d"}, "[ctrl+f]": {"ctrl+f"}, "[ctrl+g]": {"ctrl+g"}} {
		index := strings.Index(text, command)
		if index < 0 {
			t.Fatalf("footer lacks %s: %q", command, text)
		}
		if keys := bar.keysAt(len([]rune(text[:index]))); len(keys) != len(want) || keys[0] != want[0] {
			t.Errorf("%s must send %v, got %v", command, want, keys)
		}
	}
	plain, _ := newEditorForTest(t, "a\n")
	text = stripANSI(plain.footerBar().render(300, plain.theme.Footer))
	if index := strings.Index(text, "[gd]"); plain.footerBar().keysAt(len([]rune(text[:index]))) != nil {
		t.Error("without a navigator gd must be inert")
	}
}

func TestEditor_SingleReferenceJumpsAtOnce(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	nav.references = []lsp.Place{{Path: filepath.Join(root, "main.go"), Row: 2, Col: 0}}
	model, _ = projectKeys(model, "g", "r")
	if row, _ := model.Editor().Cursor(); row != 2 || model.Editor().list != nil {
		t.Fatalf("one result needs no list: row %d", row)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func TestEditor_FormatCommandReplacesTextAsOneUndoStep(t *testing.T) {
	nav := &fakeNav{formatted: "package main\n\nfunc main() {\n\thelper()\n}\n// formatted\n"}
	model, _ := navProject(t, nav)
	before := strings.Join(model.Editor().Lines(), "\n")
	model, _ = typeCommand(model, "format")
	got := model.Editor().Lines()
	if got[len(got)-1] != "// formatted" || !strings.Contains(stripANSI(model.View()), "Formatted") {
		t.Fatalf("the text must be formatted: %q\n%s", got, model.View())
	}
	if !model.Editor().Modified() {
		t.Fatal("formatting changes the buffer")
	}
	model, _ = projectKeys(model, "u")
	if strings.Join(model.Editor().Lines(), "\n") != before {
		t.Fatalf("one undo must bring the old text back: %q", model.Editor().Lines())
	}
}

func TestEditor_FormatCommandAliasesAndMessages(t *testing.T) {
	for _, command := range []string{"fmt", "Format"} {
		nav := &fakeNav{formatted: "x\n"}
		model, _ := navProject(t, nav)
		model, _ = typeCommand(model, command)
		if got := model.Editor().Lines(); len(got) != 1 || got[0] != "x" {
			t.Errorf(":%s: %q", command, got)
		}
	}
	model, _ := navProject(t, &fakeNav{})
	model, _ = typeCommand(model, "format")
	if !strings.Contains(stripANSI(model.View()), "Already formatted") || model.Editor().Modified() {
		t.Fatalf("unchanged text must say so:\n%s", model.View())
	}
	tests := []struct {
		err  error
		want string
	}{
		{&lsp.NotInstalledError{Server: "gopls", Command: "gopls"}, "gopls not found"},
		{lsp.ErrNoServer, "No formatter for this file type"},
		{lsp.ErrCannotFormat, "cannot format this file"},
		{errors.New("syntax error at 3:4"), "Format failed: syntax error at 3:4"},
	}
	for _, tt := range tests {
		model, _ := navProject(t, &fakeNav{err: tt.err})
		model, _ = typeCommand(model, "format")
		if view := stripANSI(model.View()); !strings.Contains(view, tt.want) || model.Editor().Modified() {
			t.Errorf("%v: view lacks %q:\n%s", tt.err, tt.want, view)
		}
	}
	plain, _ := newProjectFixture(t, 100, sampleFiles)
	plain, _ = projectKeys(plain, "G", "enter")
	plain, _ = typeCommand(plain, "format")
	if !strings.Contains(stripANSI(plain.View()), "No formatter available") {
		t.Fatalf("without a navigator the command must say so:\n%s", plain.View())
	}
}

func TestEditor_FormatIsNotAppliedWhenTextChangedMeanwhile(t *testing.T) {
	nav := &fakeNav{formatted: "formatted\n"}
	model, _ := navProject(t, nav)
	model, cmd := projectKeys(model, ":", "f", "o", "r", "m", "a", "t", "enter")
	_ = cmd
	// run the request by hand and change the text before its answer arrives
	editor := model.Editor()
	command := editor.formatCommand()
	if command == nil {
		t.Fatal("expected a request")
	}
	msg := command()
	typed, _ := editor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // the user keeps typing: deletes a character
	result, _ := typed.Update(msg)
	if got := result.(EditorModel).Lines(); got[0] == "formatted" {
		t.Fatalf("a stale answer must not be applied: %q", got)
	}
	if !strings.Contains(stripANSI(result.View()), "text changed while it was formatted") {
		t.Fatalf("view:\n%s", result.View())
	}
}

func TestEditor_CtrlCAndCtrlDGoBackAfterAJumpInsteadOfClosing(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD} {
		nav := &fakeNav{}
		model, root := navProject(t, nav)
		util := filepath.Join(root, "util.go")
		nav.definitions = map[string][]lsp.Place{"3:1": {{Path: util, Row: 3, Col: 5}}}
		model, _ = projectKeys(model, "j", "j", "j", "l", "g", "d")
		if model.Editor().Path() != util {
			t.Fatalf("setup: %q", model.Editor().Path())
		}
		updated, cmd := model.Update(tea.KeyMsg{Type: key})
		if isQuit(cmd) {
			t.Fatalf("key %v after a jump must not close the editor", key)
		}
		model = settleProject(updated.(ProjectModel), cmd)
		if model.Editor().Path() != filepath.Join(root, "main.go") {
			t.Fatalf("key %v must go back to the previous file: %q", key, model.Editor().Path())
		}
		if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
			t.Fatalf("key %v: cursor %d,%d", key, row, col)
		}
		// nothing left to go back to: now the key closes the tab, and with the last tab the window
		updated, cmd = model.Update(tea.KeyMsg{Type: key})
		model = settleProject(updated.(ProjectModel), cmd)
		if model.dlg == nil || len(model.openEditors()) != 2 {
			t.Fatalf("key %v without a jump left must ask before it closes the tab", key)
		}
		model, _ = projectKeys(model, "y")
		if len(model.openEditors()) != 1 {
			t.Fatalf("yes closes the tab: %d tabs", len(model.openEditors()))
		}
		updated, cmd = model.Update(tea.KeyMsg{Type: key})
		model = settleProject(updated.(ProjectModel), cmd)
		if model.dlg == nil {
			t.Fatalf("key %v on the last tab must ask before it closes the window", key)
		}
		if _, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter}); !isQuit(cmd) {
			t.Fatalf("yes on the last tab must close the window")
		}
	}
}

func TestEditor_BackStepsThroughSeveralJumpsInOneFile(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	main := filepath.Join(root, "main.go")
	nav.definitions = map[string][]lsp.Place{
		"3:1": {{Path: main, Row: 2, Col: 5}},
		"2:5": {{Path: main, Row: 0, Col: 8}},
	}
	model, _ = projectKeys(model, "j", "j", "j", "l", "g", "d") // to line 3
	model, _ = projectKeys(model, "g", "d")                     // from there to line 1
	if row, _ := model.Editor().Cursor(); row != 0 {
		t.Fatalf("second jump: row %d", row)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(ProjectModel)
	if row, _ := model.Editor().Cursor(); row != 2 {
		t.Fatalf("the first ctrl+c goes back one step: row %d", row)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = updated.(ProjectModel)
	if row, col := model.Editor().Cursor(); row != 3 || col != 1 {
		t.Fatalf("the second one to the start: %d,%d", row, col)
	}
	if !strings.Contains(stripANSI(model.View()), "Back at main.go") && !strings.Contains(stripANSI(model.View()), "main.go") {
		t.Fatal("going back is announced")
	}
}

func TestProject_GoingBackKeepsUnsavedChangesOfTheTab(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	util := filepath.Join(root, "util.go")
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: util, Row: 3, Col: 5}}}
	model, _ = projectKeys(model, "j", "j", "j", "l", "g", "d")
	model, _ = projectKeys(model, "x") // an unsaved change in util.go
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = settleProject(updated.(ProjectModel), cmd)
	if filepath.Base(model.Editor().Path()) != "main.go" || !model.openEditors()[model.tabIndex(util)].Modified() {
		t.Fatalf("going back must show main.go and keep the changes of util.go:\n%s", model.View())
	}
}

func TestEditor_CtrlCStillLeavesInsertModeAndPopupsAfterAJump(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	nav.definitions = map[string][]lsp.Place{"3:1": {{Path: filepath.Join(root, "main.go"), Row: 2, Col: 5}}}
	model, _ = projectKeys(model, "j", "j", "j", "l", "g", "d", "i")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(ProjectModel)
	if isQuit(cmd) || model.Editor().vim.Mode() != vimbuf.Normal || row(model) != 2 {
		t.Fatalf("in insert mode ctrl+c only leaves it (mode %v row %d)", model.Editor().vim.Mode(), row(model))
	}
}

func row(model ProjectModel) int {
	r, _ := model.Editor().Cursor()
	return r
}
