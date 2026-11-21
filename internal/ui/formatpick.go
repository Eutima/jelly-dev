package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

// formatPick is the open list of formatters: :format found none for the file, the user chooses one.
type formatPick struct {
	picker  PickerModel
	path    string
	text    string // the text that is to be formatted
	choices []lsp.FormatterChoice
}

// openFormatPicker shows the searchable list of formatters for the file of a FormatPickMsg.
func (m ProjectModel) openFormatPicker(msg FormatPickMsg) (tea.Model, tea.Cmd) {
	if m.hooks.Navigator == nil {
		m.say("E1: No formatter available in this editor", true)
		return m, nil
	}
	choices := m.hooks.Navigator.FormatterChoices(msg.Path)
	items := make([]PickItem, len(choices))
	for i, c := range choices {
		items[i] = PickItem{Label: c.Name, Detail: c.Detail, Dim: !c.Installed}
		if !c.Installed {
			items[i].Detail += " (not installed)"
		}
	}
	title := "Choose a formatter for " + m.relative(msg.Path)
	m.pick = &formatPick{picker: newPicker(m.theme, title, items), path: msg.Path, text: msg.Text, choices: choices}
	return m, nil
}

// updateFormatPicker passes a key or click to the open list; a chosen formatter formats the text now and is remembered for
// the file type when it worked.
func (m ProjectModel) updateFormatPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.pick.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	switch outcome {
	case pickNone:
		pick := *m.pick
		pick.picker = picker
		m.pick = &pick
		return m, nil
	case pickCancelled:
		m.pick = nil
		return m, nil
	}
	pick := *m.pick
	m.pick = nil
	choice := pick.choices[picker.Chosen()]
	if !choice.Installed {
		m.say(fmt.Sprintf("%s is not installed: install it, or run the jd installer (:tools installs some formatters)", choice.Name), true)
		return m, nil
	}
	nav := m.hooks.Navigator
	m.say("Formatting with "+choice.Name+"...", false)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		formatted, err := nav.FormatWith(ctx, pick.path, pick.text, choice)
		return formatResultMsg{original: pick.text, formatted: formatted, err: err, remember: &choice}
	}
}
