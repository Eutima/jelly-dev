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

// dialogButton is one choice of a dialog: what it does, what it shows and the key that picks it.
type dialogButton struct{ action, label, key string }

var (
	// unsavedButtons are the choices when a file with changes is about to be replaced or closed.
	unsavedButtons = []dialogButton{{"save", "Save", "s"}, {"discard", "Discard", "d"}, {"cancel", "Cancel", "c"}}
	// closeButtons are the choices when something is about to be closed that has no changes.
	closeButtons = []dialogButton{{"yes", "Yes", "y"}, {"no", "No", "n"}}
)

// unsavedDialog asks something before the window goes on: what to do with the changes of the open file before it is
// replaced or closed (unsavedButtons), or whether to close at all (closeButtons).
type unsavedDialog struct {
	title   string
	hint    string
	buttons []dialogButton
	choice  int
	strict  bool                                    // ctrl+d never confirms (for what cannot be undone)
	then    func(ProjectModel) (tea.Model, tea.Cmd) // the blocked action, run once the question is answered with yes, save or discard
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
	m.dlg = &unsavedDialog{title: "Unsaved changes in " + name, hint: " ←/→ choose, enter confirms, esc cancels", buttons: unsavedButtons, then: then}
	return m, true
}

// confirmClose asks whether to close, even though nothing is unsaved; yes runs then. A second ctrl+d answers yes; ctrl+c cancels like esc.
func (m ProjectModel) confirmClose(title string, then func(ProjectModel) (tea.Model, tea.Cmd)) ProjectModel {
	m.dlg = &unsavedDialog{title: title, hint: " y or enter closes, n or esc stays, ctrl+d again closes", buttons: closeButtons, then: then}
	return m
}

func (m ProjectModel) dialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := *m.dlg
	switch key := msg.String(); key {
	case "left", "h", "shift+tab":
		d.choice = (d.choice + len(d.buttons) - 1) % len(d.buttons)
		m.dlg = &d
		return m, nil
	case "right", "l", "tab":
		d.choice = (d.choice + 1) % len(d.buttons)
		m.dlg = &d
		return m, nil
	case "enter":
		return m.dialogAction(d.buttons[d.choice].action)
	case "ctrl+d":
		if d.buttons[0].action == "yes" && !d.strict { // the key that asked also confirms
			return m.dialogAction("yes")
		}
		return m.dialogAction("cancel") // in the save dialog ctrl+d means: do not go on
	case "esc", "q", "ctrl+c": // ctrl+c is esc: it never confirms

		return m.dialogAction("cancel")
	default:
		for _, b := range d.buttons {
			if key == b.key || key == strings.ToUpper(b.key) {
				return m.dialogAction(b.action)
			}
		}
	}
	return m, nil
}

// dialogAction closes the dialog and carries out the choice: yes, save and discard go on with the blocked action, anything else
// (no, cancel) leaves everything as it is.
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
	case "yes":
	default:
		return m, nil
	}
	return then(m)
}

func (m ProjectModel) dialogBar() *bar {
	d := m.dlg
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for i, b := range d.buttons {
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
	width := min(dialogBox, m.width)
	return m.overlayRows(screen, []string{
		m.theme.Header.Render(padRight(" "+sanitize(m.dlg.title), width)),
		m.dialogBar().render(width, m.theme.Footer),
		m.theme.Footer.Render(padRight(m.dlg.hint, width)),
	})
}

// overlayRows draws rows of the dialog width over the middle rows of a rendered screen.
func (m ProjectModel) overlayRows(screen string, rows []string) string {
	lines := strings.Split(screen, "\n")
	x, y := m.dialogOrigin()
	width := min(dialogBox, m.width)
	for i, row := range rows {
		if y+i < len(lines) {
			line := lines[y+i]
			lines[y+i] = ansi.Truncate(line, x, "") + padRight("", x-ansi.StringWidth(ansi.Truncate(line, x, ""))) + row + ansi.TruncateLeft(line, x+width, "")
		}
	}
	return strings.Join(lines, "\n")
}
