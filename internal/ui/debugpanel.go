package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/mattn/go-runewidth"
)

const (
	actionDebugStop = "debugstop"
	actionDebugView = "debugview:"
	minStackWidth   = 24 // the call stack column, left of the console
)

// debugPanelHeight is the height of the debug panel including its header; 0 when no run is shown or it does not fit.
func (m ProjectModel) debugPanelHeight() int {
	if m.dbg == nil {
		return 0
	}
	return m.panelSlot()
}

// debugHeader is the title line of the panel: the file and state of the run, the buttons and the keys.
func (m ProjectModel) debugHeader() *bar {
	result := &bar{}
	style := m.theme.Header
	if m.focus == focusDebug {
		style = m.theme.Selected.Bold(true)
	}
	run := m.dbg
	result.add(barPart{focusMark(m.focus == focusDebug) + "DEBUG ", style})
	result.add(barPart{fmt.Sprintf(" %s  %s ", filepath.Base(run.file), run.state), m.theme.Footer})
	for _, tab := range []struct {
		view  debugView
		label string
	}{{viewVariables, " Variables "}, {viewStack, " Call stack "}} {
		look := m.theme.Footer
		if run.view == tab.view {
			look = m.theme.Header
		}
		result.click([]string{actionDebugView + strconv.Itoa(int(tab.view))}, barPart{tab.label, look})
	}
	result.add(barPart{" ", m.theme.Footer})
	result.click([]string{actionDebugStop}, barPart{"[×]", m.theme.Footer})
	var hints []string
	for _, h := range []struct {
		command keymap.Command
		what    string
	}{{keymap.DebugStart, "continue"}, {keymap.StepOver, "over"}, {keymap.StepIn, "in"}, {keymap.StepOut, "out"}, {keymap.DebugStop, "stop"}} {
		if label := panelLabel(h.command); label != "" {
			hints = append(hints, label+" "+h.what)
		}
	}
	result.hint(barPart{"  " + strings.Join(hints, "  "), m.theme.Footer})
	return result
}

// leftRows are the rows of the list on the left of the panel (the call stack or the variables, as chosen), the selected row and a note for an
// empty list.
func (m ProjectModel) leftRows() (rows []string, selected int, top *int, note string) {
	run := m.dbg
	if run.view == viewVariables {
		flat := run.flatVars()
		for _, row := range flat {
			rows = append(rows, row.text())
		}
		note = "  no variables"
		switch {
		case run.state == debugRunning:
			note = "  running"
		case run.state == debugStopped && run.varsLoading:
			note = "  reading the variables ..."
		}
		return rows, run.varSel, &run.varTop, note
	}
	for _, frame := range run.frames {
		text := frame.Name
		if frame.Path != "" {
			text += "  " + filepath.Base(frame.Path) + ":" + fmt.Sprint(frame.Line)
		}
		rows = append(rows, sanitize(text))
	}
	note = "  no call stack"
	switch run.state {
	case debugRunning:
		note = "  running"
	case debugStopped:
		note = "  reading the stack ..."
	}
	return rows, run.frame, &run.top, note
}

// debugRows draws the body of the panel: the call stack or the variables on the left, the console with the line for expressions on the right.
// Long lines wrap. It also returns, for every line of the left column, the row it belongs to (a click on it selects the row), -1 for none.
func (m ProjectModel) debugRows(width, body int) (lines []string, rowOf []int) {
	run := m.dbg
	leftWidth := max(min(width/3, 48), min(minStackWidth, width/2))
	consoleWidth := max(width-leftWidth-1, 1)

	rows, selected, top, note := m.leftRows()
	rowLines := func(i int) []string { return splitRowLines(rows[i], max(leftWidth-2, 1)) }
	*top = scrollToShow(selected, *top, body, len(rows), func(i int) int { return len(rowLines(i)) })
	left, rowOf := drawRows(*top, len(rows), body, rowLines)
	first := map[int]bool{}
	for y := range left {
		prefix := "  "
		if !first[rowOf[y]] {
			first[rowOf[y]] = true
			if rowOf[y] == selected {
				prefix = "> "
			}
		}
		left[y] = prefix + left[y]
	}
	if len(rows) == 0 {
		left = []string{note}
		rowOf = []int{-1}
	}

	// the console: the tail of the output that fits above the line for expressions, wrapped
	inputLines := splitRowLines("> "+string(run.input)+"\u258f", consoleWidth)
	if len(inputLines) > body-1 && body > 1 {
		inputLines = inputLines[len(inputLines)-(body-1):]
	}
	room := max(body-len(inputLines), 0)
	var console []string
	for i := len(run.console) - 1; i >= 0 && len(console) < room; i-- {
		console = append(splitRowLines(run.console[i], consoleWidth), console...)
	}
	if len(console) > room {
		console = console[len(console)-room:]
	}
	for len(console) < room {
		console = append(console, "")
	}
	console = append(console, inputLines...)
	for y := 0; y < body; y++ {
		leftText, right := "", ""
		if y < len(left) {
			leftText = left[y]
		}
		if y < len(console) {
			right = console[y]
		}
		style := m.theme.Equal
		if y < len(left) && y < len(rowOf) && rowOf[y] == selected && len(rows) > 0 {
			style = m.theme.Selected
		}
		lines = append(lines, style.Render(padWide(leftText, leftWidth))+m.theme.Gutter.Render("│")+m.theme.Equal.Render(padWide(right, consoleWidth)))
	}
	for len(rowOf) < body {
		rowOf = append(rowOf, -1)
	}
	return lines, rowOf[:body]
}

