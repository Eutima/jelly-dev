package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// hoverDelay is how long the mouse pointer rests on a symbol before its documentation is asked for.
const hoverDelay = 500 * time.Millisecond

// hoverResultMsg is the answer of a hover request.
type hoverResultMsg struct {
	seq   int  // the pointer and key presses since the question was asked: a newer one makes the answer stale
	quiet bool // asked by the mouse pointer: no message when there is nothing to show
	text  string
	err   error
}

// hoverDwellMsg says the mouse pointer has rested for hoverDelay on a place of the text.
type hoverDwellMsg struct {
	seq      int
	row, col int
}

// askHover asks the language server for the documentation of the symbol under the cursor.
func (m EditorModel) askHover() (EditorModel, tea.Cmd) {
	switch {
	case m.nav == nil:
		m.notice("E1: No language server available in this editor", true)
		return m, nil
	case m.doc.Path == "":
		m.notice("E32: No file name: save the file first", true)
		return m, nil
	}
	row, col := m.vim.Cursor()
	m.notice("Asking the language server for the documentation...", false)
	return m, m.hoverRequest(row, col, false)
}

// hoverRequest asks for the documentation of the symbol at a place; the answer counts while no key or pointer movement follows.
func (m EditorModel) hoverRequest(row, col int, quiet bool) tea.Cmd {
	text := strings.Join(m.vim.Lines(), "\n")
	path, nav, seq := m.doc.Path, m.nav, m.hoverSeq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		doc, err := nav.Hover(ctx, path, text, row, col)
		return hoverResultMsg{seq: seq, quiet: quiet, text: doc, err: err}
	}
}

// pointerRested starts the wait for the pointer to rest on a character of the text; it returns nil when the pointer is elsewhere.
func (m EditorModel) pointerRested(x, y int) tea.Cmd {
	if m.nav == nil || m.doc.Path == "" || m.completion != nil || m.list != nil || m.nanoAsking() || m.vim.Mode() == vimbuf.Command {
		return nil
	}
	row, col, ok := m.textPosition(x, y)
	if !ok {
		return nil
	}
	line := []rune(m.vim.Line(row))
	if x < m.numberWidth() || col >= len(line) || !isWordRune(line[col]) {
		return nil // not on a character of an identifier
	}
	seq := m.hoverSeq
	return tea.Tick(hoverDelay, func(time.Time) tea.Msg { return hoverDwellMsg{seq: seq, row: row, col: col} })
}

// handleHoverResult shows the documentation while the cursor is still where it was asked for.
func (m EditorModel) handleHoverResult(msg hoverResultMsg) EditorModel {
	if msg.seq != m.hoverSeq {
		if strings.HasPrefix(m.status, "Asking the language server for the documentation") {
			m.notice("", false) // the answer is dropped: the question must not stay on screen
		}
		return m // a key or the pointer moved on
	}
	if msg.quiet && (msg.err != nil || strings.TrimSpace(msg.text) == "") {
		return m
	}
	var missing *lsp.NotInstalledError
	switch {
	case errors.As(msg.err, &missing):
		m.notice(msg.err.Error(), true)
	case errors.Is(msg.err, lsp.ErrNoServer):
		m.notice("No language server for this file type (add one in lsp.json)", true)
	case msg.err != nil:
		m.notice("Language server: "+msg.err.Error(), true)
	default:
		if strings.TrimSpace(msg.text) == "" {
			m.notice("No documentation found", true)
			return m
		}
		m.notice("", false)
		m.hover = msg.text
		m.scrollToCursor() // the documentation takes rows from the text: the cursor line must stay in view
	}
	return m
}

// hoverCap is the most lines the documentation takes: two thirds of the window, so that some of the text stays in view.
func hoverCap(height int) int { return max(height*2/3, 3) }

// hoverRows are the lines of the documentation, wrapped at the spaces; a text longer than hoverCap lines stops there (without a marker).
func (m EditorModel) hoverRows(width int) []string {
	limit := hoverCap(m.height)
	var lines []string
	for _, source := range strings.Split(m.hover, "\n") {
		if len(lines) >= limit {
			break
		}
		source = sanitize(source)
		if strings.TrimSpace(source) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapIndented(source, max(width, 2)-1, limit-len(lines))...)
	}
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = m.theme.Equal.Faint(false).Render(padRight(" "+line, width))
	}
	return rows
}

// wrapIndented wraps a line at the spaces and keeps its indentation (the signatures and code of the documentation) on every row.
func wrapIndented(text string, width, maxLines int) []string {
	trimmed := strings.TrimLeft(text, " \t")
	indent := min(len(strings.ReplaceAll(text[:len(text)-len(trimmed)], "\t", "    ")), width/2)
	lines := wrapWords(trimmed, max(width-indent, 1), maxLines)
	for i := range lines {
		lines[i] = strings.Repeat(" ", indent) + lines[i]
	}
	return lines
}

// nanoAsking tells whether the nano-style editor has a question open: keys then belong to it.
func (m EditorModel) nanoAsking() bool {
	if m.nano == nil {
		return false
	}
	_, _, asking := m.nano.Prompt()
	return asking
}
