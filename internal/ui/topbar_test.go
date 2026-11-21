package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// buttonX returns a column inside the top bar button with the given action.
func buttonX(t *testing.T, m ProjectModel, action string) int {
	t.Helper()
	for _, r := range m.topBar().regions {
		if r.keys[0] == action {
			return r.from + 1
		}
	}
	t.Fatalf("no %s button", action)
	return 0
}

func clickBar(m ProjectModel, x int) (ProjectModel, tea.Cmd) {
	updated, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 0})
	return updated.(ProjectModel), cmd
}

func TestTopBar_ShowsButtonsOnFirstLine(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if first := strings.Split(model.View(), "\n")[0]; strings.Contains(first, "Diff") {
		t.Fatalf("outside a repository there is no diff button: %q", first)
	}
	if err := os.Mkdir(filepath.Join(model.root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := strings.Split(model.View(), "\n")[0]
	for _, label := range []string{"Project", "Terminal", "Diff"} {
		if !strings.Contains(first, label) {
			t.Fatalf("top bar lacks %s: %q", label, first)
		}
	}
}

func TestTopBar_ProjectButtonOpensPicker(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = clickBar(model, buttonX(t, model, actionProject))
	if model.picker == nil {
		t.Fatal("the project button must open the project list")
	}
}

func TestTopBar_TerminalButtonRunsShellInProject(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	var dir string
	model.hooks.Terminal = func(d string) *exec.Cmd { dir = d; return exec.Command("true") }
	model, cmd := clickBar(model, buttonX(t, model, actionTerminal))
	if cmd == nil || dir != model.root {
		t.Fatalf("terminal must start in the project: cmd %v dir %q", cmd != nil, dir)
	}
}

func TestTopBar_DiffNeedsGitRepository(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	called := false
	model.hooks.Diff = func(string) *exec.Cmd { called = true; return exec.Command("true") }
	updated, cmd := model.runProjectCommand("diff")
	model = updated.(ProjectModel)
	if called || cmd != nil || !model.warn {
		t.Fatalf("outside a repository the diff must warn: called %v warn %v status %q", called, model.warn, model.status)
	}
	if err := os.Mkdir(filepath.Join(model.root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, cmd = clickBar(model, buttonX(t, model, actionDiff)); cmd == nil || !called {
		t.Fatal("in a repository the diff must start")
	}
}

func TestTopBar_ExCommandsAndFailureReported(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("true") }
	_, cmd := model.runProjectCommand("terminal")
	if cmd == nil {
		t.Fatal(":terminal must start a shell")
	}
	updated, _ := model.Update(externalDoneMsg{what: "terminal", err: os.ErrNotExist})
	if got := updated.(ProjectModel); !got.warn {
		t.Fatal("a failing program must be reported")
	}
}

func TestEditor_FooterClickInInsertModeRunsCommand(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter") // opens a file in the editor
	model, _ = projectKeys(model, "i")
	before := strings.Join(model.Editor().Lines(), "\n")
	var x int
	for _, r := range model.Editor().footerBar().regions {
		if r.keys[0] == ":" { // :w
			x = r.from + 1
		}
	}
	editorX := model.editorOffset() + x
	updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: editorX, Y: topBarHeight + model.bodyHeight() - 1})
	model = updated.(ProjectModel)
	if got := strings.Join(model.Editor().Lines(), "\n"); got != before {
		t.Fatalf("a footer click must not type into the text:\n%q\n%q", before, got)
	}
}

func TestTerminalPanel_OpensUnderEditorTakesKeysAndCloses(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "read w; echo got-$w; sleep 30") }
	model, _ = clickBar(model, buttonX(t, model, actionTerminal))
	if len(model.terms) == 0 || model.focus != focusTerminal || !strings.Contains(stripANSI(model.View()), "TERMINAL") {
		t.Fatalf("the terminal must open in a panel:\n%s", model.View())
	}
	t.Cleanup(model.closeTerminal)
	if lines := strings.Split(model.View(), "\n"); len(lines) != model.height {
		t.Fatalf("the panel must fit the window: %d lines", len(lines))
	}
	model, _ = projectKeys(model, "x", "y", "enter")
	deadline := time.After(5 * time.Second)
	for !strings.Contains(stripANSI(model.View()), "got-xy") {
		select {
		case <-model.activeTerm().Updates():
		case <-deadline:
			t.Fatalf("the shell never answered:\n%s", model.View())
		}
	}
	model, _ = projectKeys(model, "ctrl+]")
	if model.focus != focusProjectEdit || model.termPanelHeight() == 0 {
		t.Fatal("ctrl+] must give the focus back and keep the panel")
	}
	model, _ = clickBar(model, buttonX(t, model, actionTerminal)) // focuses the running shell again
	if model.focus != focusTerminal {
		t.Fatal("the button must focus the shell")
	}
	model, _ = clickBar(model, buttonX(t, model, actionTerminal)) // and hides it
	if model.termShown || len(model.terms) == 0 {
		t.Fatal("a second click must hide the panel but keep the shell")
	}
}

