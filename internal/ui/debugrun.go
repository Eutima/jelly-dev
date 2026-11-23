package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/dap"
)

// DebugSession is a running debug session; *dap.Session is one.
type DebugSession interface {
	Events() <-chan dap.Event
	Continue(ctx context.Context) error
	Next(ctx context.Context) error
	StepIn(ctx context.Context) error
	StepOut(ctx context.Context) error
	Stack(ctx context.Context) ([]dap.Frame, error)
	SetBreakpoints(ctx context.Context, path string, lines []int) ([]dap.Breakpoint, error)
	Stop(ctx context.Context) error
}

const (
	debugStartTimeout   = 2 * time.Minute  // building the program and starting the adapter
	debugRequestTimeout = 15 * time.Second // one step, one stack
	consoleCap          = 2000             // lines of program output that are kept
)

type debugState int

const (
	debugStarting debugState = iota // the adapter is started and the program built
	debugRunning
	debugStopped // waiting at a breakpoint or after a step
	debugEnded   // the program has finished or the adapter is gone
)

func (s debugState) String() string {
	return [...]string{"starting", "running", "stopped", "ended"}[s]
}

// debugRun is one debugging run of the project window: its session and what the panel shows.
type debugRun struct {
	session DebugSession // nil while the adapter starts
	state   debugState
	file    string   // the file the run was started for
	console []string // the lines of program output and messages
	partial bool     // the last console line is not finished (output without a line break)
	frames  []dap.Frame
	frame   int // the selected frame
	top     int // first frame row shown

	cancel   func()           // ends a start that is still building the program
	launched map[string][]int // the breakpoints the adapter was started with: later changes are sent when it is up
}

// say adds a message line to the console.
func (r *debugRun) say(text string) {
	r.partial = false // a message starts a line of its own, also behind output that has no line break yet
	r.write(text + "\n")
}

// write appends program output to the console: a text without a line break continues the last line.
func (r *debugRun) write(text string) {
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		finished := strings.HasSuffix(line, "\n")
		line = sanitize(strings.TrimRight(line, "\r\n"))
		if r.partial && len(r.console) > 0 {
			r.console[len(r.console)-1] += line
		} else {
			r.console = append(r.console, line)
		}
		r.partial = !finished
	}
	if extra := len(r.console) - consoleCap; extra > 0 {
		r.console = r.console[extra:]
	}
}

// Messages of the run. They carry the run they belong to: the answer of an older run is dropped.
type (
	debugStartedMsg struct {
		run     *debugRun
		session DebugSession
		err     error
	}
	debugEventMsg struct {
		run   *debugRun
		event dap.Event
		ok    bool // false: the adapter has stopped
	}
	debugStackMsg struct {
		run    *debugRun
		frames []dap.Frame
		err    error
	}
	debugDoneMsg struct { // the end of a step or of a request without an answer worth showing
		run  *debugRun
		what string
		err  error
	}
)

// startDebug starts the adapter for the shown file and runs the program with the breakpoints of the project.
func (m ProjectModel) startDebug() (tea.Model, tea.Cmd) {
	file := m.editor.Path()
	switch {
	case m.hooks.StartDebug == nil:
		m.say("the debugger is not available in this window", true)
		return m, nil
	case file == "":
		m.say("save the file first: the debugger runs files from disk", true)
		return m, nil
	case m.editor.Modified():
		m.say("the file has unsaved changes: save it first (:w), the debugger runs the file on disk", true)
		return m, nil
	}
	if m.dbg != nil && m.dbg.session != nil {
		m.stopDebugNow(true) // a run that is still there (ended, or one that is being replaced)
	}
	run := &debugRun{state: debugStarting, file: file}
	run.say("starting " + filepath.Base(file) + " ...")
	m.dbg, m.focus = run, focusProjectEdit
	breakpoints, start := m.marks.all(), m.hooks.StartDebug
	run.launched = breakpoints
	ctx, cancel := context.WithTimeout(context.Background(), debugStartTimeout)
	run.cancel = cancel
	cmd := func() tea.Msg {
		defer cancel()
		session, err := start(ctx, file, breakpoints)
		return debugStartedMsg{run: run, session: session, err: err}
	}
	return m.resizeEditor(), cmd
}

// waitForDebug waits for the next event of the adapter.
func waitForDebug(run *debugRun) tea.Cmd {
	session := run.session
	return func() tea.Msg {
		event, ok := <-session.Events()
		return debugEventMsg{run: run, event: event, ok: ok}
	}
}

func (m ProjectModel) debugStarted(msg debugStartedMsg) (tea.Model, tea.Cmd) {
	if msg.run != m.dbg {
		if msg.session != nil { // the run was closed while the adapter started
			go func() { _ = msg.session.Stop(context.Background()) }()
		}
		return m, nil
	}
	if msg.err != nil {
		m.dbg.state = debugEnded
		m.dbg.say("cannot start the debugger: " + msg.err.Error())
		return m, nil
	}
	m.dbg.session, m.dbg.state = msg.session, debugRunning
	cmds := []tea.Cmd{waitForDebug(m.dbg)}
	current := m.marks.all()
	for path := range m.dbg.launched { // breakpoints that changed while the adapter was starting
		if _, there := current[path]; !there {
			current[path] = nil
		}
	}
	for path, lines := range current {
		if !slices.Equal(lines, m.dbg.launched[path]) {
			cmds = append(cmds, m.updateBreakpointsOfRun(path))
		}
	}
	return m, tea.Batch(cmds...)
}

