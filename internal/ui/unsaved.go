package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	dialogRows = 3
	dialogBox  = 52 // width of the dialog box
)

// The buttons of the dialog about unsaved changes, in the order they are shown.
var dialogButtons = []struct{ action, label, key string }{
	{"save", "Save", "s"}, {"discard", "Discard", "d"}, {"cancel", "Cancel", "c"},
}

// unsavedDialog asks what to do with the changes of the open file before something replaces it.
type unsavedDialog struct {
	file   string
	choice int
	then   func(ProjectModel) (tea.Model, tea.Cmd) // the blocked action, run once the file is saved or discarded
}

// confirmUnsaved opens the dialog when the file has unsaved changes and returns true; otherwise it does nothing.
func (m ProjectModel) confirmUnsaved(then func(ProjectModel) (tea.Model, tea.Cmd)) (ProjectModel, bool) {
	if !m.editor.Modified() {
		return m, false
	}
	name := "[No Name]"
	if path := m.editor.Path(); path != "" {
		name = m.relative(path)
	}
	m.dlg = &unsavedDialog{file: name, then: then}
	return m, true
}

func (m ProjectModel) dialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := *m.dlg
	switch key := msg.String(); key {
	case "left", "h", "shift+tab":
		d.choice = (d.choice + len(dialogButtons) - 1) % len(dialogButtons)
		m.dlg = &d
		return m, nil
	case "right", "l", "tab":
		d.choice = (d.choice + 1) % len(dialogButtons)
		m.dlg = &d
		return m, nil
	case "enter":
		return m.dialogAction(dialogButtons[d.choice].action)
	case "esc", "ctrl+c", "q":
		return m.dialogAction("cancel")
	default:
		for _, b := range dialogButtons {
			if key == b.key || key == strings.ToUpper(b.key) {
				return m.dialogAction(b.action)
			}
		}
	}
	return m, nil
}

// dialogAction closes the dialog and carries out the choice.
func (m ProjectModel) dialogAction(action string) (tea.Model, tea.Cmd) {
	then := m.dlg.then
	m.dlg = nil
	switch action {
	case "save":
		if !m.editor.save("") {
			m.say(m.editor.status, true) // no file name or not writable: nothing is replaced
			return m, nil
		}
	case "discard":
		m.editor.vim.MarkWritten() // the changes are given up
	default:
		return m, nil
	}
	return then(m)
}

func (m ProjectModel) dialogBar() *bar {
	d := m.dlg
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for i, b := range dialogButtons {
		style := m.theme.Header
		if i == d.choice {
			style = m.theme.Selected.Bold(true)
		}
		result.click([]string{b.action}, barPart{"[" + b.key + "] " + b.label, style})
		result.add(barPart{"   ", m.theme.Footer})
	}
	return result
}

// dialogOrigin is the screen position of the dialog box's top-left corner.
func (m ProjectModel) dialogOrigin() (x, y int) {
	return max((m.width-dialogBox)/2, 0), topBarHeight + max((m.bodyHeight()-dialogRows)/2, 0)
}

func (m ProjectModel) clickDialog(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !leftPress(msg) {
		return m, nil
	}
	x, y := m.dialogOrigin()
	if msg.Y != y+1 {
		return m, nil
	}
	if keys := m.dialogBar().visibleKeysAt(msg.X-x, dialogBox); len(keys) > 0 {
		return m.dialogAction(keys[0])
	}
	return m, nil
}

// overlayDialog draws the dialog over the middle rows of a rendered screen.
func (m ProjectModel) overlayDialog(screen string) string {
	lines := strings.Split(screen, "\n")
	x, y := m.dialogOrigin()
	width := min(dialogBox, m.width)
	rows := []string{
		m.theme.Header.Render(padRight(" Unsaved changes in "+sanitize(m.dlg.file), width)),
		m.dialogBar().render(width, m.theme.Footer),
		m.theme.Footer.Faint(true).Render(padRight(" ←/→ choose, enter confirms, esc cancels", width)),
	}
	for i, row := range rows {
		if y+i < len(lines) {
			line := lines[y+i]
			lines[y+i] = ansi.Truncate(line, x, "") + padRight("", x-ansi.StringWidth(ansi.Truncate(line, x, ""))) + row + ansi.TruncateLeft(line, x+width, "")
		}
	}
	return strings.Join(lines, "\n")
}
