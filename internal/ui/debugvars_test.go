package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/editor"
)

// settle delivers the answers of the variable requests that a stop started (the fake answers at once).
func (fx *debugFixture) settle(t *testing.T) {
	t.Helper()
	fx.model = update(fx.model, answerOf(t, fx.model.fetchVariables(), func(msg tea.Msg) bool { _, ok := msg.(debugScopesMsg); return ok }))
}

func typeInPanel(m ProjectModel, text string) ProjectModel {
	for _, r := range text {
		m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestDebugVariables_AStopShowsTheScopesTheFirstOneOpenAndOpensChildrenWhenAsked(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	view := stripANSI(fx.model.View())
	for _, want := range []string{"Variables", "▾ Locals", "x = 21  (int)", "▸ user = {...}", "▸ Globals"} {
		if !strings.Contains(view, want) {
			t.Fatalf("%q is missing:\n%s", want, view)
		}
	}
	if strings.Contains(view, "version") || strings.Contains(view, "Ann") {
		t.Fatalf("the other scope and the children are read when they are opened:\n%s", view)
	}
	fx.model.focus = focusDebug
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown}) // x
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown}) // user
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyRight})
	fx.model = updated.(ProjectModel)
	fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugChildrenMsg); return ok }))
	if view := stripANSI(fx.model.View()); !strings.Contains(view, "▾ user = {...}") || !strings.Contains(view, `Name = "Ann"`) {
		t.Fatalf("user is open with its field:\n%s", view)
	}
	before := len(fx.session.called())
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyLeft})  // close
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEnter}) // open again: nothing is read twice
	if len(fx.session.called()) != before || !strings.Contains(stripANSI(fx.model.View()), `Name = "Ann"`) {
		t.Fatalf("children are read once: %v", fx.session.called()[before:])
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyTab})
	if fx.model.dbg.view != viewStack || !strings.Contains(stripANSI(fx.model.View()), "main.main  main.go:5") {
		t.Fatal("tab switches to the call stack")
	}
}

func TestDebugVariables_AreDroppedWhenTheProgramGoesOnAndOldAnswersAreIgnored(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	late := answerOf(t, fx.model.fetchVariables(), func(msg tea.Msg) bool { _, ok := msg.(debugScopesMsg); return ok })
	fx.event("continued", map[string]any{"threadId": 1})
	if len(fx.model.dbg.vars) != 0 {
		t.Fatal("continuing drops the variables")
	}
	fx.model = update(fx.model, late) // an answer for the stop before
	if len(fx.model.dbg.vars) != 0 {
		t.Fatalf("an old answer must not come back: %+v", fx.model.dbg.vars)
	}
	if view := stripANSI(fx.model.View()); !strings.Contains(view, "running") {
		t.Fatalf("a running program says so:\n%s", view)
	}
}

func TestDebugEvaluate_TypedInThePanelAndWithTheEvalCommand(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	if m, cmd := fx.model.evaluate("x"); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "not stopped") {
		t.Fatalf("a running program cannot evaluate: %q", status(m.(ProjectModel)))
	}
	fx.stop(t)
	fx.settle(t)
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "x * 2")
	if view := stripANSI(fx.model.View()); !strings.Contains(view, "> x * 2") {
		t.Fatalf("the typed expression is shown:\n%s", view)
	}
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fx.model = updated.(ProjectModel)
	fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugEvalMsg); return ok }))
	console := strings.Join(fx.model.dbg.console, "\n")
	if !strings.Contains(console, "> x * 2\n  42  (int)") || len(fx.model.dbg.input) != 0 {
		t.Fatalf("console: %q input %q", console, string(fx.model.dbg.input))
	}
	if calls := fx.session.called(); calls[len(calls)-1] != "evaluate 1 x * 2" {
		t.Fatalf("the expression runs in the selected frame: %v", calls)
	}
	if !strings.Contains(status(fx.model), "x * 2 = 42") {
		t.Fatalf("the value is also said: %q", status(fx.model))
	}
	m, cmd := fx.model.runProjectCommand("eval boom")
	fx.model = update(m.(ProjectModel), answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugEvalMsg); return ok }))
	if console := strings.Join(fx.model.dbg.console, "\n"); !strings.Contains(console, "> boom\n  could not find symbol boom") || !fx.model.editor.warn {
		t.Fatalf("an error is shown as one: %q", console)
	}
	fx.model.dbg.input = []rune("ab")
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyBackspace})
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEsc}) // clears the input first
	if len(fx.model.dbg.input) != 0 || fx.model.focus != focusDebug {
		t.Fatalf("esc clears the line before it leaves: %q focus %d", string(fx.model.dbg.input), fx.model.focus)
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEsc})
	if fx.model.focus != focusProjectEdit {
		t.Fatal("esc on an empty line leaves the panel")
	}
}

