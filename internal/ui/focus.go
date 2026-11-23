package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The focus of the project window is shown in four ways that do not depend on colours (some terminals draw the focused and the other
// pane alike): a ">" in front of the title of the focused pane, its name in the footer of the editor, the half block on the divider that
// points to it, and the text of the other panes drawn faint. The cursor of an editor without the focus is not drawn.

// focusMark starts the title of a pane: ">" when the pane has the focus. Always two columns wide: nothing moves when the focus does.
func focusMark(focused bool) string {
	if focused {
		return "> "
	}
	return "  "
}

// paneName is what the footer of the editor says about the focus.
func paneName(focus int) string {
	switch focus {
	case focusTree:
		return "tree"
	case focusTerminal:
		return "terminal"
	case focusDebug:
		return "debug"
	}
	return "editor"
}

// dividerLook is the glyph and style of the divider between the tree and the editor: a left half block when the tree has the focus, a
// right half block when the editor has it (both in the accent style), the plain line when the focus is in the terminal panel.
func (m ProjectModel) dividerLook() (string, lipgloss.Style) {
	switch m.focus {
	case focusTree:
		return "\u258c", m.theme.Selected.Bold(true)
	case focusProjectEdit:
		return "\u2590", m.theme.Selected.Bold(true)
	}
	return separator, m.theme.Gutter
}

// dim draws a rendered line faint. Every reset inside the line ends the faint too, so it is switched on again after each one.
func dim(line string) string {
	if line == "" {
		return line
	}
	faint := "\x1b[2m"
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+faint)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+faint)
	return faint + line + "\x1b[0m"
}

// dimLines draws the lines faint, in place.
func dimLines(lines []string) {
	for i := range lines {
		lines[i] = dim(lines[i])
	}
}
