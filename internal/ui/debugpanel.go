package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/mattn/go-runewidth"
)

const (
	actionDebugStop  = "debugstop"
	actionDebugFrame = "frame:"
	minStackWidth    = 24 // the call stack column, left of the console
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

// debugRows draws the body of the panel: the call stack on the left, the console on the right. Long lines wrap. It also returns, for every
// line of the call stack column, the frame it belongs to (a click on it selects the frame).
func (m ProjectModel) debugRows(width, body int) (lines []string, frameOf []int) {
	run := m.dbg
	stackWidth := max(min(width/3, 48), min(minStackWidth, width/2))
	consoleWidth := max(width-stackWidth-1, 1)

	frameLines := func(i int) []string {
		frame := run.frames[i]
		text := frame.Name
		if frame.Path != "" {
			text += "  " + filepath.Base(frame.Path) + ":" + fmt.Sprint(frame.Line)
		}
		return splitRowLines(sanitize(text), max(stackWidth-2, 1))
	}
	run.top = scrollToShow(run.frame, run.top, body, len(run.frames), func(i int) int { return len(frameLines(i)) })
	stack, rowOf := drawRows(run.top, len(run.frames), body, frameLines)
	first := map[int]bool{}
	for y := range stack {
		prefix := "  "
		if !first[rowOf[y]] {
			first[rowOf[y]] = true
			if rowOf[y] == run.frame {
				prefix = "> "
			}
		}
		stack[y] = prefix + stack[y]
	}
	if len(run.frames) == 0 {
		switch run.state {
		case debugStopped:
			stack = []string{"  reading the stack ..."}
		case debugRunning:
			stack = []string{"  running"}
		}
		rowOf = make([]int, len(stack))
		for i := range rowOf {
			rowOf[i] = -1
		}
	}

	var console []string // the tail of the output that fits, wrapped
	for i := len(run.console) - 1; i >= 0 && len(console) < body; i-- {
		wrapped := splitRowLines(run.console[i], consoleWidth)
		console = append(wrapped, console...)
	}
	if len(console) > body {
		console = console[len(console)-body:]
	}
	for y := 0; y < body; y++ {
		left, right := "", ""
		if y < len(stack) {
			left = stack[y]
		}
		if y < len(console) {
			right = console[y]
		}
		style := m.theme.Equal
		if y < len(stack) && rowOf[y] == run.frame && run.frame >= 0 && len(run.frames) > 0 {
			style = m.theme.Selected
		}
		lines = append(lines, style.Render(padWide(left, stackWidth))+m.theme.Gutter.Render("│")+m.theme.Equal.Render(padWide(right, consoleWidth)))
		if y < len(stack) {
			frameOf = append(frameOf, rowOf[y])
		} else {
			frameOf = append(frameOf, -1)
		}
	}
	return lines, frameOf
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

// debugKey handles a key while the panel has the focus: up and down choose a frame, enter shows it, the leave key goes back to the editor.
func (m ProjectModel) debugKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	run := m.dbg
	key := msg.String()
	switch {
	case keys.Is(keymap.LeaveTerminal, key) || key == "esc":
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case key == "up" || key == "k":
		return m.showFrame(max(run.frame-1, 0)), nil
	case key == "down" || key == "j":
		return m.showFrame(min(run.frame+1, len(run.frames)-1)), nil
	case key == "enter":
		return m.showFrame(run.frame), nil
	}
	return m, nil
}

// clickDebug handles a click in the panel: [×] stops the run, a frame is shown, anywhere else gives the panel the focus.
func (m ProjectModel) clickDebug(msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if !leftPress(msg) {
		return m, nil
	}
	if y == 0 {
		if keys := m.debugHeader().visibleKeysAt(x, m.editorWidth()); len(keys) > 0 && keys[0] == actionDebugStop {
			return m.stopDebug()
		}
		m.focus = focusDebug
		return m, nil
	}
	m.focus = focusDebug
	_, frameOf := m.debugRows(max(m.editorWidth(), 1), m.debugPanelHeight()-1)
	if y-1 < len(frameOf) && frameOf[y-1] >= 0 {
		return m.showFrame(frameOf[y-1]), nil
	}
	return m, nil
}
