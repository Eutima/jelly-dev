package ui

import (
	"github.com/eutima/jelly-dev/internal/dap"
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
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown, Alt: true}) // x
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown, Alt: true}) // user
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyEnter})       // enter on an empty line opens it
	fx.model = updated.(ProjectModel)
	fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugChildrenMsg); return ok }))
	if view := stripANSI(fx.model.View()); !strings.Contains(view, "▾ user = {...}") || !strings.Contains(view, `Name = "Ann"`) {
		t.Fatalf("user is open with its field:\n%s", view)
	}
	before := len(fx.session.called())
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEnter}) // close
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEnter}) // open again: nothing is read twice
	if len(fx.session.called()) != before || !strings.Contains(stripANSI(fx.model.View()), `Name = "Ann"`) {
		t.Fatalf("children are read once: %v", fx.session.called()[before:])
	}
	fx.model.dbg.pane = paneList
	fx.model = keyType(fx.model, tea.KeyRight) // in the list: right shows the call stack, left the variables
	if fx.model.dbg.view != viewStack || !strings.Contains(stripANSI(fx.model.View()), "main.main  main.go:5") {
		t.Fatal("right switches to the call stack")
	}
	fx.model = keyType(fx.model, tea.KeyLeft)
	if fx.model.dbg.view != viewVariables {
		t.Fatal("left switches back to the variables")
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

func keyType(m ProjectModel, t tea.KeyType) ProjectModel { return update(m, tea.KeyMsg{Type: t}) }

func TestDebugConsole_TheLineForExpressionsIsALineEditor(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "abde")
	fx.model = keyType(fx.model, tea.KeyLeft)
	fx.model = keyType(fx.model, tea.KeyLeft)
	fx.model = typeInPanel(fx.model, "c") // abcde
	if string(fx.model.dbg.input) != "abcde" || fx.model.dbg.cursor != 3 {
		t.Fatalf("typing goes in at the cursor: %q at %d", string(fx.model.dbg.input), fx.model.dbg.cursor)
	}
	if view := stripANSI(fx.model.View()); !strings.Contains(view, "> abcde") || strings.Contains(view, "\u258f") {
		t.Fatalf("the text stays together, the cursor is a highlighted cell and no extra character:\n%s", view)
	}
	fx.model = keyType(fx.model, tea.KeyDelete) // d
	fx.model = keyType(fx.model, tea.KeyBackspace)
	if string(fx.model.dbg.input) != "abe" || fx.model.dbg.cursor != 2 {
		t.Fatalf("delete removes at the cursor, backspace before it: %q at %d", string(fx.model.dbg.input), fx.model.dbg.cursor)
	}
	fx.model = keyType(fx.model, tea.KeyHome)
	if fx.model.dbg.cursor != 0 {
		t.Fatal("home")
	}
	fx.model = keyType(fx.model, tea.KeyBackspace) // nothing before the cursor
	fx.model = keyType(fx.model, tea.KeyEnd)
	fx.model = keyType(fx.model, tea.KeyRight) // not past the end
	if string(fx.model.dbg.input) != "abe" || fx.model.dbg.cursor != 3 {
		t.Fatalf("end and right stop at the end: %q at %d", string(fx.model.dbg.input), fx.model.dbg.cursor)
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(fx.model.dbg.input) != 0 || fx.model.dbg.cursor != 0 {
		t.Fatal("ctrl+u clears the line")
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("日本")})
	fx.model = keyType(fx.model, tea.KeyLeft)
	fx.model = typeInPanel(fx.model, "x")
	if string(fx.model.dbg.input) != "日x本" {
		t.Fatalf("wide characters count as one: %q", string(fx.model.dbg.input))
	}
}

