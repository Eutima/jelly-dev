package ui

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/dap"
)

// pumper feeds the messages of commands back into the model, like the runtime does. The commands that wait for the next event of the
// adapter stay armed between the calls of until, so it is one pumper for the whole run.
type pumper struct {
	t        *testing.T
	model    ProjectModel
	messages chan tea.Msg
}

func newPumper(t *testing.T, model ProjectModel) *pumper {
	return &pumper{t: t, model: model, messages: make(chan tea.Msg, 64)}
}

func (p *pumper) run(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				p.run(sub)
			}
			return
		}
		p.messages <- msg
	}()
}

// send gives the model a message (a key) and runs what it asks for.
func (p *pumper) send(msg tea.Msg) {
	updated, cmd := p.model.Update(msg)
	p.model = updated.(ProjectModel)
	p.run(cmd)
}

// until feeds messages to the model until done reports true or the time is up.
func (p *pumper) until(done func(ProjectModel) bool) {
	p.t.Helper()
	deadline := time.After(90 * time.Second)
	for !done(p.model) {
		select {
		case msg := <-p.messages:
			p.send(msg)
		case <-deadline:
			p.t.Fatalf("the debugger did not get there: %+v", p.model.dbg)
		}
	}
}

// TestDebug_RealDelveFromTheEditorToTheStopAndTheEnd runs the whole chain with the real delve: F9, F5, the stop at the breakpoint with its
// stack, a step, continue to the end. It is skipped where delve is not installed (or too old for this Go).
func TestDebug_RealDelveFromTheEditorToTheStopAndTheEnd(t *testing.T) {
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv is not installed")
	}
	model, pf := newProjectFixture(t, 120, map[string]string{
		"go.mod":  "module demo\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {\n\tx := 21\n\ty := x * 2\n\tprintln(\"answer\", y)\n}\n",
	})
	file := filepath.Join(pf.root, "main.go")
	model.hooks.StartDebug = func(ctx context.Context, f string, bps map[string][]int) (DebugSession, error) {
		session, _, err := dap.Debug(ctx, dap.DefaultAdapters(), exec.LookPath, f, pf.root, bps)
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	if err := model.openFile(file, true); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	model.editor = model.editor.At(4, 0) // line 5
	model = f9(model)

	p := newPumper(t, model)
	p.send(tea.KeyMsg{Type: tea.KeyF5})
	p.until(func(m ProjectModel) bool {
		return m.dbg != nil && (m.dbg.state == debugEnded || (m.dbg.state == debugStopped && len(m.dbg.frames) > 0))
	})
	model = p.model
	if model.dbg.state == debugEnded && strings.Contains(strings.Join(model.dbg.console, "\n"), "too old for this version of Delve") {
		t.Skip("this Go is older than the delve that is installed")
	}
	defer p.model.stopDebugNow(false)
	if model.dbg.state != debugStopped || !model.marks.stoppedAt(file, 5) {
		t.Fatalf("stopped at the breakpoint on line 5: %+v", model.dbg)
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "DEBUG") || !strings.Contains(view, stopMark) || len(model.dbg.frames) == 0 || model.dbg.frames[0].Name != "main.main" {
		t.Fatalf("the panel and the gutter show the stop:\n%s", view)
	}
	p.until(func(m ProjectModel) bool { return !m.dbg.varsLoading && len(m.dbg.vars) > 0 })
	if rows := p.model.dbg.flatVars(); len(rows) < 2 || !strings.Contains(rows[0].text(), "Locals") || !strings.Contains(strings.Join(texts(rows), "\n"), "x = 21") {
		t.Fatalf("the locals of main show x = 21: %v", texts(rows))
	}
	updated, evalCmd := p.model.runProjectCommand("eval x + 1")
	p.model = updated.(ProjectModel)
	p.run(evalCmd)
	p.until(func(m ProjectModel) bool { return strings.Contains(strings.Join(m.dbg.console, "\n"), "> x + 1\n  22") })
	p.send(tea.KeyMsg{Type: tea.KeyF10}) // over line 5
	p.until(func(m ProjectModel) bool { return m.marks.stoppedAt(file, 6) })
	p.send(tea.KeyMsg{Type: tea.KeyF5}) // continue: the program ends
	p.until(func(m ProjectModel) bool { return m.dbg.state == debugEnded })
	model = p.model
	if console := strings.Join(model.dbg.console, "\n"); !strings.Contains(console, "answer 42") || !strings.Contains(console, "the program has ended") {
		t.Fatalf("the output of the program and the end are in the console:\n%s", console)
	}
	if model.marks.stopPath != "" {
		t.Fatal("no stop mark after the end")
	}
}

func texts(rows []varRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.text())
	}
	return out
}

// TestDebug_RealNodeFromTheEditor runs the chain with the real js-debug (it asks for a session of its own for the program); skipped where
// js-debug-adapter or node are missing.
func TestDebug_RealNodeFromTheEditor(t *testing.T) {
	for _, command := range []string{"js-debug-adapter", "node"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skip(command + " is not installed")
		}
	}
	model, pf := newProjectFixture(t, 120, map[string]string{"main.js": "const x = 21;\nconst y = x * 2;\nconsole.log('answer', y);\n"})
	file := filepath.Join(pf.root, "main.js")
	model.hooks.StartDebug = func(ctx context.Context, f string, bps map[string][]int) (DebugSession, error) {
		session, _, err := dap.Debug(ctx, dap.DefaultAdapters(), exec.LookPath, f, pf.root, bps)
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	if err := model.openFile(file, true); err != nil {
		t.Fatal(err)
	}
	model.focus = focusProjectEdit
	model.editor = model.editor.At(1, 0) // line 2
	model = f9(model)

	p := newPumper(t, model)
	p.send(tea.KeyMsg{Type: tea.KeyF5})
	p.until(func(m ProjectModel) bool {
		return m.dbg != nil && (m.dbg.state == debugEnded || (m.dbg.state == debugStopped && len(m.dbg.frames) > 0 && !m.dbg.varsLoading && len(m.dbg.vars) > 0))
	})
	defer p.model.stopDebugNow(false)
	if p.model.dbg.state != debugStopped || !p.model.marks.stoppedAt(file, 2) {
		t.Fatalf("stopped at the breakpoint on line 2: %+v", p.model.dbg)
	}
	updated, evalCmd := p.model.runProjectCommand("eval x + 1")
	p.model = updated.(ProjectModel)
	p.run(evalCmd)
	p.until(func(m ProjectModel) bool { return strings.Contains(strings.Join(m.dbg.console, "\n"), "> x + 1\n  22") })
	p.send(tea.KeyMsg{Type: tea.KeyF5}) // continue: the program ends
	p.until(func(m ProjectModel) bool { return m.dbg.state == debugEnded })
	if console := strings.Join(p.model.dbg.console, "\n"); !strings.Contains(console, "answer 42") {
		t.Fatalf("the output of the program is in the console:\n%s", console)
	}
}
