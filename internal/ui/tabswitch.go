package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"
)

// tabTarget is what an entry of the tab switcher shows.
type tabTarget struct {
	terminal bool // a shell tab, else a file tab
	index    int  // the tab; ignored for the entry that starts a new shell
	newShell bool // the entry "New terminal"
}

// tabSwitcher is the open list of all tabs, files and shells.
type tabSwitcher struct {
	picker  PickerModel
	targets []tabTarget
}

// openTabSwitcher shows the searchable list of the file tabs, the shells and "New terminal"; the shown tab is selected.
func (m ProjectModel) openTabSwitcher() (tea.Model, tea.Cmd) {
	var items []PickItem
	var targets []tabTarget
	names := m.tabNames()
	for i, e := range m.openEditors() {
		detail := "no file yet"
		if path := e.Path(); path != "" {
			detail = sanitize(m.relative(path))
		}
		if e.Modified() {
			detail += "  [+]"
		}
		items = append(items, PickItem{Lead: fmt.Sprint(i + 1), Label: sanitize(names[i]), Detail: detail})
		targets = append(targets, tabTarget{index: i})
	}
	for i, session := range m.terms {
		items = append(items, PickItem{Lead: fmt.Sprintf("alt+%d", i+1), Label: fmt.Sprintf("Terminal %d", i+1), Detail: sanitize(session.Name())})
		targets = append(targets, tabTarget{terminal: true, index: i})
	}
	items = append(items, PickItem{Label: "New terminal", Detail: "start another shell in the project"})
	targets = append(targets, tabTarget{newShell: true})

	picker := newPicker(m.theme, "Tabs ("+keys.Label(keymap.Tabs)+")", items)
	picker.sel = m.tab // the shown file tab (or the first entry)
	if m.focus == focusTerminal && m.termShown {
		picker.sel = len(m.openEditors()) + m.termActive
	}
	m.tabs = &tabSwitcher{picker: picker, targets: targets}
	return m, nil
}

// updateTabSwitcher passes a key or click to the open switcher; the chosen tab is shown.
func (m ProjectModel) updateTabSwitcher(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.tabs.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keys.Is(keymap.Tabs, msg.String()) {
			m.tabs = nil
			return m, nil
		}
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	switch outcome {
	case pickNone:
		switcher := *m.tabs
		switcher.picker = picker
		m.tabs = &switcher
		return m, nil
	case pickCancelled:
		m.tabs = nil
		return m, nil
	}
	chosen := picker.Chosen()
	targets := m.tabs.targets
	m.tabs = nil
	if chosen < 0 || chosen >= len(targets) {
		return m, nil
	}
	return m.showTab(targets[chosen])
}

// showTab brings a tab to the front: a file in the editor, a shell in the terminal panel.
func (m ProjectModel) showTab(target tabTarget) (tea.Model, tea.Cmd) {
	switch {
	case target.newShell:
		return m.newTerminal()
	case target.terminal:
		if target.index >= len(m.terms) {
			return m, nil
		}
		m.termActive, m.termShown, m.focus = target.index, true, focusTerminal
		return m.resizeEditor(), nil
	}
	m.selectTab(target.index)
	m.focus = focusProjectEdit
	return m.resizeEditor(), nil
}
