package ui

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/dap"
	"github.com/eutima/jelly-dev/internal/project"
)

// fakeDebug is a debug session that records what it is asked.
type fakeDebug struct {
	mu      sync.Mutex
	calls   []string
	events  chan dap.Event
	frames  []dap.Frame
	stopped chan struct{}
}

func newFakeDebug(frames ...dap.Frame) *fakeDebug {
	return &fakeDebug{events: make(chan dap.Event, 16), frames: frames, stopped: make(chan struct{}, 1)}
}

func (f *fakeDebug) record(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return nil
}
func (f *fakeDebug) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}
func (f *fakeDebug) Events() <-chan dap.Event                   { return f.events }
func (f *fakeDebug) Continue(context.Context) error             { return f.record("continue") }
func (f *fakeDebug) Next(context.Context) error                 { return f.record("next") }
func (f *fakeDebug) StepIn(context.Context) error               { return f.record("stepIn") }
func (f *fakeDebug) StepOut(context.Context) error              { return f.record("stepOut") }
func (f *fakeDebug) Stack(context.Context) ([]dap.Frame, error) { return f.frames, nil }
func (f *fakeDebug) Scopes(_ context.Context, frame int) ([]dap.Scope, error) {
	_ = f.record("scopes " + strconv.Itoa(frame))
	return []dap.Scope{{Name: "Locals", Reference: 10}, {Name: "Globals", Reference: 11}}, nil
}
func (f *fakeDebug) Variables(_ context.Context, reference int) ([]dap.Variable, error) {
	_ = f.record("variables " + strconv.Itoa(reference))
	switch reference {
	case 10:
		return []dap.Variable{{Name: "x", Value: "21", Type: "int"}, {Name: "user", Value: "{...}", Type: "main.User", Reference: 20}}, nil
	case 11:
		return []dap.Variable{{Name: "version", Value: `"1.0"`, Type: "string"}}, nil
	case 20:
		return []dap.Variable{{Name: "Name", Value: `"Ann"`, Type: "string"}}, nil
	}
	return nil, nil
}
func (f *fakeDebug) Completions(_ context.Context, frame int, text string, column int) ([]dap.Completion, error) {
	_ = f.record("completions " + strconv.Itoa(frame) + " " + strconv.Itoa(column) + " " + text)
	switch {
	case strings.HasSuffix(text, "re"):
		return []dap.Completion{{Label: "request", Start: len([]rune(text)) - 2, Length: 2}, {Label: "requests", Start: len([]rune(text)) - 2, Length: 2}}, nil
	case strings.HasSuffix(text, "request"):
		return []dap.Completion{{Label: "request", Start: len([]rune(text)) - 7, Length: 7}, {Label: "requests", Start: len([]rune(text)) - 7, Length: 7}}, nil
	case strings.HasSuffix(text, "us"):
		return []dap.Completion{{Label: "user", Start: len([]rune(text)) - 2, Length: 2}}, nil
	case strings.HasSuffix(text, "qq"):
		return nil, errors.New("Unsupported command")
	case strings.HasSuffix(text, "ab"):
		return []dap.Completion{{Label: "abc", Start: -1}, {Label: "abd", Start: -1}}, nil
	}
	return nil, nil
}
func (f *fakeDebug) Evaluate(_ context.Context, frame int, expression string) (dap.Variable, error) {
	_ = f.record("evaluate " + strconv.Itoa(frame) + " " + expression)
	if expression == "boom" {
		return dap.Variable{}, errors.New("could not find symbol boom")
	}
	return dap.Variable{Value: "42", Type: "int"}, nil
}
func (f *fakeDebug) SetBreakpoints(_ context.Context, path string, lines []int) ([]dap.Breakpoint, error) {
	return nil, f.record("breakpoints " + filepath.Base(path) + " " + strings.Trim(strings.Join(strings.Fields(strings.Trim(jsonInts(lines), "[]")), ","), ","))
}
func (f *fakeDebug) Stop(context.Context) error {
	_ = f.record("stop")
	f.stopped <- struct{}{}
	return nil
}

func jsonInts(lines []int) string { data, _ := json.Marshal(lines); return string(data) }

type debugFixture struct {
	model   ProjectModel
	session *fakeDebug
	path    string
	started []string // the files the hook was asked to debug
	bps     map[string][]int
}

