package ui

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/dap"
	"github.com/eutima/jelly-dev/internal/keymap"
)

// ctrlF5Key is the escape sequence xterm, Windows Terminal and most terminals send for ctrl+f5. Bubble Tea does not know it and hands it
// over as a message of an unexported type, which is recognised by its name and its bytes.
const ctrlF5Key = "\x1b[15;5~"

// ctrlF5Msg is ctrl+f5 as the project window handles it.
type ctrlF5Msg struct{}

// asCtrlF5 turns the message Bubble Tea sends for the escape sequence of ctrl+f5 into a ctrlF5Msg.
func asCtrlF5(msg tea.Msg) (ctrlF5Msg, bool) {
	if _, ok := msg.(ctrlF5Msg); ok {
		return ctrlF5Msg{}, true
	}
	value := reflect.ValueOf(msg)
	if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 && strings.Contains(value.Type().Name(), "unknownCSISequenceMsg") {
		return ctrlF5Msg{}, string(value.Bytes()) == ctrlF5Key
	}
	return ctrlF5Msg{}, false
}

// ctrlF5 runs the command that ctrl+f5 is bound to.
func (m ProjectModel) ctrlF5() (tea.Model, tea.Cmd) {
	command, _ := keys.Lookup(keymap.ScopeIDE, "ctrl+f5")
	switch command {
	case keymap.DebugStart:
		return m.startDebug()
	case keymap.Run:
		return m.runFile()
	}
	return m, nil
}

// runFile runs the shown file in a terminal tab: the command is typed into a shell, so the output stays when the program ends and ctrl+c
// stops it. A run of the same window that is still there is closed first, which restarts it.
func (m ProjectModel) runFile() (tea.Model, tea.Cmd) {
	file := m.editor.Path()
	switch {
	case file == "":
		m.say("save the file first: a run starts the file on disk", true)
		return m, nil
	case m.editor.Modified():
		m.say("the file has unsaved changes: save it first (:w), a run starts the file on disk", true)
		return m, nil
	}
	run, err := dap.RunCommand(file, m.root)
	if err != nil {
		m.say("cannot run this file: "+err.Error(), true)
		return m, nil
	}
	if m.dbg != nil { // one panel under the editor: the run takes the place of the debug panel
		m.stopDebugNow(true)
	}
	for i, session := range m.terms { // the earlier run
		if session == m.runTerm {
			m.closeTermTab(i)
			break
		}
	}
	before := len(m.terms)
	model, cmd := m.newTerminal()
	m, _ = model.(ProjectModel)
	if len(m.terms) == before {
		return m, cmd // the shell did not start: newTerminal said why
	}
	session := m.terms[len(m.terms)-1]
	session.Rename("run " + shortName(file))
	m.runTerm = session
	session.Write([]byte(shellLine(runtime.GOOS, run) + "\r"))
	return m, cmd
}

func shortName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// shellLine is the line that starts a command in a shell: in the folder of the command, with its variables.
func shellLine(goos string, run dap.Command) string {
	names := make([]string, 0, len(run.Env))
	for name := range run.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	if goos == "windows" { // PowerShell
		quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }
		parts = append(parts, "Set-Location "+quote(run.Dir))
		for _, name := range names {
			parts = append(parts, fmt.Sprintf("$env:%s=%s", name, quote(run.Env[name])))
		}
		call := "& "
		for i, arg := range run.Argv {
			if i > 0 {
				call += " "
			}
			call += quote(arg)
		}
		return strings.Join(append(parts, call), "; ")
	}
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }
	call := "env"
	for _, name := range names {
		call += " " + quote(name+"="+run.Env[name])
	}
	for _, arg := range run.Argv {
		call += " " + quote(arg)
	}
	return "cd " + quote(run.Dir) + " && " + call
}
