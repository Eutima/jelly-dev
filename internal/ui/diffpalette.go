package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// diffPaletteAction is one entry of the command palette of the diff and git views: what it does, the command of the diff scope
// that does it, and whether it can be used now.
type diffPaletteAction struct {
	label   string
	command keymap.Command
	enabled bool
}

// diffPalette is the open list of the actions of a diff or git view.
type diffPalette struct {
	picker  PickerModel
	actions []diffPaletteAction
}

// newDiffPalette lists the actions that can be used now, each with the key the command has in .jdrc.
func newDiffPalette(theme Theme, all []diffPaletteAction) *diffPalette {
	palette := &diffPalette{}
	var items []PickItem
	for _, action := range all {
		if action.enabled {
			palette.actions = append(palette.actions, action)
			items = append(items, PickItem{Lead: keys.Label(action.command), Label: action.label})
		}
	}
	palette.picker = newPicker(theme, "Commands ("+keys.Label(keymap.DiffPalette)+")", items)
	return palette
}

// update passes a key or click to the palette. It returns the palette that stays open (nil when closed) and the command that was
// chosen ("" when none).
func (p diffPalette) update(msg tea.Msg, width, height int) (*diffPalette, keymap.Command) {
	var outcome pickOutcome
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keys.Is(keymap.DiffPalette, msg.String()) { // the key that opens the palette closes it again
			return nil, ""
		}
		p.picker, outcome = p.picker.handleKey(msg, height)
	case tea.MouseMsg:
		p.picker, outcome = p.picker.handleMouse(msg, width, height)
	}
	switch outcome {
	case pickNone:
		return &p, ""
	case pickCancelled:
		return nil, ""
	}
	if chosen := p.picker.Chosen(); chosen >= 0 && chosen < len(p.actions) {
		return nil, p.actions[chosen].command
	}
	return nil, ""
}

// key is a key that runs a command, "" when .jdrc left it without one.
func diffCommandKey(command keymap.Command) string { return keys.Key(command) }

// diffActions are the actions of a diff: moving between the changes and the view; revert, edit and undo when the file can be changed.
func (m DiffModel) paletteActions() []diffPaletteAction {
	return []diffPaletteAction{
		{"Next change", keymap.DiffNext, m.HunkCount() > 0},
		{"Previous change", keymap.DiffPrev, m.HunkCount() > 0},
		{"Switch between the split and the unified view", keymap.DiffSplit, true},
		{"Revert the selected change", keymap.DiffRevert, m.editable && m.current >= 0},
		{"Edit the file", keymap.DiffEdit, m.editable},
		{"Undo the last revert or edit", keymap.DiffUndo, m.editable && m.canUndo},
		{"Quit", keymap.DiffQuit, true},
	}
}

// paletteActions of the git view: the ones of the diff of the shown file, the list of files, and the way back from the diff.
func (m GitModel) paletteActions() []diffPaletteAction {
	actions := m.diff.paletteActions()
	for i := range actions {
		if m.loading || m.message != "" {
			actions[i].enabled = actions[i].enabled && actions[i].command == keymap.DiffQuit
		}
	}
	return actions
}

// runPaletteCommand does what a chosen command says: the key of the command is pressed.
func runPaletteCommand(model tea.Model, command keymap.Command) (tea.Model, tea.Cmd) {
	key := diffCommandKey(command)
	if key == "" {
		return model, nil
	}
	return replay(model, []string{key})
}
