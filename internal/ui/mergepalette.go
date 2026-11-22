package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// mergePaletteAction is one entry of the command palette of the merge view: what it does, the command that does it directly, and
// whether it can be used now. The key shown for it is the one the command has in .jdrc.
type mergePaletteAction struct {
	label   string
	command keymap.Command
	enabled bool
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
		{"Next change", keymap.MergeNext, true},
		{"Previous change", keymap.MergePrev, true},
		{"Take ours", keymap.MergeOurs, selected},
		{"Take theirs", keymap.MergeTheirs, selected},
		{"Take both", keymap.MergeBoth, selected},
		{"Take the base", keymap.MergeBase, selected},
		{"Ours, then the next change", keymap.MergePickOurs, selected},
		{"Theirs, then the next change", keymap.MergePickTheirs, selected},
		{"Ignore the change", keymap.MergeIgnore, selected && m.unresolvedSelected()},
		{"Cancel the choice", keymap.MergeCancel, selected && m.choiceMade()},
		{"Resolve the simple conflicts", keymap.MergeAuto, true},
		{"All ours", keymap.MergeAllOurs, unresolved},
		{"All theirs", keymap.MergeAllTheirs, unresolved},
		{"Edit the result", keymap.MergeEdit, len(m.rows) > 0},
		{"Insert a line", keymap.MergeInsert, len(m.rows) > 0},
		{"Delete the line", keymap.MergeDelete, len(m.rows) > 0},
		{"Undo", keymap.MergeUndo, len(m.history) > 0},
		{"Wrap long lines", keymap.MergeWrap, true},
		{"Switch the pane", keymap.MergePane, m.width < threePaneWidth},
	}
	// both assistants are always listed: one that cannot run says why when it is chosen (see runCommand)
	for _, entry := range []struct {
		name    string
		command keymap.Command
	}{{"Claude", keymap.MergeClaude}, {"Codex", keymap.MergeCodex}} {
		actions = append(actions, mergePaletteAction{"Resolve with " + entry.name, entry.command, true})
	}
	return append(actions,
		mergePaletteAction{"Save the result", keymap.MergeSave, true},
		mergePaletteAction{"Quit", keymap.MergeQuit, true},
	)
}

// openPalette shows the searchable list of the actions that can be used now.
func (m MergeModel) openPalette() (tea.Model, tea.Cmd) {
	var actions []mergePaletteAction
	var items []PickItem
	for _, action := range m.paletteActions() {
		if action.enabled {
			actions = append(actions, action)
			items = append(items, PickItem{Lead: keys.Label(action.command), Label: action.label})
		}
	}
	m.palette = &mergePalette{picker: newPicker(m.theme, "Actions ("+keys.Label(keymap.MergePalette)+")", items), actions: actions}
	return m, nil
}

// updatePalette passes a key or click to the open palette; a chosen action runs as if its key was pressed.
func (m MergeModel) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.palette.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keys.Is(keymap.MergePalette, msg.String()) { // the key that opens the palette closes it again
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
	return m.runCommand(actions[chosen].command)
}
