package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// mergePaletteAction is one entry of the command palette of the merge view: what it does, the key shown for it and the
// key that runs it.
type mergePaletteAction struct {
	label, shown, key string
	enabled           bool
}

// mergePalette is the open list of all actions of the merge view.
type mergePalette struct {
	picker  PickerModel
	actions []mergePaletteAction
}

// paletteActions are the actions the footer does not show.
func (m MergeModel) paletteActions() []mergePaletteAction {
	selected := m.current >= 0
	unresolved := m.Unresolved() > 0
	actions := []mergePaletteAction{
		{"Next change", "n", "n", true},
		{"Previous change", "p", "p", true},
		{"Take ours", "→", "right", selected},
		{"Take theirs", "←", "left", selected},
		{"Take both", "b", "b", selected},
		{"Take the base", "r", "r", selected},
		{"Ours, then the next change", "o", "o", selected},
		{"Theirs, then the next change", "t", "t", selected},
		{"Ignore the change", "x", "x", selected && m.unresolvedSelected()},
		{"Cancel the choice", "⌫", "backspace", selected && m.choiceMade()},
		{"Resolve the simple conflicts", "m", "m", true},
		{"All ours", "ctrl+o", "ctrl+o", unresolved},
		{"All theirs", "ctrl+t", "ctrl+t", unresolved},
		{"Edit the result", "e", "e", len(m.rows) > 0},
		{"Insert a line", "i", "i", len(m.rows) > 0},
		{"Delete the line", "d", "d", len(m.rows) > 0},
		{"Undo", "u", "u", len(m.history) > 0},
		{"Wrap long lines", "w", "w", true},
		{"Switch the pane", "v", "v", m.width < threePaneWidth},
	}
	for _, assistant := range m.assistants {
		actions = append(actions, mergePaletteAction{"Resolve with " + assistant.Name, assistant.Key, assistant.Key, unresolved && !m.aiBusy})
	}
	return append(actions,
		mergePaletteAction{"Save the result", "ctrl+s", "ctrl+s", true},
		mergePaletteAction{"Quit", "q", "q", true},
	)
}

// openPalette shows the searchable list of the actions that can be used now.
func (m MergeModel) openPalette() (tea.Model, tea.Cmd) {
	var actions []mergePaletteAction
	var items []PickItem
	for _, action := range m.paletteActions() {
		if action.enabled {
			actions = append(actions, action)
			items = append(items, PickItem{Lead: action.shown, Label: action.label})
		}
	}
	m.palette = &mergePalette{picker: newPicker(m.theme, "Commands ("+paletteKey+")", items), actions: actions}
	return m, nil
}

// updatePalette passes a key or click to the open palette; a chosen action runs as if its key was pressed.
func (m MergeModel) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	return m.handleKey(keyMsg(actions[chosen].key))
}
