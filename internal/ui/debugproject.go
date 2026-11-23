package ui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// toggleBreakpointAtCursor sets or removes the breakpoint of the cursor line of the shown file.
func (m ProjectModel) toggleBreakpointAtCursor() (tea.Model, tea.Cmd) {
	row, _ := m.editor.Cursor()
	return m.toggleBreakpoint(m.editor.Path(), row+1)
}

// toggleBreakpoint sets or removes the breakpoint of a line (1-based) and says which.
func (m ProjectModel) toggleBreakpoint(path string, line int) (tea.Model, tea.Cmd) {
	if path == "" {
		m.notice("a breakpoint needs a file: save it first", true)
		return m, nil
	}
	where := fmt.Sprintf("%s:%d", filepath.Base(path), line)
	if m.marks.toggle(path, line) {
		m.notice("breakpoint set at "+where, false)
	} else {
		m.notice("breakpoint removed at "+where, false)
	}
	return m, nil
}