func TestDebugConsole_UpAndDownBringBackTheLinesTypedBeforeAndKeepTheDraft(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	evaluate := func(expression string) {
		fx.model = typeInPanel(fx.model, expression)
		updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		fx.model = updated.(ProjectModel)
		fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugEvalMsg); return ok }))
	}
	evaluate("first")
	evaluate("second")
	evaluate("second") // the same line twice is remembered once
	if got := strings.Join(fx.model.evalHistory, ","); got != "first,second" {
		t.Fatalf("history: %q", got)
	}
	fx.model = typeInPanel(fx.model, "dra")
	fx.model = keyType(fx.model, tea.KeyUp)
	if string(fx.model.dbg.input) != "second" || fx.model.dbg.cursor != 6 {
		t.Fatalf("up brings back the last line: %q", string(fx.model.dbg.input))
	}
	fx.model = keyType(fx.model, tea.KeyUp)
	fx.model = keyType(fx.model, tea.KeyUp) // already the oldest
	if string(fx.model.dbg.input) != "first" {
		t.Fatalf("up again: %q", string(fx.model.dbg.input))
	}
	fx.model = keyType(fx.model, tea.KeyDown)
	fx.model = keyType(fx.model, tea.KeyDown)
	if string(fx.model.dbg.input) != "dra" {
		t.Fatalf("down past the newest brings back what was being typed: %q", string(fx.model.dbg.input))
	}
	fx.model = keyType(fx.model, tea.KeyDown)
	if string(fx.model.dbg.input) != "dra" {
		t.Fatal("down at the bottom changes nothing")
	}
	// the history outlives the run
	fx.model.stopDebugNow(true)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model.dbg.pane = paneConsole
	fx.model = keyType(fx.model, tea.KeyUp)
	if string(fx.model.dbg.input) != "second" {
		t.Fatalf("a new run starts with the old history: %q", string(fx.model.dbg.input))
	}
	// alt+up and alt+down move the list on the left, not the history
	fx.settle(t)
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if fx.model.dbg.varSel != 1 || string(fx.model.dbg.input) != "second" {
		t.Fatalf("alt+down chooses a row: %d %q", fx.model.dbg.varSel, string(fx.model.dbg.input))
	}
}

func completeNow(t *testing.T, fx *debugFixture) {
	t.Helper()
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyTab})
	fx.model = updated.(ProjectModel)
	if cmd != nil {
		fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugCompleteMsg); return ok }))
	}
}

func TestDebugConsole_TabCompletesWhatTheAdapterSuggests(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "us")
	completeNow(t, fx)
	if string(fx.model.dbg.input) != "user" || fx.model.dbg.cursor != 4 {
		t.Fatalf("one suggestion is put in: %q at %d", string(fx.model.dbg.input), fx.model.dbg.cursor)
	}
	if calls := fx.session.called(); calls[len(calls)-1] != "completions 1 2 us" {
		t.Fatalf("the adapter is asked with the frame and the cursor: %v", calls)
	}
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "print(re")
	completeNow(t, fx)
	// request and requests: the common start "request" is put in, replacing "re" only
	if string(fx.model.dbg.input) != "print(request" {
		t.Fatalf("what the suggestions have in common: %q", string(fx.model.dbg.input))
	}
	if popup := fx.model.dbg.popup; popup == nil || len(popup.items) != 2 || popup.start != 6 {
		t.Fatalf("what is left to choose is offered in a popup: %+v", popup)
	}
	fx.model = keyType(fx.model, tea.KeyEsc)
	if fx.model.dbg.popup != nil || string(fx.model.dbg.input) != "print(request" {
		t.Fatal("esc closes the popup and leaves the line")
	}
	completeNow(t, fx) // nothing more in common: the choices are offered again
	if fx.model.dbg.popup == nil || len(fx.model.dbg.popup.items) != 2 {
		t.Fatal("tab opens the popup again")
	}
	fx.model = keyType(fx.model, tea.KeyEsc)
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "ab")
	completeNow(t, fx)
	if popup := fx.model.dbg.popup; string(fx.model.dbg.input) != "ab" || popup == nil || len(popup.items) != 2 {
		t.Fatalf("a start that is missing replaces the name before the cursor; no common start beyond it, so they are offered: %q %+v", string(fx.model.dbg.input), popup)
	}
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "zz")
	completeNow(t, fx) // the adapter has nothing
	if string(fx.model.dbg.input) != "zz" {
		t.Fatal("no suggestion, no change")
	}
}

func TestDebugConsole_WithoutAdapterCompletionTheNamesOnScreenAreUsedAndTabOnAnEmptyLineSwitchesTheList(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	fx.model.focus = focusDebug
	fx.model.dbg.varSel = 2 // user
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fx.model = update(updated.(ProjectModel), answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugChildrenMsg); return ok })) // open user
	fx.model = typeInPanel(fx.model, "qq")                                                                                             // the fake adapter refuses this one
	completeNow(t, fx)
	if got := string(fx.model.dbg.input); got != "qq" {
		t.Fatalf("no name starts with qq: %q", got)
	}
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "user.Na")
	// the adapter says nothing for this text, so the names of the opened variables answer
	completeNow(t, fx)
	if got := string(fx.model.dbg.input); got != "user.Name" {
		t.Fatalf("the field of an opened variable: %q", got)
	}
	fx.model.dbg.clearInput()
	fx.model = keyType(fx.model, tea.KeyTab) // an empty line: tab goes back to the list
	if fx.model.dbg.pane != paneList {
		t.Fatal("tab on an empty line gives the keys back to the list")
	}
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "x")
	fx.model.dbg.state = debugRunning
	m, cmd := fx.model.complete()
	if cmd != nil || !strings.Contains(status(m.(ProjectModel)), "stopped") {
		t.Fatalf("a running program cannot complete: %q", status(m.(ProjectModel)))
	}
}

