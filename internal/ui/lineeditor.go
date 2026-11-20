package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// editAction tells the caller what a key press did to a lineEditor.
type editAction int

const (
	// editContinue: the editor handled the key (or ignored it) and stays open.
	editContinue editAction = iota
	// editApply: the user confirmed the line with enter.
	editApply
	// editCancel: the user cancelled with esc or ctrl+c.
	editCancel
)

// lineEditor edits one line of text with a cursor (no line breaks are possible).
type lineEditor struct {
	text   []rune
	cursor int // index of the rune the cursor is on; len(text) means after the last rune
}

// newLineEditor starts editing text with the cursor at its end. The existing text is kept exactly as it is
// (control characters of the file are shown as "·" but not removed); only inserted text is sanitized.
func newLineEditor(text string) lineEditor {
	runes := []rune(text)
	return lineEditor{text: runes, cursor: len(runes)}
}

// String returns the edited line.
func (e lineEditor) String() string { return string(e.text) }

// sanitizeEditable drops everything that must not end up in a line: line breaks and control characters.
// Tabs are kept.
func sanitizeEditable(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || !isControl(r) {
			return r
		}
		return -1
	}, text)
}

// handle applies a key press to the editor and returns the editor and what the caller should do.
func (e lineEditor) handle(msg tea.KeyMsg) (lineEditor, editAction) {
	switch msg.Type {
	case tea.KeyEnter:
		return e, editApply
	case tea.KeyEsc, tea.KeyCtrlC:
		return e, editCancel
	case tea.KeyLeft, tea.KeyCtrlB:
		e.cursor = max(e.cursor-1, 0)
	case tea.KeyRight, tea.KeyCtrlF:
		e.cursor = min(e.cursor+1, len(e.text))
	case tea.KeyHome, tea.KeyCtrlA:
		e.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		e.cursor = len(e.text)
	case tea.KeyBackspace:
		if e.cursor > 0 {
			e.text = append(append([]rune(nil), e.text[:e.cursor-1]...), e.text[e.cursor:]...)
			e.cursor--
		}
	case tea.KeyDelete, tea.KeyCtrlD:
		if e.cursor < len(e.text) {
			e.text = append(append([]rune(nil), e.text[:e.cursor]...), e.text[e.cursor+1:]...)
		}
	case tea.KeyCtrlU: // delete everything before the cursor
		e.text = append([]rune(nil), e.text[e.cursor:]...)
		e.cursor = 0
	case tea.KeyCtrlK: // delete everything from the cursor on
		e.text = append([]rune(nil), e.text[:e.cursor]...)
	case tea.KeySpace:
		e = e.insert([]rune{' '})
	case tea.KeyTab:
		e = e.insert([]rune{'\t'})
	case tea.KeyRunes:
		e = e.insert(msg.Runes)
	}
	return e, editContinue
}

// insert adds runes at the cursor; line breaks and control characters (also from pasted text) are dropped.
func (e lineEditor) insert(runes []rune) lineEditor {
	clean := []rune(sanitizeEditable(string(runes)))
	if len(clean) == 0 {
		return e
	}
	text := make([]rune, 0, len(e.text)+len(clean))
	text = append(text, e.text[:e.cursor]...)
	text = append(text, clean...)
	text = append(text, e.text[e.cursor:]...)
	e.text = text
	e.cursor += len(clean)
	return e
}

// item is one rune prepared for display.
type item struct {
	text  string
	width int
}

// items converts the text to display items; a tab becomes spaces up to the next tab stop.
func (e lineEditor) items() []item {
	items := make([]item, 0, len(e.text)+1)
	column := 0
	for _, r := range e.text {
		if r == '\t' {
			spaces := tabWidth - column%tabWidth
			items = append(items, item{text: strings.Repeat(" ", spaces), width: spaces})
			column += spaces
			continue
		}
		if isControl(r) {
			items = append(items, item{text: string(replacement), width: 1})
			column++
			continue
		}
		width := max(runewidth.RuneWidth(r), 0)
		items = append(items, item{text: string(r), width: width})
		column += width
	}
	return items
}

// view draws the line in exactly width columns, scrolled so the cursor is visible. The cursor cell is drawn
// with cursorStyle, everything else with base.
func (e lineEditor) view(width int, base, cursorStyle lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	items := e.items()
	items = append(items, item{text: " ", width: 1}) // the cell after the last rune holds the cursor
	// Find the first item to show: drop items from the left until the cursor item fits.
	start := 0
	for used := widthOf(items[start : e.cursor+1]); used > width && start < e.cursor; start++ {
		used -= items[start].width
	}
	var out strings.Builder
	used := 0
	for i := start; i < len(items) && used+items[i].width <= width; i++ {
		style := base
		if i == e.cursor {
			style = cursorStyle
		}
		out.WriteString(style.Render(items[i].text))
		used += items[i].width
	}
	if used < width {
		out.WriteString(base.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

func widthOf(items []item) int {
	total := 0
	for _, it := range items {
		total += it.width
	}
	return total
}
