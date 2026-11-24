package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/mattn/go-runewidth"
)

const (
	actionDebugStop    = "debugstop"
	actionDebugStep    = "debugstep:" // followed by the name debugStep takes: continue, step over, step in, step out
	actionDebugRestart = "debugrestart"
	actionDebugView    = "debugview:"
	minStackWidth      = 24 // the call stack column, left of the console
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
	result.add(barPart{" ", m.theme.Footer})
	stopped := run.state == debugStopped // the steps need a program that waits
	for _, button := range []struct {
		command keymap.Command
		label   string
		action  string
		enabled bool
	}{
		{keymap.DebugContinue, "continue", actionDebugStep + "continue", stopped},
		{keymap.StepOver, "over", actionDebugStep + "step over", stopped},
		{keymap.StepIn, "in", actionDebugStep + "step in", stopped},
		{keymap.StepOut, "out", actionDebugStep + "step out", stopped},
		{keymap.DebugStart, "restart", actionDebugRestart, true},
		{keymap.DebugStop, "stop", actionDebugStop, true},
	} {
		key := panelLabel(button.command)
		if key == "" {
			continue
		}
		var clicks []string
		if button.enabled {
			clicks = []string{button.action}
		}
		result.hintWith(key, clicks, button.label, button.enabled, m.theme)
	}
	return result
}

// leftColumnWidth is the width of the list on the left of the panel.
func leftColumnWidth(width int) int { return max(min(width/3, 48), min(minStackWidth, width/2)) }

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
	leftWidth := leftColumnWidth(width)
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
	inputLines, cursorRow, cursorCol := inputView(run.input, run.cursor, consoleWidth)
	if len(inputLines) > body-1 && body > 1 { // a long line shows its end, or the part with the cursor when that is further up
		cut := min(len(inputLines)-(body-1), cursorRow)
		inputLines, cursorRow = inputLines[cut:cut+body-1], cursorRow-cut
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
	cursorRow += len(console) // the position in the console column
	chosen := make([]bool, len(console)+len(inputLines))
	if run.popup != nil && room > 0 { // the suggestions sit on the console rows just above the line
		popupLines, popupChosen := run.popup.popupRows(consoleWidth, min(maxCompletionRows, room))
		copy(console[room-len(popupLines):], popupLines)
		copy(chosen[room-len(popupLines):], popupChosen)
	}
	console = append(console, inputLines...)
	run.shown = console
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
		rightStyle := m.theme.Equal
		if y < len(chosen) && chosen[y] {
			rightStyle = m.theme.Selected
		}
		rightCell := rightStyle.Render(padWide(right, consoleWidth))
		if run.sel != nil && run.sel.moved {
			rightCell = m.selectedCell(right, y, consoleWidth, rightStyle)
		}
		if y == cursorRow && m.focus == focusDebug && run.pane == paneConsole { // the character under the cursor is drawn in reverse, the text stays where it is
			runes := []rune(right)
			col := min(cursorCol, len(runes))
			before, under, after := string(runes[:col]), " ", ""
			if col < len(runes) {
				under, after = string(runes[col]), string(runes[col+1:])
			}
			rightCell = m.theme.Equal.Render(before) + m.theme.CursorStyle().Render(under) + m.theme.Equal.Render(padWide(after, consoleWidth-runewidth.StringWidth(before)-runewidth.StringWidth(under)))
		}
		lines = append(lines, style.Render(padWide(leftText, leftWidth))+m.theme.Gutter.Render("│")+rightCell)
	}
	for len(rowOf) < body {
		rowOf = append(rowOf, -1)
	}
	return lines, rowOf[:body]
}

