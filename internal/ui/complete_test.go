package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

func ctrlSpace() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlAt} }

// completionEditor opens an editor on text in insert mode at the end of line row.
func completionEditor(t *testing.T, nav CodeNavigator, text string, keys string) EditorModel {
	t.Helper()
	model, _ := newEditorForTest(t, text)
	model = model.WithNavigator(nav, "")
	model, _ = typeEditor(model, keys)
	return model
}

// request presses ctrl+space and feeds the answer back, like the runtime.
func request(model EditorModel) EditorModel {
	updated, cmd := model.Update(ctrlSpace())
	model = updated.(EditorModel)
	if cmd != nil {
		updated, _ = model.Update(cmd())
		model = updated.(EditorModel)
	}
	return model
}

func labels(items []lsp.CompletionItem) []string {
	var out []string
	for _, item := range items {
		out = append(out, item.Label)
	}
	return out
}

func TestCompletion_CtrlSpaceOpensPopupFromServerAndTabAccepts(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{
		{Label: "println", Kind: 3, Detail: "func(a ...any)"}, {Label: "print", Kind: 3}, {Label: "printf", Kind: 3}, {Label: "other"},
	}}
	model := completionEditor(t, nav, "fmt.\nvar x = 1\n", "A"+"pr")
	model = request(model)
	if model.completion == nil || strings.Join(labels(model.completion.items), ",") != "print,printf,println" {
		t.Fatalf("the popup must list the matches of the typed prefix: %+v", model.completion)
	}
	if len(nav.completeAt) != 1 || nav.completeAt[0] != [2]int{0, 6} {
		t.Fatalf("the server must be asked at the cursor: %v", nav.completeAt)
	}
	view := stripANSI(model.View())
	for _, want := range []string{"println", "func(a ...any)", "printf", "3 suggestions", "language server"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	model, _ = typeEditor(model, "i")   // typing narrows the list: "pri"
	model, _ = typeEditor(model, "ntf") // "printf"
	if model.completion == nil || strings.Join(labels(model.completion.items), ",") != "printf" {
		t.Fatalf("typing must narrow the list: %+v", model.completion)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(EditorModel)
	if model.completion != nil || model.Lines()[0] != "fmt.printf" {
		t.Fatalf("tab must insert the suggestion: %q", model.Lines())
	}
	if row, col := model.Cursor(); row != 0 || col != len("fmt.printf") {
		t.Fatalf("cursor %d,%d", row, col)
	}
}

func TestCompletion_NavigationAcceptWithEnterAndEscapeClosesOnlyThePopup(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "alpha"}, {Label: "alpine"}, {Label: "alps"}}}
	model := completionEditor(t, nav, "\n", "Aal")
	model = request(model)
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyUp} {
		updated, _ := model.Update(tea.KeyMsg{Type: key})
		model = updated.(EditorModel)
	}
	if model.completion.sel != 1 {
		t.Fatalf("selection %d", model.completion.sel)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(EditorModel)
	if model.Lines()[0] != "alpine" || len(model.Lines()) != 1 {
		t.Fatalf("enter accepts and does not add a line: %q", model.Lines())
	}
	model, _ = typeEditor(model, " al")
	model = request(model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(EditorModel)
	if model.completion != nil || model.vim.Mode() != vimbuf.Insert {
		t.Fatalf("the first esc closes the popup only (mode %v)", model.vim.Mode())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(EditorModel).vim.Mode() == vimbuf.Insert {
		t.Fatal("the second esc leaves insert mode")
	}
}

func TestCompletion_FallsBackToWordsOfTheFile(t *testing.T) {
	model := completionEditor(t, nil, "counter := 1\nconcat := 2\nvalue\n\n", "GAcon")
	model = request(model)
	if model.completion == nil || strings.Join(labels(model.completion.items), ",") != "concat,counter" { // the prefix match comes first
		t.Fatalf("words of the file must be offered without a server: %+v", model.completion)
	}
	if model.completion.source != "words in the file" {
		t.Fatalf("source %q", model.completion.source)
	}
	model = completionEditor(t, &fakeNav{}, "counter := 1\nconcat := 2\n\n", "GAco") // a server without answers
	model = request(model)
	if model.completion == nil || len(model.completion.items) != 2 {
		t.Fatalf("an empty server answer falls back to words: %+v", model.completion)
	}
}

func TestCompletion_ServerTextEditReplacesItsRange(t *testing.T) {
	edit := &lsp.TextEdit{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 4}, End: lsp.Position{Line: 0, Character: 6}}, NewText: "Println"}
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "Println", Edit: edit}}}
	model := completionEditor(t, nav, "fmt.Pr\n", "A")
	model = request(model)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(EditorModel).Lines()[0]; got != "fmt.Println" {
		t.Fatalf("got %q", got)
	}
}