// padWide pads text with blanks to width columns (text is already cut to fit; a wider rune stays whole).
func padWide(text string, width int) string {
	if gap := width - runewidth.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// debugColumn is the editor with the debug panel under it.
func (m ProjectModel) debugColumn() string {
	width := max(m.editorWidth(), 1)
	panel := m.debugPanelHeight()
	body, _ := m.debugRows(width, panel-1)
	lines := append([]string{m.debugHeader().render(width, m.theme.Footer)}, body...)
	if m.focus != focusDebug {
		dimLines(lines[1:])
	}
	return m.withTabBar(m.dimEditor(m.editor.View())) + "\n" + strings.Join(lines, "\n")
}

// debugKey handles a key while the panel has the focus: up and down choose a row of the list on the left, enter shows the frame or opens
// the variable, tab switches between the call stack and the variables, and text goes to the line for expressions (enter evaluates it).
func (m ProjectModel) debugKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	run := m.dbg
	key := msg.String()
	switch {
	case keys.Is(keymap.LeaveTerminal, key):
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case key == "esc":
		if len(run.input) > 0 {
			run.input = nil
			return m, nil
		}
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case key == "tab":
		run.view = 1 - run.view
		return m, nil
	case key == "up":
		return m.moveList(-1)
	case key == "down":
		return m.moveList(1)
	case key == "right" && run.view == viewVariables:
		return m.openVariable(true)
	case key == "left" && run.view == viewVariables:
		return m.openVariable(false)
	case key == "backspace":
		if len(run.input) > 0 {
			run.input = run.input[:len(run.input)-1]
		}
		return m, nil
	case key == "enter":
		if expression := strings.TrimSpace(string(run.input)); expression != "" {
			next, cmd := m.evaluate(expression)
			if cmd != nil { // refused (the program runs): the typed text stays
				run.input = nil
			}
			return next, cmd
		}
		if run.view == viewVariables {
			return m.toggleVariable()
		}
		return m.showFrame(run.frame)
	case (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) && !msg.Alt:
		run.input = append(run.input, []rune(sanitizeEditable(string(msg.Runes)))...)
	}
	return m, nil
}

// moveList moves the selection of the list on the left; in the call stack it shows the frame.
func (m ProjectModel) moveList(step int) (tea.Model, tea.Cmd) {
	run := m.dbg
	if run.view == viewVariables {
		run.varSel = max(min(run.varSel+step, len(run.flatVars())-1), 0)
		return m, nil
	}
	return m.showFrame(max(min(run.frame+step, len(run.frames)-1), 0))
}

// clickDebug handles a click in the panel: [×] stops the run, a tab switches the list, a row of the list is selected (a frame is shown, a
// variable opens or closes), anywhere else gives the panel the focus.
func (m ProjectModel) clickDebug(msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if !leftPress(msg) {
		return m, nil
	}
	m.focus = focusDebug
	if y == 0 {
		keys := m.debugHeader().visibleKeysAt(x, m.editorWidth())
		switch {
		case len(keys) > 0 && keys[0] == actionDebugStop:
			return m.stopDebug()
		case len(keys) > 0 && strings.HasPrefix(keys[0], actionDebugView):
			if view, err := strconv.Atoi(strings.TrimPrefix(keys[0], actionDebugView)); err == nil {
				m.dbg.view = debugView(view)
			}
		}
		return m, nil
	}
	_, rowOf := m.debugRows(max(m.editorWidth(), 1), m.debugPanelHeight()-1)
	if y-1 >= len(rowOf) || rowOf[y-1] < 0 {
		return m, nil
	}
	if m.dbg.view == viewVariables {
		m.dbg.varSel = rowOf[y-1]
		return m.toggleVariable()
	}
	return m.showFrame(rowOf[y-1])
}
