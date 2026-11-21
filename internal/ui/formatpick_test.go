package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

func formatChoices() []lsp.FormatterChoice {
	return []lsp.FormatterChoice{
		{Name: "gofmt", Command: []string{"gofmt"}, Detail: "Go", Installed: true},
		{Name: "prettier", Command: []string{"prettier", "--stdin-filepath", "{file}"}, Detail: "JavaScript, HTML, CSS, JSON", Installed: true},
		{Name: "black", Command: []string{"black", "-q", "-"}, Detail: "Python", Installed: false},
	}
}

func typeIntoPicker(model ProjectModel, text string) ProjectModel {
	for _, r := range text {
		model, _ = projectKeys(model, string(r))
	}
	return model
}

// :format finds no formatter: a searchable list opens, typing narrows it, enter formats with the one chosen and remembers it.
func TestFormatPicker_OpensWhenNoFormatterIsFoundAndFormatsWithTheChosenOne(t *testing.T) {
	for _, err := range []error{lsp.ErrNoServer, lsp.ErrCannotFormat, &lsp.NotInstalledError{Server: "gopls", Command: "gopls"}} {
		nav := &fakeNav{err: err, choices: formatChoices(), withText: "package main\n\nfunc main() {\n}\n// by prettier\n"}
		model, _ := navProject(t, nav)
		model, _ = typeCommand(model, "format")
		view := stripANSI(model.View())
		if model.pick == nil || !strings.Contains(view, "Choose a formatter for main.go") {
			t.Fatalf("%v: the list must open:\n%s", err, view)
		}
		for _, want := range []string{"gofmt", "prettier", "black", "(not installed)"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%v: the list lacks %q:\n%s", err, want, view)
			}
		}
		model = typeIntoPicker(model, "css") // matches the detail of prettier only
		if view = stripANSI(model.View()); !strings.Contains(view, "prettier") || strings.Contains(view, "gofmt") {
			t.Fatalf("typing must narrow the list:\n%s", view)
		}
		model, _ = projectKeys(model, "enter")
		if model.pick != nil {
			t.Fatal("choosing closes the list")
		}
		lines := model.Editor().Lines()
		if lines[len(lines)-1] != "// by prettier" {
			t.Fatalf("the text must be formatted by the chosen formatter: %q", lines)
		}
		if len(nav.remembered) != 1 || nav.remembered[0] != "main.go:prettier" {
			t.Fatalf("a working choice is remembered for the file type: %v", nav.remembered)
		}
		if view = stripANSI(model.View()); !strings.Contains(view, "prettier is used for this file type") {
			t.Fatalf("the user must be told that the choice is remembered:\n%s", view)
		}
	}
}

func TestFormatPicker_CancelAndMissingToolsAndFailuresChangeNothing(t *testing.T) {
	nav := &fakeNav{err: lsp.ErrNoServer, choices: formatChoices()}
	model, _ := navProject(t, nav)
	model, _ = typeCommand(model, "format")
	if model, _ = projectKeys(model, "esc"); model.pick != nil || model.Editor().Modified() || len(nav.remembered) != 0 {
		t.Fatal("esc closes the list and leaves everything as it is")
	}

	model, _ = typeCommand(model, "format")
	model = typeIntoPicker(model, "black")
	model, _ = projectKeys(model, "enter")
	if view := stripANSI(model.View()); model.pick != nil || !strings.Contains(view, "black is not installed") || len(nav.remembered) != 0 {
		t.Fatalf("a missing formatter is reported, not run:\n%s", view)
	}
	for _, asked := range nav.asked {
		if strings.HasPrefix(asked, "with ") {
			t.Fatalf("a missing formatter must not be run: %v", nav.asked)
		}
	}

	failing := &fakeNav{err: lsp.ErrNoServer, choices: formatChoices(), withErr: errors.New("syntax error at 3:4")}
	model, _ = navProject(t, failing)
	model, _ = typeCommand(model, "format")
	model, _ = projectKeys(model, "enter") // gofmt, the first entry
	if view := stripANSI(model.View()); !strings.Contains(view, "Format failed: syntax error at 3:4") || len(failing.remembered) != 0 || model.Editor().Modified() {
		t.Fatalf("a failing formatter is reported and not remembered:\n%s", view)
	}
}