func TestKeyBytes(t *testing.T) {
	tests := map[string]struct {
		msg  tea.KeyMsg
		want string
	}{
		"text":  {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")}, "é"},
		"enter": {tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		"ctrl":  {tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		"up":    {tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		"alt":   {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, "\x1bb"},
		"back":  {tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
	}
	for name, tc := range tests {
		if got := string(keyBytes(tc.msg)); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestShortcuts_CtrlPOpensProjectsAndCtrlTTerminal(t *testing.T) {
	for name, start := range map[string]func(ProjectModel) ProjectModel{
		"tree":   func(m ProjectModel) ProjectModel { return m },
		"editor": inEditor,
	} {
		model, _ := newProjectFixture(t, 100, sampleFiles)
		model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
		model = start(model)
		model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlP})
		if model.picker == nil {
			// the editor hands the key over as a command: run it
			updated, _ := model.runProjectCommand("project")
			model = updated.(ProjectModel)
		}
		if model.picker == nil {
			t.Fatalf("%s: ctrl+p must open the project list", name)
		}
	}
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT}) // not projectSend: it would wait for the shell to end
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	if len(model.terms) == 0 || model.focus != focusTerminal {
		t.Fatal("ctrl+t must open the terminal")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if model = updated.(ProjectModel); model.termShown || model.focus == focusTerminal {
		t.Fatal("ctrl+t in the terminal must hide it")
	}
}

func TestTerminalTabs_OpenSwitchAndClose(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	send := func(msg tea.Msg) {
		updated, _ := model.Update(msg)
		model = updated.(ProjectModel)
	}
	t.Cleanup(model.closeTerminal)
	send(tea.KeyMsg{Type: tea.KeyCtrlT})
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t"), Alt: true})
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t"), Alt: true})
	if len(model.terms) != 3 || model.termActive != 2 {
		t.Fatalf("alt+t must open tabs: %d tabs, active %d", len(model.terms), model.termActive)
	}
	header := stripANSI(strings.Split(model.View(), "\n")[1+model.editorHeight()])
	for _, want := range []string{"1:sh", "2:sh", "3:sh", "[+]"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header lacks %q: %q", want, header)
		}
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1"), Alt: true})
	if model.termActive != 0 {
		t.Fatalf("alt+1 must show the first tab: %d", model.termActive)
	}
	x := 0 // a click on the header's tab 2
	for _, r := range model.termHeader().regions {
		if r.keys[0] == actionSelectTab+"1" {
			x = model.editorOffset() + r.from + 1
		}
	}
	send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: topBarHeight + model.editorHeight()})
	if model.termActive != 1 {
		t.Fatalf("a click must choose the tab: %d", model.termActive)
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	if len(model.terms) != 2 || model.termActive != 1 {
		t.Fatalf("alt+w must close the shown tab: %d tabs, active %d", len(model.terms), model.termActive)
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	if len(model.terms) != 0 || model.termPanelHeight() != 0 || model.focus == focusTerminal {
		t.Fatalf("closing the last tab must remove the panel: %d tabs", len(model.terms))
	}
}