// debugProject opens src/main.go with a hook that returns a fake session.
func debugProject(t *testing.T) *debugFixture {
	t.Helper()
	fx := &debugFixture{}
	model, pf := newProjectFixture(t, 120, map[string]string{"src/main.go": "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"})
	fx.path = filepath.Join(pf.root, "src", "main.go")
	fx.session = newFakeDebug(dap.Frame{ID: 1, Name: "main.main", Path: fx.path, Line: 5}, dap.Frame{ID: 2, Name: "runtime.main", Path: "", Line: 0})
	model.hooks.StartDebug = func(_ context.Context, file string, bps map[string][]int) (DebugSession, error) {
		fx.started, fx.bps = append(fx.started, file), bps
		return fx.session, nil
	}
	if err := model.openFile(fx.path, true); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	fx.model = model
	return fx
}

func update(m ProjectModel, msg tea.Msg) ProjectModel {
	updated, _ := m.Update(msg)
	return updated.(ProjectModel)
}

// start presses the debug key and delivers the answer of the hook.
func (fx *debugFixture) start(t *testing.T) {
	t.Helper()
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyF5})
	fx.model = updated.(ProjectModel)
	if cmd == nil {
		t.Fatalf("F5 must start the adapter: %q", fx.model.editor.status)
	}
	fx.model = update(fx.model, answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugStartedMsg); return ok }))
}

// answerOf runs a command (and the commands of a batch, each on its own: some only wait) and returns the first message that matches.
func answerOf(t *testing.T, cmd tea.Cmd, match func(tea.Msg) bool) tea.Msg {
	t.Helper()
	found := make(chan tea.Msg, 8)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if match(msg) {
				found <- msg
			}
		}()
	}
	run(cmd)
	select {
	case msg := <-found:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("the command gave no matching message")
		return nil
	}
}

func (fx *debugFixture) event(name string, body any) {
	data, _ := json.Marshal(body)
	fx.model = update(fx.model, debugEventMsg{run: fx.model.dbg, event: dap.Event{Name: name, Body: data}, ok: true})
}

// stop reports a stop at the first frame and delivers the stack.
func (fx *debugFixture) stop(t *testing.T) {
	t.Helper()
	fx.event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1})
	fx.model = update(fx.model, fx.model.debugStack(fx.model.dbg)())
}

func status(m ProjectModel) string { return m.status + " " + m.editor.status }

func TestDebug_StartIsRefusedWithoutHookFileOrSavedText(t *testing.T) {
	fx := debugProject(t)
	hook := fx.model.hooks.StartDebug
	fx.model.hooks.StartDebug = nil
	m, cmd := fx.model.startDebug()
	if cmd != nil || m.(ProjectModel).dbg != nil || !strings.Contains(status(m.(ProjectModel)), "not available") {
		t.Fatalf("without a hook: %q", status(m.(ProjectModel)))
	}
	fx.model.hooks.StartDebug = hook
	fx.model.editor = fx.model.editor.WithMarks(fx.model.marks)
	empty, _ := newProjectFixture(t, 120, nil)
	empty.hooks.StartDebug = hook
	if m, cmd := empty.startDebug(); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "save the file first") {
		t.Fatalf("a file without a name: %q", status(m.(ProjectModel)))
	}
	updated, _ := fx.model.editor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // delete a character: unsaved
	fx.model.editor = updated.(EditorModel)
	if m, cmd := fx.model.startDebug(); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "unsaved changes") {
		t.Fatalf("unsaved text: %q", status(m.(ProjectModel)))
	}
}

