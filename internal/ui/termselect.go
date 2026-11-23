package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// terminalWheelLines is how many lines one turn of the wheel scrolls the terminal panel.
const terminalWheelLines = 3

// termSelection is the text that is selected with the mouse in the terminal panel (cells of the screen, row and column).
type termSelection struct {
	anchorX, anchorY int
	curX, curY       int
	dragging         bool // the button is still down
	moved            bool
}

// bounds returns the selection in reading order, both ends included.
func (s termSelection) bounds() (x0, y0, x1, y1 int) {
	x0, y0, x1, y1 = s.anchorX, s.anchorY, s.curX, s.curY
	if y1 < y0 || (y1 == y0 && x1 < x0) {
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	return
}

// covers reports whether a cell is selected.
func (s termSelection) covers(x, y int) bool {
	if !s.moved {
		return false
	}
	x0, y0, x1, y1 := s.bounds()
	switch {
	case y < y0 || y > y1:
		return false
	case y == y0 && x < x0:
		return false
	case y == y1 && x > x1:
		return false
	}
	return true
}

// text is the selected text: lines end where the selection leaves them, trailing spaces of a line are dropped.
func (s termSelection) text(cells [][]string) string {
	if !s.moved {
		return ""
	}
	_, y0, _, y1 := s.bounds()
	var lines []string
	for y := max(y0, 0); y <= y1 && y < len(cells); y++ {
		var line strings.Builder
		for x, cell := range cells[y] {
			if s.covers(x, y) {
				line.WriteString(cell)
			}
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// mouseInTerminal handles the mouse while the terminal panel shows a shell: pressing in the screen and dragging selects text,
// letting go copies it to the clipboard. It reports whether the event was used.
func (m ProjectModel) mouseInTerminal(msg tea.MouseMsg, x, y int) (ProjectModel, bool) {
	session := m.activeTerm()
	if session == nil {
		return m, false
	}
	cells := session.Cells()
	rows := len(cells)
	if rows == 0 {
		return m, false
	}
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		direction := 1 // wheel up: back into the history
		if msg.Button == tea.MouseButtonWheelDown {
			direction = -1
		}
		if session.AltScreen() { // vim, less: no history, the program scrolls (the arrows are what a terminal sends for the wheel there, in the form the program asked for)
			session.Arrows(direction > 0, terminalWheelLines)
		} else {
			session.ScrollBy(direction * terminalWheelLines)
		}
		m.termSel = nil // the selection was of the rows that were on screen
		return m, true
	}
	column := func(v int) int { return min(max(v, 0), max(len(cells[0])-1, 0)) }
	row := func(v int) int { return min(max(v-1, 0), rows-1) } // the panel's first row is its header
	switch {
	case leftPress(msg) && y >= 1:
		m.focus = focusTerminal
		m.termSel = &termSelection{anchorX: column(x), anchorY: row(y), curX: column(x), curY: row(y), dragging: true}
		return m, true
	case m.termSel != nil && m.termSel.dragging && msg.Action == tea.MouseActionMotion:
		sel := *m.termSel
		sel.curX, sel.curY = column(x), row(y)
		sel.moved = sel.moved || sel.curX != sel.anchorX || sel.curY != sel.anchorY
		m.termSel = &sel
		return m, true
	case m.termSel != nil && m.termSel.dragging && msg.Action == tea.MouseActionRelease:
		sel := *m.termSel
		sel.dragging = false
		m.termSel = &sel
		if text := sel.text(cells); text != "" && m.hooks.WriteClipboard != nil {
			if err := m.hooks.WriteClipboard(text); err != nil {
				m.say("Cannot copy: "+err.Error(), true)
			} else {
				m.say("Copied "+countLines(text), false)
			}
		}
		if !sel.moved {
			m.termSel = nil
		}
		return m, true
	}
	return m, false
}

func countLines(text string) string {
	if n := strings.Count(text, "\n") + 1; n > 1 {
		return strconv.Itoa(n) + " lines"
	}
	return strconv.Itoa(len([]rune(text))) + " characters"
}