func TestFormatPicker_NothingMatchesAndClicksChoose(t *testing.T) {
	nav := &fakeNav{err: lsp.ErrNoServer, choices: formatChoices(), withText: "x\n"}
	model, _ := navProject(t, nav)
	model, _ = typeCommand(model, "format")
	model = typeIntoPicker(model, "zzzz")
	if view := stripANSI(model.View()); !strings.Contains(view, "nothing matches") {
		t.Fatalf("an empty result says so:\n%s", view)
	}
	if model, _ = projectKeys(model, "enter"); model.pick == nil {
		t.Fatal("enter on an empty list does nothing")
	}
	for range "zzzz" {
		model, _ = projectKeys(model, "backspace")
	}
	// a click on the second entry (prettier) chooses it: the first entry is two rows under the top of the box
	x, y := model.pick.picker.origin(model.width, model.height)
	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x + 3, Y: y + 2 + 1})
	model = settleProject(updated.(ProjectModel), cmd)
	if model.pick != nil || len(nav.asked) < 2 || nav.asked[len(nav.asked)-1] != "with prettier main.go" {
		t.Fatalf("a click on an entry formats with it: %v", nav.asked)
	}
}

func TestFormatPicker_WithoutProjectWindowTheOldMessagesStay(t *testing.T) {
	editor, _ := newEditorForTest(t, "x\n")
	editor = editor.WithNavigator(&fakeNav{err: lsp.ErrNoServer}, t.TempDir())
	editor, _ = typeEditor(editor, ":format<cr>")
	updated, _ := editor.handleFormatResult(formatResultMsg{original: "x", err: lsp.ErrNoServer})
	if !strings.Contains(updated.status, "No formatter for this file type") {
		t.Fatalf("a standalone editor has no window to ask: %q", updated.status)
	}
}

// A formatter whose result does not apply is not remembered: the text changed under it, or it failed.
func TestFormatPicker_ChoiceIsRememberedOnlyWhenItsResultWasApplied(t *testing.T) {
	nav := &fakeNav{err: lsp.ErrNoServer, choices: formatChoices(), withText: "package main\n// by gofmt\n"}
	model, _ := navProject(t, nav)
	model, _ = typeCommand(model, "format")
	model, cmd := projectKeys(model, "enter")
	_ = cmd
	// the text was changed while the formatter ran: the result is dropped and nothing is remembered
	model.editor.vim.ReplaceLines([]string{"changed by the user"})
	updated, _ := model.Update(formatResultMsg{original: "package main", formatted: "x\n", remember: &lsp.FormatterChoice{Name: "gofmt"}})
	if len(nav.remembered) > 1 || strings.Contains(stripANSI(updated.(ProjectModel).View()), "gofmt is used for this file type") {
		t.Fatalf("a dropped result must not be remembered: %v", nav.remembered)
	}
}

// A standalone editor (no project window) keeps the old messages for every kind of "no formatter".
func TestEditor_WithoutProjectWindowKeepsTheOldFormatMessages(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&lsp.NotInstalledError{Server: "gopls", Command: "gopls"}, "gopls not found"},
		{lsp.ErrNoServer, "No formatter for this file type"},
		{lsp.ErrCannotFormat, "cannot format this file"},
	}
	for _, tt := range tests {
		editor, _ := newEditorForTest(t, "x\n")
		updated, cmd := editor.handleFormatResult(formatResultMsg{original: "x", err: tt.err})
		if !strings.Contains(updated.status, tt.want) || cmd != nil {
			t.Errorf("%v: status %q lacks %q", tt.err, updated.status, tt.want)
		}
	}
}