func TestDebug_RunStartsWithTheBreakpointsShowsTheStopAndTheStack(t *testing.T) {
	fx := debugProject(t)
	fx.model = toggleBreakpointKey(fx.model)
	fx.start(t)
	if len(fx.started) != 1 || fx.started[0] != fx.path || len(fx.bps[fx.path]) != 1 || fx.bps[fx.path][0] != 1 {
		t.Fatalf("the hook got %v %v", fx.started, fx.bps)
	}
	if fx.model.dbg == nil || fx.model.dbg.state != debugRunning || fx.model.debugPanelHeight() == 0 {
		t.Fatalf("running with a panel: %+v", fx.model.dbg)
	}
	fx.event("output", map[string]any{"category": "stdout", "output": "hello "})
	fx.event("output", map[string]any{"category": "stdout", "output": "world\nnext\n"})
	if got := fx.model.dbg.console; len(got) < 3 || got[len(got)-2] != "hello world" || got[len(got)-1] != "next" {
		t.Fatalf("output without a line break continues the line: %q", got)
	}
	fx.stop(t)
	if fx.model.dbg.state != debugStopped || len(fx.model.dbg.frames) != 2 {
		t.Fatalf("stopped with a stack: %+v", fx.model.dbg)
	}
	if row, _ := fx.model.Editor().Cursor(); row != 4 || !fx.model.marks.stoppedAt(fx.path, 5) {
		t.Fatalf("the cursor and the mark are on the stop line (5): row %d, %s %d", row, fx.model.marks.stopPath, fx.model.marks.stopLine)
	}
	if fx.model.focus != focusProjectEdit {
		t.Fatal("showing the stop must not take the focus")
	}
	fx.model.dbg.view = viewStack
	view := stripANSI(fx.model.View())
	for _, want := range []string{"DEBUG", "main.go  stopped", "main.main  main.go:5", "runtime.main", "hello world", stopMark} {
		if !strings.Contains(view, want) {
			t.Fatalf("%q is missing:\n%s", want, view)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines != fx.model.height {
		t.Fatalf("the panel must not change the height of the window: %d lines for %d", lines, fx.model.height)
	}
}

func TestDebug_StepKeysNeedAStopAndRunTheSessionCommands(t *testing.T) {
	fx := debugProject(t)
	if m, cmd := fx.model.debugStep("step over"); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "no program") {
		t.Fatalf("no run: %q", status(m.(ProjectModel)))
	}
	fx.start(t)
	if m, cmd := fx.model.debugStep("step over"); cmd != nil || !strings.Contains(status(m.(ProjectModel)), "not stopped (running)") {
		t.Fatalf("running: %q", status(m.(ProjectModel)))
	}
	fx.stop(t)
	for key, want := range map[tea.KeyType]string{tea.KeyF8: "next", tea.KeyF7: "stepIn", tea.KeyF6: "stepOut", tea.KeyF9: "continue"} {
		_, cmd := fx.model.Update(tea.KeyMsg{Type: key})
		if cmd == nil {
			t.Fatalf("%v must run a step", key)
		}
		answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugDoneMsg); return ok })
		if calls := fx.session.called(); calls[len(calls)-1] != want {
			t.Fatalf("%v: want %s, got %v", key, want, calls)
		}
	}
	fx.event("continued", map[string]any{"threadId": 1})
	if fx.model.dbg.state != debugRunning || fx.model.marks.stopPath != "" || len(fx.model.dbg.frames) != 0 {
		t.Fatalf("continued clears the stop: %+v", fx.model.dbg)
	}
}

func TestDebug_TheProgramEndingAndStoppingTheRun(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.event("terminated", map[string]any{})
	if fx.model.dbg.state != debugEnded || fx.model.marks.stopPath != "" || !strings.Contains(strings.Join(fx.model.dbg.console, "\n"), "the program has ended") {
		t.Fatalf("ended: %+v", fx.model.dbg)
	}
	updated, _ := fx.model.Update(tea.KeyMsg{Type: tea.KeyCtrlB}) // a breakpoint after the end reaches no adapter
	fx.model = updated.(ProjectModel)
	for _, call := range fx.session.called() {
		if strings.HasPrefix(call, "breakpoints") {
			t.Fatalf("an ended run is not told about breakpoints: %v", fx.session.called())
		}
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyF17}) // shift+f5 as a terminal sends it
	select {
	case <-fx.session.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the adapter must be stopped")
	}
	if fx.model.dbg != nil || fx.model.debugPanelHeight() != 0 || strings.Contains(stripANSI(fx.model.View()), "DEBUG") {
		t.Fatal("stopping removes the panel")
	}
	if m, _ := fx.model.stopDebug(); !strings.Contains(status(m.(ProjectModel)), "no program") {
		t.Fatal("stopping twice says so")
	}
}

func TestDebug_StartFailureStaysInTheConsoleAndAStaleRunIsIgnored(t *testing.T) {
	fx := debugProject(t)
	fx.model.hooks.StartDebug = func(context.Context, string, map[string][]int) (DebugSession, error) {
		return nil, errors.New("dlv is missing")
	}
	fx.start(t)
	if fx.model.dbg.state != debugEnded || !strings.Contains(strings.Join(fx.model.dbg.console, "\n"), "cannot start the debugger: dlv is missing") {
		t.Fatalf("failure: %+v", fx.model.dbg)
	}
	if !strings.Contains(stripANSI(fx.model.View()), "dlv is missing") {
		t.Fatal("the console shows the reason")
	}
	stale := &debugRun{state: debugRunning}
	m := update(fx.model, debugEventMsg{run: stale, event: dap.Event{Name: "terminated"}, ok: true})
	if m.dbg != fx.model.dbg || m.dbg.state != debugEnded || stale.state != debugRunning {
		t.Fatal("events of another run change nothing")
	}
	late := newFakeDebug()
	m = update(fx.model, debugStartedMsg{run: stale, session: late})
	select {
	case <-late.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("a session that started for a closed run is stopped")
	}
}