func TestDebugConsole_ASuggestionForAnOlderLineIsDropped(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "us")
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyTab})
	fx.model = updated.(ProjectModel)
	late := answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugCompleteMsg); return ok })
	fx.model = typeInPanel(fx.model, "x") // typed while the answer was on its way
	fx.model = update(fx.model, late)
	if string(fx.model.dbg.input) != "usx" {
		t.Fatalf("the line was changed since: %q", string(fx.model.dbg.input))
	}
}

func TestDebugConsole_LocalNamesSkipScopesAndWriteArrayElementsWithoutADot(t *testing.T) {
	run := &debugRun{}
	locals := &varNode{name: "Locals", ref: 1, open: true, loaded: true, children: []*varNode{
		{name: "arr", ref: 5, open: true, loaded: true, children: []*varNode{{name: "[0]"}, {name: "[1]"}}},
		{name: "answer"},
	}}
	run.vars = []*varNode{locals, {name: "Globals", ref: 2}}
	words := func(text string) []string {
		var out []string
		for _, item := range run.localCompletions([]rune(text), len([]rune(text))) {
			out = append(out, item.Label)
		}
		return out
	}
	if got := words("Glo"); len(got) != 0 {
		t.Fatalf("a scope is no name: %v", got)
	}
	if got := words("a"); strings.Join(got, ",") != "answer,arr,arr[0],arr[1]" {
		t.Fatalf("variables of the open scope: %v", got)
	}
	if got := words("arr["); strings.Join(got, ",") != "arr[0],arr[1]" {
		t.Fatalf("elements are joined without a dot: %v", got)
	}
}

func TestDebugConsole_SuggestionsForDifferentPartsOfTheLineAreListedNotMerged(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.dbg.input = []rune("ab")
	fx.model.dbg.cursor = 2
	items := []dap.Completion{{Label: "abc", Start: 0, Length: 2}, {Label: "abd", Start: 1, Length: 1}}
	fx.model = update(fx.model, debugCompleteMsg{run: fx.model.dbg, gen: fx.model.dbg.gen, text: "ab", cursor: 2, items: items})
	if string(fx.model.dbg.input) != "ab" || !strings.Contains(strings.Join(fx.model.dbg.console, "\n"), "  abc  abd") {
		t.Fatalf("different ranges are only listed: %q", string(fx.model.dbg.input))
	}
}

func TestInputView_PlacesTheCursorCellAlsoWhenTheLineWraps(t *testing.T) {
	lines, row, col := inputView([]rune("abc"), 1, 20)
	if len(lines) != 1 || row != 0 || col != 3 { // "> abc ": the cursor is on the b, the fourth cell
		t.Fatalf("%q row %d col %d", lines, row, col)
	}
	_, row, col = inputView([]rune("abc"), 3, 20)
	if row != 0 || col != 5 { // behind the last character: the blank cell
		t.Fatalf("at the end: row %d col %d", row, col)
	}
	lines, row, col = inputView([]rune("0123456789"), 7, 6) // "> 0123456789 " in rows of six
	if len(lines) < 3 || row != 1 || col != 3 {
		t.Fatalf("wrapped: %q row %d col %d", lines, row, col)
	}
	if lines, row, col = inputView(nil, 0, 10); len(lines) != 1 || row != 0 || col != 2 {
		t.Fatalf("empty line: %q %d %d", lines, row, col)
	}
}