func TestCompletion_InsertTextWinsOverLabelAndMultilineWorks(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "if", InsertText: "if x {\n\t\n}"}}}
	model := completionEditor(t, nav, "\n", "Aif")
	model = request(model)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(EditorModel).Lines()
	if strings.Join(got, "|") != "if x {|\t|}" {
		t.Fatalf("got %q", got)
	}
}

func TestCompletion_ClosesWhenTheWordEndsOrCursorLeaves(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "alpha"}, {Label: "alpine"}}}
	for name, keys := range map[string]string{"a space": " ", "a dot": ".", "no match": "zz"} {
		model := completionEditor(t, nav, "\n", "Aal")
		model = request(model)
		model, _ = typeEditor(model, keys)
		if model.completion != nil {
			t.Errorf("%s must close the popup", name)
		}
	}
	model := completionEditor(t, nav, "x\n", "Aal")
	model = request(model)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.(EditorModel).completion != nil {
		t.Error("deleting back before the start of the word must close the popup")
	}
}

func TestCompletion_IgnoresLateAnswerAfterCursorMoved(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "alpha"}}}
	model := completionEditor(t, nav, "\n", "Aal")
	updated, cmd := model.Update(ctrlSpace())
	model = updated.(EditorModel)
	msg := cmd()
	model, _ = typeEditor(model, "<esc>")
	updated, _ = model.Update(msg)
	if updated.(EditorModel).completion != nil {
		t.Fatal("an answer that arrives after leaving insert mode must be dropped")
	}
}

func TestCompletion_CtrlNOpensAndDoesNotChangeNormalMode(t *testing.T) {
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "alpha"}, {Label: "alpine"}}}
	model := completionEditor(t, nav, "\nb\n", "Aal")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	model = updated.(EditorModel)
	updated, _ = model.Update(cmd())
	if updated.(EditorModel).completion == nil {
		t.Fatal("ctrl+n in insert mode must open the popup")
	}
	normal, _ := newEditorForTest(t, "a\nb\n")
	updated, _ = normal.Update(ctrlSpace())
	if updated.(EditorModel).completion != nil {
		t.Fatal("ctrl+space outside insert mode does nothing")
	}
}

func TestCompletion_ErrorsAreShownButWordsStillWork(t *testing.T) {
	nav := &fakeNav{err: errors.New("server crashed")}
	model := completionEditor(t, nav, "\ncounter\n", "Aco")
	model = request(model)
	if model.completion == nil || !strings.Contains(stripANSI(model.View()), "Completion: server crashed") {
		t.Fatalf("view:\n%s", model.View())
	}
	missing := &fakeNav{err: &lsp.NotInstalledError{Server: "gopls", Command: "gopls"}}
	model = completionEditor(t, missing, "\ncounter\n", "Aco")
	model = request(model)
	if model.completion == nil {
		t.Fatal("a missing server falls back to words without noise")
	}
	if strings.Contains(stripANSI(model.View()), "Completion:") {
		t.Fatal("a missing server is not an error in the middle of typing")
	}
	empty := completionEditor(t, nil, "x\n", "Azzzz")
	empty = request(empty)
	if empty.completion != nil || !strings.Contains(stripANSI(empty.View()), "No completions") {
		t.Fatalf("view:\n%s", empty.View())
	}
}