// debugEvent applies an event of the adapter.
func (m ProjectModel) debugEvent(msg debugEventMsg) (tea.Model, tea.Cmd) {
	run := msg.run
	if run != m.dbg {
		return m, nil
	}
	if !msg.ok {
		if run.state != debugEnded {
			run.state = debugEnded
			run.say("the debugger has stopped")
		}
		run.frames = nil
		m.marks.clearStop()
		return m, nil
	}
	next := waitForDebug(run)
	switch msg.event.Name {
	case "output":
		var body struct {
			Category string `json:"category"`
			Output   string `json:"output"`
		}
		if json.Unmarshal(msg.event.Body, &body) == nil && body.Category != "telemetry" {
			run.write(body.Output)
		}
	case "stopped":
		run.state = debugStopped
		return m, tea.Batch(next, m.debugStack(run))
	case "continued":
		run.state = debugRunning
		run.frames = nil
		m.marks.clearStop()
	case "terminated", "exited":
		if run.state != debugEnded {
			run.state = debugEnded
			run.say("the program has ended")
		}
		m.marks.clearStop()
		run.frames = nil
	}
	return m, next
}

// debugStack asks for the call stack after a stop.
func (m ProjectModel) debugStack(run *debugRun) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		frames, err := run.session.Stack(ctx)
		return debugStackMsg{run: run, frames: frames, err: err}
	}
}

func (m ProjectModel) debugStackReady(msg debugStackMsg) (tea.Model, tea.Cmd) {
	run := msg.run
	if run != m.dbg || run.state != debugStopped {
		return m, nil // it went on before the stack came
	}
	if msg.err != nil {
		run.say("cannot read the call stack: " + msg.err.Error())
		return m, nil
	}
	run.frames, run.frame, run.top = msg.frames, 0, 0
	return m.showFrame(0), nil
}

// showFrame marks the line of a frame in its file and shows it, without taking the focus from where it is.
func (m ProjectModel) showFrame(index int) ProjectModel {
	run := m.dbg
	if run == nil || index < 0 || index >= len(run.frames) {
		return m
	}
	run.frame = index
	frame := run.frames[index]
	if frame.Path == "" || frame.Line < 1 {
		m.marks.clearStop()
		return m
	}
	m.marks.setStop(frame.Path, frame.Line)
	focus := m.focus
	if err := m.openFile(frame.Path, false); err != nil {
		m.say(err.Error(), true)
		return m
	}
	m.editor = m.editor.At(frame.Line-1, 0)
	m.focus = focus
	return m.resizeEditor()
}

// debugStep runs one control of the stopped program (continue, step over, in, out).
func (m ProjectModel) debugStep(what string) (tea.Model, tea.Cmd) {
	run := m.dbg
	switch {
	case run == nil || run.session == nil:
		m.say("no program is being debugged: start it with the debug key", true)
		return m, nil
	case run.state != debugStopped:
		m.say("the program is not stopped ("+run.state.String()+")", true)
		return m, nil
	}
	session := run.session
	var action func(context.Context) error
	switch what {
	case "continue":
		action = session.Continue
	case "step over":
		action = session.Next
	case "step in":
		action = session.StepIn
	default:
		action = session.StepOut
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		return debugDoneMsg{run: run, what: what, err: action(ctx)}
	}
}

func (m ProjectModel) debugDone(msg debugDoneMsg) (tea.Model, tea.Cmd) {
	if msg.run == m.dbg && msg.err != nil {
		msg.run.say(msg.what + " failed: " + msg.err.Error())
	}
	return m, nil
}

// stopDebug ends the run and closes the panel.
func (m ProjectModel) stopDebug() (tea.Model, tea.Cmd) {
	if m.dbg == nil {
		m.say("no program is being debugged", true)
		return m, nil
	}
	m.stopDebugNow(true)
	return m.resizeEditor(), nil
}

// stopDebugNow ends the adapter of the run (and the program it started) and removes the panel. The adapter gets a short time to end
// the program itself; then it is killed. Without background the call returns when that is done (the window is about to close).
func (m *ProjectModel) stopDebugNow(background bool) {
	run := m.dbg
	m.dbg = nil
	m.marks.clearStop()
	if run != nil && run.cancel != nil {
		run.cancel() // a start that is still running gives up
	}
	if m.focus == focusDebug {
		m.focus = focusProjectEdit
	}
	if run == nil || run.session == nil {
		return
	}
	if background {
		go func() { _ = run.session.Stop(context.Background()) }()
		return
	}
	_ = run.session.Stop(context.Background())
}

// updateBreakpointsOfRun tells a running adapter about the breakpoints of a file that just changed.
func (m ProjectModel) updateBreakpointsOfRun(path string) tea.Cmd {
	run := m.dbg
	if run == nil || run.session == nil || run.state == debugEnded {
		return nil
	}
	lines := m.marks.all()[path]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		_, err := run.session.SetBreakpoints(ctx, path, lines)
		return debugDoneMsg{run: run, what: fmt.Sprintf("setting the breakpoints of %s", filepath.Base(path)), err: err}
	}
}