// inputView is the line for expressions as the console shows it: "> " and the text, wrapped to width columns, with a blank cell at the end for
// the cursor when it stands behind the last character. It also returns the line and the rune column (in that line) of the cursor cell.
func inputView(input []rune, cursor, width int) (lines []string, row, col int) {
	lines = splitRowLines("> "+string(input)+" ", width)
	target := 2 + cursor // rune index of the cursor cell in the whole text
	for i, line := range lines {
		n := len([]rune(line))
		if target < n || i == len(lines)-1 {
			return lines, i, min(target, max(n-1, 0))
		}
		target -= n
	}
	return lines, 0, 0
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

// debugKey handles a key while the panel has the focus. The panel has two parts and the keys go to one of them (run.pane; a click or tab
// chooses, typing text goes to the console). In the list on the left (the variables or the call stack): up/down choose a row, left/right
// switch between the variables and the call stack, enter opens a variable or shows a frame. In the console on the right the line for expressions
// is a small line editor: text, left/right/home/end, backspace/delete, up/down for the lines typed before, tab to complete (on an empty line:
// back to the list), enter to evaluate, esc to clear it (then to leave the panel). alt+up and alt+down choose a row of the list from the console.
func (m ProjectModel) debugKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	run := m.dbg
	key := msg.String()
	typing := (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) && !msg.Alt
	if run.pane == paneConsole && run.popup != nil && m.popupKey(key) { // the list of suggestions has the keys it has in the editor
		return m, nil
	}
	defer m.narrowPopup()     // typing and erasing narrow the list of suggestions
	if run.pane == paneList { // the keys of the list on the left; typing text goes to the console, which then has the keys
		switch {
		case key == "up":
			return m.moveList(-1)
		case key == "down":
			return m.moveList(1)
		case key == "left":
			run.view = viewVariables
			return m, nil
		case key == "right":
			run.view = viewStack
			return m, nil
		case key == "tab":
			run.pane = paneConsole
			return m, nil
		case keys.Is(keymap.InsertComplete, key):
			run.pane = paneConsole // completion is asked for in the console
		case typing, key == "home", key == "end", key == "backspace", key == "delete", key == "ctrl+u":
			run.pane = paneConsole // the keys of the line editor belong to the console
		}
	}
	switch {
	case keys.Is(keymap.LeaveTerminal, key):
		run.popup = nil
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case key == "esc":
		if len(run.input) > 0 {
			run.clearInput()
			return m, nil
		}
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case keys.Is(keymap.InsertComplete, key): // ctrl+space: the same completion as the editor's, also on an empty line
		return m.complete()
	case key == "tab": // in the console: complete; on an empty line go back to the list
		if len(run.input) > 0 {
			return m.complete()
		}
		run.pane = paneList
		return m, nil
	case key == "alt+up":
		return m.moveList(-1)
	case key == "alt+down":
		return m.moveList(1)
	case key == "up":
		m.recall(-1)
	case key == "down":
		m.recall(1)
	case key == "left":
		run.cursor = max(run.cursor-1, 0)
	case key == "right":
		run.cursor = min(run.cursor+1, len(run.input))
	case key == "home":
		run.cursor = 0
	case key == "end":
		run.cursor = len(run.input)
	case key == "ctrl+u":
		run.clearInput()
	case key == "backspace":
		if run.cursor > 0 {
			run.input = append(run.input[:run.cursor-1:run.cursor-1], run.input[run.cursor:]...)
			run.cursor--
		}
	case key == "delete":
		if run.cursor < len(run.input) {
			run.input = append(run.input[:run.cursor:run.cursor], run.input[run.cursor+1:]...)
		}
	case key == "enter":
		if expression := strings.TrimSpace(string(run.input)); expression != "" {
			next, cmd := m.evaluate(expression)
			if cmd != nil { // evaluating: the line is done with; a refusal (the program runs) leaves the typed text
				next.(ProjectModel).dbg.clearInput()
			}
			return next, cmd
		}
		if run.view == viewVariables {
			return m.toggleVariable()
		}
		return m.showFrame(run.frame)
	case typing:
		typed := []rune(sanitizeEditable(string(msg.Runes)))
		rest := append([]rune(nil), run.input[run.cursor:]...)
		run.input = append(append(run.input[:run.cursor:run.cursor], typed...), rest...)
		run.cursor += len(typed)
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
		case len(keys) > 0 && keys[0] == actionDebugRestart:
			return m.startDebug()
		case len(keys) > 0 && strings.HasPrefix(keys[0], actionDebugStep):
			return m.debugStep(strings.TrimPrefix(keys[0], actionDebugStep))
		case len(keys) > 0 && strings.HasPrefix(keys[0], actionDebugView):
			if view, err := strconv.Atoi(strings.TrimPrefix(keys[0], actionDebugView)); err == nil {
				m.dbg.view = debugView(view)
			}
		}
		return m, nil
	}
	m.dbg.sel = nil
	if left := leftColumnWidth(max(m.editorWidth(), 1)); x > left { // a click in the console gives it the keys; dragging selects text
		m.dbg.pane = paneConsole
		cx, cy := m.consoleCell(x, y)
		m.dbg.sel = &termSelection{anchorX: cx, anchorY: cy, curX: cx, curY: cy, dragging: true}
		return m, nil
	}
	m.dbg.pane = paneList
	m.dbg.popup = nil // the list has the keys now: the suggestions are closed
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

// consoleCell turns a position in the panel into a cell of the console column (a row of the drawn lines, a character of it).
func (m ProjectModel) consoleCell(x, y int) (cx, cy int) {
	width := max(m.editorWidth(), 1)
	cx = max(x-leftColumnWidth(width)-1, 0)
	cy = max(y-1, 0)
	if rows := len(m.dbg.shown); rows > 0 {
		cy = min(cy, rows-1)
	}
	return cx, cy
}

// selectedCell draws line y of the console column with the selected part in the selection style.
func (m ProjectModel) selectedCell(line string, y, width int, base lipgloss.Style) string {
	var out strings.Builder
	var run strings.Builder
	used, current := 0, false
	flush := func() {
		if run.Len() > 0 {
			style := base
			if current {
				style = m.theme.Selected
			}
			out.WriteString(style.Render(run.String()))
			run.Reset()
		}
	}
	for x, r := range []rune(line) {
		if covered := m.dbg.sel.covers(x, y); covered != current {
			flush()
			current = covered
		}
		run.WriteRune(r)
		used += runewidth.RuneWidth(r)
	}
	flush()
	if used < width {
		out.WriteString(base.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

// dragConsole continues a selection in the console column: moving extends it, letting go copies the text to the clipboard.
func (m ProjectModel) dragConsole(msg tea.MouseMsg, x, y int) ProjectModel {
	sel := *m.dbg.sel
	sel.curX, sel.curY = m.consoleCell(x, y)
	sel.moved = sel.moved || sel.curX != sel.anchorX || sel.curY != sel.anchorY
	switch msg.Action {
	case tea.MouseActionMotion:
	case tea.MouseActionRelease:
		sel.dragging = false
		if sel.moved {
			cells := make([][]string, len(m.dbg.shown))
			for i, line := range m.dbg.shown {
				for _, r := range line {
					cells[i] = append(cells[i], string(r))
				}
			}
			if text := sel.text(cells); text != "" && m.hooks.WriteClipboard != nil {
				if err := m.hooks.WriteClipboard(text); err != nil {
					m.say("Cannot copy: "+err.Error(), true)
				} else {
					m.say("Copied "+countLines(text), false)
				}
			}
		}
	default:
		return m
	}
	m.dbg.sel = &sel
	return m
}
