package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/config"
)

// The command palette opens with ctrl+a or f1 by default (a terminal sends ctrl+shift+p like ctrl+p, and Windows Terminal keeps it for
// its own palette); .jdrc changes that.

// paletteClick is what the top bar button of the palette sends back when it is clicked.
const paletteClick = ":palette"

// mergeToolKey is what the top bar button of the merge conflicts sends back when it is clicked.
const mergeToolKey = ":mergetool"

// paletteAction is one entry of the command palette: what it does, the key that does it directly, and how it runs.
type paletteAction struct {
	label, key string
	run        func(m ProjectModel) (tea.Model, tea.Cmd)
}

// commandPalette is the open list of all actions of the project window.
type commandPalette struct {
	picker  PickerModel
	actions []paletteAction
}

// paletteActions are the actions the palette offers. Everything the bars used to show is here, with its shortcut.
func (m ProjectModel) paletteActions() []paletteAction {
	command := func(name string) func(ProjectModel) (tea.Model, tea.Cmd) {
		return func(m ProjectModel) (tea.Model, tea.Cmd) { return m.runProjectCommand(name) }
	}
	// editor commands run in the editor of the shown tab, whatever has the focus
	editorCommand := func(name string) func(ProjectModel) (tea.Model, tea.Cmd) {
		return func(m ProjectModel) (tea.Model, tea.Cmd) {
			return m, m.editor.runEx(name)
		}
	}
	navigate := func(kind navKind) func(ProjectModel) (tea.Model, tea.Cmd) {
		return func(m ProjectModel) (tea.Model, tea.Cmd) {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.askNavigator(kind)
			return m, cmd
		}
	}
	actions := []paletteAction{
		{"Switch project", keys.Label(keymap.Projects), command("project")},
		{"Switch tab (files and terminals)", keys.Label(keymap.Tabs), command("tabs")},
		{"Show or hide the terminal", keys.Label(keymap.Terminal), command("terminal")},
		{"Show the git diff", keys.Label(keymap.Diff), command("diff")},
		{"Resolve merge conflicts (git mergetool)", ":mergetool", command("mergetool")},
		{"Find file", keys.Label(keymap.Find), command("find")},
		{"Search text in the project", keys.Label(keymap.Grep), command("grep")},
		{"Save file", keys.Label(keymap.Save), editorCommand("w")},
		{"Save all files", ":wa", command("wa")},
		{"Open the notes", ":note", command("note")},
		{"Notes: choose the project of the notes", ":note project", command("note project")},
		{"Copy the file path (relative to the project)", ":copypath", command("copypath")},
		{"Copy the absolute file path", ":copypath abs", command("copypath abs")},
		{"Refactor: rename the symbol under the cursor", ":refactor <name>", command("refactor")},
		{"Refactor: rename this file", ":refactor file <name>", command("refactor file")},
		{"Refactor: undo the last refactoring", ":refactor undo", command("refactor undo")},
		{"Set or remove a breakpoint on the cursor line", keys.Label(keymap.ToggleBreakpoint), command("break")},
		{"Debug: start the shown file, or continue", keys.Label(keymap.DebugStart), command("debug")},
		{"Debug: evaluate the selection or the name under the cursor", ":eval [expression]", command("eval")},
		{"Debug: step over", keys.Label(keymap.StepOver), command("stepover")},
		{"Debug: step into", keys.Label(keymap.StepIn), command("stepin")},
		{"Debug: step out", keys.Label(keymap.StepOut), command("stepout")},
		{"Debug: stop", keys.Label(keymap.DebugStop), command("debugstop")},
		{"Format file", ":format", editorCommand("format")},
		{"Go to definition", "gd", navigate(navDefinition)},
		{"Find references", "gr", navigate(navReferences)},
		{"Next tab", "gt", command("tabnext")},
		{"Previous tab", "gT", command("tabprev")},
		{"Close tab", ":bd", command("bd")},
		{"Close the other tabs", ":closeother", command("closeother")},
		{"Close all tabs", ":closeall", command("closeall")},
		{"Switch focus between tree and editor", keys.Label(keymap.Focus), func(m ProjectModel) (tea.Model, tea.Cmd) { return m.toggleFocus(), nil }},
		{"New terminal tab", ":terminal new", command("terminal new")},
		{"Open this project in a new window", ":window", command("window")},
		{"Refresh the file tree", "R", func(m ProjectModel) (tea.Model, tea.Cmd) {
			m.tree.Refresh()
			m.say("Tree refreshed", false)
			return m, nil
		}},
		{"Diff against a branch, tag or commit", ":diffrev", command("diffrev")},
		{"Diff between two revisions", ":diffrevs", command("diffrevs")},
		{"Diff this file against a revision", ":difffile", command("difffile")},
		{"Diff against another worktree", ":diffworktree", command("diffworktree")},
		{"Add a language (install its tools)", ":addlang", command("addlang")},
		{"Install missing tools", ":tools", command("tools")},
		{"Editor: nano style (no modes, keys shown at the bottom)", ":editor nano", command("editor nano")},
		{"Editor: vim style (modal)", ":editor vim", command("editor vim")},
		{"Quit", ":qa", command("qa")},
	}
	if m.hooks.Theme != nil {
		for _, name := range config.ThemeNames() {
			actions = append(actions, paletteAction{"Theme: " + name, ":theme " + name, command("theme " + name)})
		}
	}
	return actions
}

// openPalette shows the searchable list of all actions.
func (m ProjectModel) openPalette() (tea.Model, tea.Cmd) {
	actions := m.paletteActions()
	items := make([]PickItem, len(actions))
	for i, action := range actions {
		items[i] = PickItem{Lead: action.key, Label: action.label}
	}
	m.palette = &commandPalette{picker: newPicker(m.theme, "Actions ("+keys.Label(keymap.Palette)+")", items), actions: actions}
	return m, nil
}

// updatePalette passes a key or click to the open palette; a chosen action runs.
func (m ProjectModel) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.palette.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keys.Is(keymap.Palette, msg.String()) { // the key that opens the palette closes it again
			m.palette = nil
			return m, nil
		}
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	switch outcome {
	case pickNone:
		palette := *m.palette
		palette.picker = picker
		m.palette = &palette
		return m, nil
	case pickCancelled:
		m.palette = nil
		return m, nil
	}
	chosen := picker.Chosen()
	actions := m.palette.actions
	m.palette = nil
	if chosen < 0 || chosen >= len(actions) {
		return m, nil
	}
	m.notice("", false)
	return actions[chosen].run(m)
}

// paletteHint is the one hint the bars of the project window show: a click opens the palette.
func paletteHint() string { return fmt.Sprintf("actions (%s)", keys.Label(keymap.Palette)) }