func TestDebug_BreakpointChangesReachARunningAdapterAndTheEditorFollowsFrames(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	_, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if cmd == nil {
		t.Fatal("a breakpoint while the program runs goes to the adapter")
	}
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugDoneMsg); return ok })
	if calls := fx.session.called(); calls[len(calls)-1] != "breakpoints main.go 1" {
		t.Fatalf("calls: %v", calls)
	}
	fx.stop(t)
	fx.model.focus = focusDebug
	fx.model.dbg.view = viewStack
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyDown, Alt: true}) // runtime.main has no file: the mark goes away
	if fx.model.dbg.frame != 1 || fx.model.marks.stopPath != "" {
		t.Fatalf("frame %d, mark %q", fx.model.dbg.frame, fx.model.marks.stopPath)
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if fx.model.dbg.frame != 0 || !fx.model.marks.stoppedAt(fx.path, 5) {
		t.Fatal("up goes back to the first frame and marks its line")
	}
	fx.model = update(fx.model, tea.KeyMsg{Type: tea.KeyEsc})
	if fx.model.focus != focusProjectEdit {
		t.Fatal("esc leaves the panel")
	}
}

func TestDebug_LongConsoleLinesWrapAndTheTerminalKeyExplains(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	long := strings.Repeat("0123456789", 12) + "THEEND"
	fx.event("output", map[string]any{"category": "stdout", "output": long + "\n"})
	view := stripANSI(fx.model.View())
	if !strings.Contains(view, "THEEND") {
		t.Fatalf("nothing of a long line is cut off:\n%s", view)
	}
	if strings.Contains(view, "…") {
		t.Fatal("no ellipsis")
	}
	m, _ := fx.model.openTerminal()
	if !strings.Contains(status(m.(ProjectModel)), "stop debugging first") {
		t.Fatalf("the terminal key while a run is shown: %q", status(m.(ProjectModel)))
	}
}

func TestDebug_ClickingThePanelSelectsFramesAndTheCrossStops(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	fx.model.dbg.view = viewStack
	top := fx.model.editorHeight() + topBarHeight
	click := func(m ProjectModel, x, y int) ProjectModel {
		return update(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x + m.editorOffset(), Y: y})
	}
	fx.model = click(fx.model, 3, top+2) // the second frame (the first body row is the first frame)
	if fx.model.focus != focusDebug || fx.model.dbg.frame != 1 {
		t.Fatalf("focus %d frame %d", fx.model.focus, fx.model.dbg.frame)
	}
	header := stripANSI(fx.model.View())
	index := strings.Index(strings.Split(header, "\n")[top], "[×]")
	if index < 0 {
		t.Fatalf("the header has a [×]:\n%s", header)
	}
	fx.model = click(fx.model, index-fx.model.editorOffset(), top)
	if fx.model.dbg != nil {
		t.Fatal("the cross stops the run")
	}
}

func TestDebug_SessionOfAProjectWithARunStopsItWhenTheWindowQuits(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.model.hooks.SaveSession = func(string, project.Session) {}
	fx.model.quit()
	select {
	case <-fx.session.stopped:
	default:
		t.Fatal("quitting stops the adapter before the window closes")
	}
}

func TestDebug_BreakpointsChangedWhileTheAdapterStartsAreSentWhenItIsUp(t *testing.T) {
	fx := debugProject(t)
	updated, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyF5})
	fx.model = updated.(ProjectModel)
	started := answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugStartedMsg); return ok })
	fx.model = toggleBreakpointKey(fx.model) // the adapter is not up yet: the session does not know
	if len(fx.session.called()) != 0 {
		t.Fatalf("nothing can be sent yet: %v", fx.session.called())
	}
	updated, cmd = fx.model.Update(started)
	fx.model = updated.(ProjectModel)
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugDoneMsg); return ok })
	if calls := fx.session.called(); len(calls) != 1 || calls[0] != "breakpoints main.go 1" {
		t.Fatalf("the change is sent once the adapter is up: %v", calls)
	}
}