func TestDebugEvaluate_WithoutAnArgumentItTakesTheNameUnderTheCursor(t *testing.T) {
	fx := debugProject(t)
	if m, _ := fx.model.runProjectCommand("eval x"); !strings.Contains(status(m.(ProjectModel)), "no program") {
		t.Fatal("without a run there is nothing to evaluate")
	}
	fx.start(t)
	fx.stop(t) // the editor is on line 5: "\t_ = x"
	fx.model.editor = fx.model.editor.At(4, 5)
	m, cmd := fx.model.runProjectCommand("eval")
	if cmd == nil {
		t.Fatalf("the name under the cursor is evaluated: %q", status(m.(ProjectModel)))
	}
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugEvalMsg); return ok })
	if calls := fx.session.called(); calls[len(calls)-1] != "evaluate 1 x" {
		t.Fatalf("calls: %v", calls)
	}
	fx.model.editor = fx.model.editor.At(4, 0) // on the tab: no name
	if m, cmd := fx.model.runProjectCommand("eval"); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "nothing to evaluate") {
		t.Fatalf("no name: %q", status(m.(ProjectModel)))
	}
}

func TestExpressionAtCursor_NameWithDotsSelectionAndNothing(t *testing.T) {
	line := func(text string) EditorModel {
		return NewEditorModel(editor.Document{Lines: []string{text}, LineEnding: "\n"})
	}
	for _, c := range []struct {
		text string
		col  int
		want string
	}{{"fmt.Println(user.name)", 14, "user.name"}, {"fmt.Println(user.name)", 2, "fmt.Println"}, {"a := 1", 3, ""}, {"x", 0, "x"}, {"", 0, ""}, {"end.", 2, "end"}} {
		e := line(c.text).At(0, c.col)
		if got := e.ExpressionAtCursor(); got != c.want {
			t.Errorf("%q at %d: %q, want %q", c.text, c.col, got, c.want)
		}
	}
	e := line("total := price * qty").At(0, 9)
	for _, key := range []string{"v", "e"} { // select "price"
		updated, _ := e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		e = updated.(EditorModel)
	}
	if got := e.ExpressionAtCursor(); got != "price" {
		t.Errorf("the visual selection is the expression: %q", got)
	}
}

func TestDebugVariables_TheStopStartsTheReadOfTheVariablesByItself(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1})
	updated, cmd := fx.model.Update(fx.model.debugStack(fx.model.dbg)())
	fx.model = updated.(ProjectModel)
	fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugScopesMsg); return ok }))
	if rows := fx.model.dbg.flatVars(); len(rows) < 3 || !strings.Contains(rows[1].text(), "x = 21") {
		t.Fatalf("the stack answer starts the read of the variables: %v", rows)
	}
}

func TestDebugEvaluate_RefusalKeepsTheTypedTextAndALateAnswerIsDropped(t *testing.T) {
	fx := debugProject(t)
	fx.start(t) // running
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "x")
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEnter})
	if string(fx.model.dbg.input) != "x" {
		t.Fatalf("the program runs: the text stays, got %q", string(fx.model.dbg.input))
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z"), Alt: true})
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb\x01")})
	if string(fx.model.dbg.input) != "xab" {
		t.Fatalf("alt keys are ignored and pasted control characters dropped: %q", string(fx.model.dbg.input))
	}
	fx.model.dbg.input = nil
	fx.stop(t)
	_, cmd := fx.model.evaluate("x")
	late := answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugEvalMsg); return ok })
	fx.event("continued", map[string]any{"threadId": 1})
	console := len(fx.model.dbg.console)
	fx.model = update(fx.model, late)
	if len(fx.model.dbg.console) != console {
		t.Fatalf("an answer for an earlier stop must not reach the console: %q", fx.model.dbg.console)
	}
}