func TestCompletion_PopupFitsTheScreenAndMovesAboveAtTheBottom(t *testing.T) {
	var items []lsp.CompletionItem
	for _, name := range []string{"alpha", "alpine", "alps", "alpha_beta", "alphabet", "alpacas", "alpinist", "alpaca_cart", "alphanumeric", "alpenglow"} {
		items = append(items, lsp.CompletionItem{Label: name, Detail: strings.Repeat("d", 80)})
	}
	nav := &fakeNav{completions: items}
	for _, size := range [][2]int{{20, 8}, {50, 10}, {120, 24}} {
		model := completionEditor(t, nav, "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n\n", "GAal")
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model = request(updated.(EditorModel))
		lines := strings.Split(model.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("size %v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Fatalf("size %v: line %d is %d wide", size, i, w)
			}
		}
		if model.completion == nil || !strings.Contains(stripANSI(model.View()), "alpaca_cart") { // the first of them, they are sorted
			t.Fatalf("size %v: the popup must be visible:\n%s", size, model.View())
		}
	}
}

func TestCompletion_RankingPrefersPrefixThenContainsThenLetters(t *testing.T) {
	items := []lsp.CompletionItem{{Label: "myAppend"}, {Label: "append"}, {Label: "apple"}, {Label: "unrelated"}, {Label: "a_p_p"}}
	got := labels(rankCompletions(items, "app"))
	if strings.Join(got, ",") != "append,apple,myAppend,a_p_p" {
		t.Fatalf("got %v", got)
	}
	if len(rankCompletions(items, "")) != 5 {
		t.Fatal("an empty prefix shows everything")
	}
	withSort := []lsp.CompletionItem{{Label: "b", SortText: "1"}, {Label: "a", SortText: "2"}}
	if got := labels(rankCompletions(withSort, "")); got[0] != "b" {
		t.Fatalf("sortText orders items: %v", got)
	}
}

// An auto import comes as an additional edit: the name is completed and the import line is added at the top, the cursor stays behind the name.
func TestCompletion_AutoImportAddsTheImportLineAndKeepsTheCursorBehindTheName(t *testing.T) {
	importLine := lsp.TextEdit{NewText: "import { signal } from '@angular/core';\n"}
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "signal", Kind: 3, Additional: []lsp.TextEdit{importLine}}}}
	model := completionEditor(t, nav, "export class A {\n  count = \n}\n", "j$a"+"sig")
	model = request(model)
	if model.completion == nil {
		t.Fatalf("the popup opens: %v", model.vim.Lines())
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(EditorModel)
	lines := model.vim.Lines()
	if lines[0] != "import { signal } from '@angular/core';" || lines[1] != "export class A {" || !strings.HasSuffix(lines[2], "count = signal") {
		t.Fatalf("the import is added above and the name completed: %q", lines)
	}
	if row, col := model.vim.Cursor(); row != 2 || col != len([]rune(lines[2])) {
		t.Fatalf("the cursor stays behind the name: %d:%d", row, col)
	}
}

type warmNav struct {
	*fakeNav
	warmed chan string
}

func (w warmNav) Warm(_ context.Context, path, _ string) error {
	w.warmed <- path
	return nil
}

// The server loads the project after a file is opened: opening the editor opens the file in the server, before the first completion.
func TestWithNavigator_OpensTheFileInTheServerRightAway(t *testing.T) {
	model, path := newEditorForTest(t, "x\n")
	nav := warmNav{fakeNav: &fakeNav{}, warmed: make(chan string, 1)}
	model.doc.Path = path
	model.WithNavigator(nav, "")
	select {
	case got := <-nav.warmed:
		if got != path {
			t.Fatalf("the file of the editor is warmed: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the file was not opened in the server")
	}
}

func TestRankCompletions_OrderIgnoresTheCaseOfTheLetters(t *testing.T) {
	all := []lsp.CompletionItem{{Label: "Zebra"}, {Label: "apple"}, {Label: "Banana"}, {Label: "cherry"}}
	if got := strings.Join(labels(rankCompletions(all, "")), ","); got != "apple,Banana,cherry,Zebra" {
		t.Fatalf("sorted ignoring case: %s", got)
	}
	words := bufferWords([]string{"Zebra apple Banana"}, "")
	if got := strings.Join(labels(words), ","); got != "apple,Banana,Zebra" {
		t.Fatalf("the words of the file too: %s", got)
	}
}