func TestDebugPanel_UpAndDownMoveTheListUntilTextIsTypedThenTheHistory(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	fx.model.focus = focusDebug
	if fx.model.dbg.pane != paneList {
		t.Fatal("the keys go to the list when the panel opens")
	}
	fx.model = keyType(fx.model, tea.KeyDown)
	fx.model = keyType(fx.model, tea.KeyDown)
	if fx.model.dbg.varSel != 2 {
		t.Fatalf("down moves through the variables: %d", fx.model.dbg.varSel)
	}
	fx.model = keyType(fx.model, tea.KeyUp)
	if fx.model.dbg.varSel != 1 {
		t.Fatalf("up moves back: %d", fx.model.dbg.varSel)
	}
	fx.model = keyType(fx.model, tea.KeyRight)
	if fx.model.dbg.view != viewStack {
		t.Fatal("right shows the call stack")
	}
	fx.model = keyType(fx.model, tea.KeyDown) // in the call stack: the next frame
	if fx.model.dbg.frame != 1 {
		t.Fatalf("down chooses the next frame: %d", fx.model.dbg.frame)
	}
	fx.model = keyType(fx.model, tea.KeyUp)
	if fx.model.dbg.frame != 0 {
		t.Fatalf("up chooses the previous frame: %d", fx.model.dbg.frame)
	}
	fx.model = typeInPanel(fx.model, "x") // text goes to the console, which now has the keys
	if fx.model.dbg.pane != paneConsole || string(fx.model.dbg.input) != "x" {
		t.Fatalf("typing switches to the console: %d %q", fx.model.dbg.pane, string(fx.model.dbg.input))
	}
	frame := fx.model.dbg.frame
	fx.model = keyType(fx.model, tea.KeyUp) // history, not the list
	if fx.model.dbg.frame != frame {
		t.Fatal("up in the console must not move the list")
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown, Alt: true}) // alt+down still moves the list
	if fx.model.dbg.frame != frame+1 {
		t.Fatalf("alt+down moves the list from the console: %d", fx.model.dbg.frame)
	}
	fx.model.dbg.clearInput()
	fx.model = keyType(fx.model, tea.KeyTab)
	if fx.model.dbg.pane != paneList {
		t.Fatal("tab on an empty line goes back to the list")
	}
	fx.model = keyType(fx.model, tea.KeyTab)
	if fx.model.dbg.pane != paneConsole {
		t.Fatal("tab in the list goes to the console")
	}
}

func TestDebugPanel_AClickChoosesWhichPartHasTheKeys(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	top := fx.model.editorHeight() + topBarHeight
	click := func(m ProjectModel, column, row int) ProjectModel {
		return update(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: column + m.editorOffset(), Y: top + row})
	}
	width := fx.model.editorWidth()
	fx.model = click(fx.model, width-3, 2) // in the console
	if fx.model.dbg.pane != paneConsole || fx.model.focus != focusDebug {
		t.Fatalf("a click in the console gives it the keys: %d", fx.model.dbg.pane)
	}
	fx.model = click(fx.model, 3, 2) // on a row of the list
	if fx.model.dbg.pane != paneList {
		t.Fatal("a click on the list gives it the keys")
	}
}

func TestDebugPanel_EditingKeysInTheListGoToTheConsoleAndALongLineKeepsItsCursorInView(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model.dbg.input = []rune("abc")
	fx.model.dbg.cursor = 3
	fx.model.dbg.pane = paneList // as after a click on the list with text still in the line
	fx.model = keyType(fx.model, tea.KeyBackspace)
	if fx.model.dbg.pane != paneConsole || string(fx.model.dbg.input) != "ab" {
		t.Fatalf("backspace belongs to the console: %d %q", fx.model.dbg.pane, string(fx.model.dbg.input))
	}
	// a line of many rows with the cursor at its start: the window shows the cursor, not the end
	long := strings.Repeat("0123456789", 40)
	fx.model.dbg.input, fx.model.dbg.cursor = []rune(long), 0
	view := stripANSI(fx.model.View())
	if !strings.Contains(view, "> 0123456789") {
		t.Fatalf("the part with the cursor is shown:\n%s", view)
	}
}

