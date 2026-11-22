package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
)

// paletteKey opens the command palette. A terminal sends ctrl+shift+p like ctrl+p (and Windows Terminal keeps it for its own
// palette), so jd uses alt+p; f1 works too.
const paletteKey = "alt+p"

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
		{"Switch project", "ctrl+p", command("project")},
		{"Open terminal", "ctrl+t", command("terminal")},
		{"Show the git diff", "alt+d", command("diff")},
		{"Resolve merge conflicts (git mergetool)", ":mergetool", command("mergetool")},
		{"Find file", "ctrl+f", command("find")},
		{"Search text in the project", "ctrl+g", command("grep")},
		{"Save file", "ctrl+s", editorCommand("w")},
		{"Save all files", ":wa", command("wa")},
		{"Format file", ":format", editorCommand("format")},
		{"Go to definition", "gd", navigate(navDefinition)},
		{"Find references", "gr", navigate(navReferences)},
		{"Next tab", "gt", command("tabnext")},
		{"Previous tab", "gT", command("tabprev")},
		{"Close tab", ":bd", command("bd")},
		{"Switch focus between tree and editor", "ctrl+e", func(m ProjectModel) (tea.Model, tea.Cmd) { return m.toggleFocus(), nil }},
		{"New terminal tab", ":terminal new", command("terminal new")},
		{"Open this project in a new window", ":window", command("window")},
		{"Refresh the file tree", "R", func(m ProjectModel) (tea.Model, tea.Cmd) {
			m.tree.Refresh()
			m.say("Tree refreshed", false)
			return m, nil
		}},
		{"Add a language (install its tools)", ":addlang", command("addlang")},
		{"Install missing tools", ":tools", command("tools")},
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
	m.palette = &commandPalette{picker: newPicker(m.theme, "Commands ("+paletteKey+")", items), actions: actions}
	return m, nil
}

// updatePalette passes a key or click to the open palette; a chosen action runs.
func (m ProjectModel) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.palette.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key := msg.String(); key == paletteKey || key == "f1" { // the key that opens the palette closes it again
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
func paletteHint() string { return fmt.Sprintf("commands (%s)", paletteKey) }