func TestDebug_AMessageStartsItsOwnLineAndStoppingCancelsAStartInProgress(t *testing.T) {
	run := &debugRun{}
	run.write("prompt: ")
	run.say("the program has ended")
	if got := run.console; len(got) != 2 || got[0] != "prompt: " || got[1] != "the program has ended" {
		t.Fatalf("console: %q", got)
	}
	fx := debugProject(t)
	cancelled := false
	fx.model.dbg = &debugRun{state: debugStarting, cancel: func() { cancelled = true }}
	fx.model.stopDebugNow(true)
	if !cancelled || fx.model.dbg != nil {
		t.Fatal("stopping cancels the start")
	}
	gone := &debugRun{state: debugStopped, frames: []dap.Frame{{Name: "x"}}}
	fx.model.dbg = gone
	fx.model = update(fx.model, debugEventMsg{run: gone, ok: false})
	if gone.state != debugEnded || len(gone.frames) != 0 {
		t.Fatal("an adapter that is gone leaves no stack behind")
	}
}

// headerColumn is the screen column of a text in the header line of the debug panel.
func headerColumn(t *testing.T, m ProjectModel, text string) int {
	t.Helper()
	line := strings.Split(stripANSI(m.View()), "\n")[m.editorHeight()+topBarHeight]
	index := strings.Index(line, text)
	if index < 0 {
		t.Fatalf("%q is not in the header:\n%s", text, line)
	}
	return utf8.RuneCountInString(line[:index])
}

func TestDebug_TheStepButtonsOfThePanelAreClickable(t *testing.T) {
	fx := debugProject(t)
	fx.model = update(fx.model, tea.WindowSizeMsg{Width: 220, Height: 14}) // wide enough for the labels of the buttons
	fx.start(t)                                                            // running: the steps are dim and do nothing
	top := fx.model.editorHeight() + topBarHeight
	click := func(m ProjectModel, column int) (ProjectModel, tea.Cmd) {
		updated, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: column, Y: top})
		return updated.(ProjectModel), cmd
	}
	if _, cmd := click(fx.model, headerColumn(t, fx.model, "[f8] over")); cmd != nil {
		t.Fatal("a step button does nothing while the program runs")
	}
	fx.stop(t)
	for text, want := range map[string]string{"[f9] continue": "continue", "[f8] over": "next", "[f7] in": "stepIn", "[f6] out": "stepOut"} {
		model, cmd := click(fx.model, headerColumn(t, fx.model, text))
		if cmd == nil || model.focus != focusDebug {
			t.Fatalf("%s: a click runs the step and gives the panel the focus", text)
		}
		answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugDoneMsg); return ok })
		if calls := fx.session.called(); calls[len(calls)-1] != want {
			t.Fatalf("%s: want %s, got %v", text, want, calls)
		}
	}
	before := len(fx.started)
	model, cmd := click(fx.model, headerColumn(t, fx.model, "[f5] restart"))
	if cmd == nil || len(model.dbg.console) == 0 {
		t.Fatal("the restart button starts the run again")
	}
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugStartedMsg); return ok })
	if len(fx.started) != before+1 {
		t.Fatalf("the adapter must be started again: %v", fx.started)
	}
	model, _ = click(fx.model, headerColumn(t, fx.model, "[alt+f5] stop"))
	if model.dbg != nil {
		t.Fatal("the stop button ends the run")
	}
}

func TestDebug_F5RestartsARunThatExistsAndF9ContinuesIt(t *testing.T) {
	fx := debugProject(t)
	fx.start(t)
	fx.stop(t)
	_, cmd := fx.model.Update(tea.KeyMsg{Type: tea.KeyF5}) // not "continue" any more
	if cmd == nil {
		t.Fatal("F5 restarts the run")
	}
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugStartedMsg); return ok })
	if len(fx.started) != 2 {
		t.Fatalf("F5 must start the adapter again: %v", fx.started)
	}
	select {
	case <-fx.session.stopped: // the first run is stopped in the background
	case <-time.After(5 * time.Second):
		t.Fatal("the old run must be stopped")
	}
	for _, call := range fx.session.called() {
		if call == "continue" {
			t.Fatalf("F5 must not continue the program: %v", fx.session.called())
		}
	}
	fx2 := debugProject(t)
	fx2.start(t)
	fx2.stop(t)
	_, cmd = fx2.model.Update(tea.KeyMsg{Type: tea.KeyF9})
	answerOf(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(debugDoneMsg); return ok })
	if calls := fx2.session.called(); calls[len(calls)-1] != "continue" {
		t.Fatalf("F9 continues: %v", calls)
	}
}