func TestDebugConsole_ThePopupOfSuggestionsWorksLikeTheOneOfTheEditor(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model = typeInPanel(fx.model, "print(re")
	ctrlSpace := func() {
		updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyCtrlAt}) // ctrl+space as terminals send it
		fx.model = updated.(ProjectModel)
		if cmd != nil {
			fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugCompleteMsg); return ok }))
		}
	}
	ctrlSpace() // "request" and "requests" share "request": it is put in, the choice is left to the popup
	if string(fx.model.dbg.input) != "print(request" || fx.model.dbg.popup == nil {
		t.Fatalf("ctrl+space completes like tab: %q %+v", string(fx.model.dbg.input), fx.model.dbg.popup)
	}
	view := stripANSI(fx.model.View())
	if !strings.Contains(view, "  request") || !strings.Contains(view, "  requests") {
		t.Fatalf("the suggestions are drawn above the line:\n%s", view)
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyCtrlN})
	if fx.model.dbg.popup.sel != 1 {
		t.Fatalf("ctrl+n chooses the next: %d", fx.model.dbg.popup.sel)
	}
	fx.model = keyType(fx.model, tea.KeyUp) // up and down choose here, they are no history while the popup is open
	if fx.model.dbg.popup.sel != 0 || len(fx.model.evalHistory) != 0 {
		t.Fatalf("up chooses the previous: %d", fx.model.dbg.popup.sel)
	}
	fx.model = typeInPanel(fx.model, "s") // narrows the list to what starts with "requests"
	if popup := fx.model.dbg.popup; popup == nil || len(popup.items) != 1 || popup.items[0].Label != "requests" {
		t.Fatalf("typing narrows the list: %+v", popup)
	}
	fx.model = keyType(fx.model, tea.KeyBackspace)
	if fx.model.dbg.popup == nil || len(fx.model.dbg.popup.items) != 2 {
		t.Fatal("erasing widens it again")
	}
	fx.model = keyType(fx.model, tea.KeyDown)
	fx.model = keyType(fx.model, tea.KeyTab) // accepts the chosen suggestion
	if string(fx.model.dbg.input) != "print(requests" || fx.model.dbg.popup != nil || fx.model.dbg.cursor != 14 {
		t.Fatalf("tab accepts: %q popup %v cursor %d", string(fx.model.dbg.input), fx.model.dbg.popup, fx.model.dbg.cursor)
	}
	// enter accepts too (and evaluates nothing), left closes the list and moves the cursor
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "ab")
	completeNow(t, fx)
	fx.model = keyType(fx.model, tea.KeyEnter)
	if string(fx.model.dbg.input) != "abc" || len(fx.model.evalHistory) != 0 {
		t.Fatalf("enter accepts, it does not evaluate: %q", string(fx.model.dbg.input))
	}
	fx.model.dbg.clearInput()
	fx.model = typeInPanel(fx.model, "ab")
	completeNow(t, fx)
	fx.model = keyType(fx.model, tea.KeyLeft)
	if fx.model.dbg.popup != nil || fx.model.dbg.cursor != 1 {
		t.Fatalf("left closes the popup and moves: %v %d", fx.model.dbg.popup, fx.model.dbg.cursor)
	}
}

func TestDebugConsole_CtrlSpaceInTheListGoesToTheConsoleAndAsksAlsoOnAnEmptyLine(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.focus = focusDebug
	if fx.model.dbg.pane != paneList {
		t.Fatal("the list has the keys")
	}
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	fx.model = updated.(ProjectModel)
	if fx.model.dbg.pane != paneConsole || cmd == nil {
		t.Fatalf("ctrl+space gives the console the keys and asks: pane %d", fx.model.dbg.pane)
	}
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugCompleteMsg); return ok })
	if calls := fx.session.called(); calls[len(calls)-1] != "completions 1 0 " {
		t.Fatalf("an empty line is asked for as well: %v", calls)
	}
}

func TestDebugConsole_ThePopupClosesWhenTheListGetsTheKeysOrTheProgramMoves(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.settle(t)
	open := func() {
		fx.model.dbg.clearInput()
		fx.model.dbg.pane = paneConsole
		fx.model = typeInPanel(fx.model, "ab")
		completeNow(t, fx)
		if fx.model.dbg.popup == nil {
			t.Fatal("the popup is open")
		}
	}
	fx.model.focus = focusDebug
	open()
	top := fx.model.editorHeight() + topBarHeight
	fx.model = update(fx.model, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3 + fx.model.editorOffset(), Y: top + 2})
	if fx.model.dbg.popup != nil {
		t.Fatal("a click on the list closes the suggestions")
	}
	open()
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}, Alt: false})
	fx.model.dbg.popup = nil
	open()
	fx.event("continued", map[string]any{"threadId": 1})
	if fx.model.dbg.popup != nil {
		t.Fatal("the program went on: the suggestions are stale")
	}
	fx.stop(t)
	open()
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}) // leave the panel
	if fx.model.dbg.popup != nil || fx.model.focus != focusProjectEdit {
		t.Fatal("leaving the panel closes the suggestions")
	}
}
