package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	pickerWidth    = 72
	pickerMaxRows  = 10
	pickerChrome   = 4 // title, search line, hint, and a blank line of air
	pickerHintText = " type to search · ↑↓ choose · enter picks · esc cancels"
)

// PickItem is one entry of a picker: what it is called, what it is for, and whether it is usable (a missing tool is dim).
// Lead, when set, is shown first as "[lead]" in a column (the shortcut of a command).
type PickItem struct {
	Label, Detail string
	Lead          string
	Dim           bool
}

// pickOutcome is what a key or click did to a picker.
type pickOutcome int

const (
	pickNone pickOutcome = iota
	pickChosen
	pickCancelled
)

// PickerModel is a searchable list in a box over the window: typing narrows it to the entries that contain all typed words.
type PickerModel struct {
	theme Theme
	title string
	items []PickItem
	query []rune
	shown []int // indexes of the entries that match the query
	sel   int   // selected position in shown
	top   int   // first shown position that is visible
}

func newPicker(theme Theme, title string, items []PickItem) PickerModel {
	p := PickerModel{theme: theme, title: title, items: items}
	return p.refilter()
}

// Chosen returns the index in the original item list of the selected entry, -1 when nothing matches.
func (p PickerModel) Chosen() int {
	if p.sel < 0 || p.sel >= len(p.shown) {
		return -1
	}
	return p.shown[p.sel]
}

func (p PickerModel) refilter() PickerModel {
	words := strings.Fields(strings.ToLower(string(p.query)))
	p.shown = p.shown[:0:0]
	for i, item := range p.items {
		text := strings.ToLower(item.Lead + " " + item.Label + " " + item.Detail)
		match := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				match = false
				break
			}
		}
		if match {
			p.shown = append(p.shown, i)
		}
	}
	p.sel, p.top = 0, 0
	return p
}

func (p PickerModel) rows(height int) int {
	return max(min(pickerMaxRows, max(height-pickerChrome-2, 1)), 1)
}

func (p PickerModel) move(delta, rows int) PickerModel {
	if len(p.shown) == 0 {
		return p
	}
	p.sel = min(max(p.sel+delta, 0), len(p.shown)-1)
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+rows {
		p.top = p.sel - rows + 1
	}
	return p
}

// handleKey applies a key; height is the height of the window (it decides how many entries are visible).
func (p PickerModel) handleKey(msg tea.KeyMsg, height int) (PickerModel, pickOutcome) {
	rows := p.rows(height)
	switch key := msg.String(); key {
	case "esc", "ctrl+c":
		return p, pickCancelled
	case "enter":
		if p.Chosen() >= 0 {
			return p, pickChosen
		}
	case "up", "ctrl+p":
		return p.move(-1, rows), pickNone
	case "down", "ctrl+n":
		return p.move(1, rows), pickNone
	case "pgup":
		return p.move(-rows, rows), pickNone
	case "pgdown":
		return p.move(rows, rows), pickNone
	case "home":
		return p.move(-len(p.shown), rows), pickNone
	case "end":
		return p.move(len(p.shown), rows), pickNone
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			return p.refilter(), pickNone
		}
	case "ctrl+u":
		p.query = nil
		return p.refilter(), pickNone
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			p.query = append(p.query[:len(p.query):len(p.query)], []rune(sanitize(string(msg.Runes)))...)
			return p.refilter(), pickNone
		}
	}
	return p, pickNone
}

// origin is the screen position of the top-left corner of the box.
func (p PickerModel) origin(width, height int) (x, y int) {
	rows := p.rows(height)
	boxHeight := pickerChrome + rows
	return max((width-pickerWidth)/2, 0), max((height-boxHeight)/2, 0)
}

// handleMouse applies a click: an entry is chosen, the wheel scrolls. Coordinates are screen coordinates.
func (p PickerModel) handleMouse(msg tea.MouseMsg, width, height int) (PickerModel, pickOutcome) {
	rows := p.rows(height)
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return p.move(-wheelLines, rows), pickNone
	case tea.MouseButtonWheelDown:
		return p.move(wheelLines, rows), pickNone
	}
	if !leftPress(msg) {
		return p, pickNone
	}
	x, y := p.origin(width, height)
	if msg.X < x || msg.X >= x+min(pickerWidth, width) {
		return p, pickNone
	}
	if row := msg.Y - (y + 2); row >= 0 && row < rows && p.top+row < len(p.shown) {
		p.sel = p.top + row
		return p, pickChosen
	}
	return p, pickNone
}

// overlay draws the box over the screen (a rendered window of width x height).
func (p PickerModel) overlay(screen string, width, height int) string {
	boxWidth := min(pickerWidth, width)
	rows := p.rows(height)
	lines := []string{
		p.theme.Header.Render(padRight(" "+sanitize(p.title), boxWidth)),
		p.theme.Selected.Render(padRight(" > "+string(p.query)+"▏", boxWidth)),
	}
	leadWidth := p.leadWidth()
	for i := 0; i < rows; i++ {
		pos := p.top + i
		switch {
		case pos >= len(p.shown) && i == 0 && len(p.shown) == 0:
			lines = append(lines, p.theme.Equal.Faint(true).Render(padRight(" nothing matches", boxWidth)))
		case pos >= len(p.shown):
			lines = append(lines, p.theme.Equal.Render(strings.Repeat(" ", boxWidth)))
		default:
			item := p.items[p.shown[pos]]
			text := " "
			if leadWidth > 0 {
				text += padRight(leadText(item.Lead), leadWidth) + "  "
			}
			text += sanitize(item.Label)
			if item.Detail != "" {
				text += "  " + sanitize(item.Detail)
			}
			style := p.theme.Equal
			if item.Dim {
				style = style.Faint(true)
			}
			if pos == p.sel {
				style = p.theme.Selected
			}
			lines = append(lines, style.Render(padRight(text, boxWidth)))
		}
	}
	lines = append(lines, p.theme.Equal.Render(strings.Repeat(" ", boxWidth)), p.theme.Footer.Render(padRight(pickerHintText, boxWidth)))
	x, y := p.origin(width, height)
	return overlayBox(screen, lines, boxWidth, x, y)
}

// leadText is the shortcut column text of an entry: "[ctrl+s]", empty when the entry has none.
func leadText(lead string) string {
	if lead == "" {
		return ""
	}
	return "[" + sanitize(lead) + "]"
}

// leadWidth is the width of the shortcut column: the widest "[shortcut]" of all entries, 0 when no entry has one.
func (p PickerModel) leadWidth() int {
	width := 0
	for _, item := range p.items {
		width = max(width, ansi.StringWidth(leadText(item.Lead)))
	}
	return width
}

// overlayBox draws rows (each exactly width columns wide) over the screen at column x and row y; the screen shows around it.
func overlayBox(screen string, rows []string, width, x, y int) string {
	lines := strings.Split(screen, "\n")
	for i, row := range rows {
		if y+i >= len(lines) {
			break
		}
		line := lines[y+i]
		left := ansi.Truncate(line, x, "")
		lines[y+i] = left + padRight("", x-ansi.StringWidth(left)) + row + ansi.TruncateLeft(line, x+width, "")
	}
	return strings.Join(lines, "\n")
}
