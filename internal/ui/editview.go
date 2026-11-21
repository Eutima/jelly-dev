package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// item is one rune prepared for display.
type item struct {
	text  string
	width int
}

// editItems converts the text to display items; a tab becomes spaces up to the next tab stop and control
// characters are drawn as "·" (the text itself is never changed).
func editItems(text []rune) []item {
	items := make([]item, 0, len(text)+1)
	column := 0
	for _, r := range text {
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

func widthOf(items []item) int {
	total := 0
	for _, it := range items {
		total += it.width
	}
	return total
}

// editLineStyles are the three looks of a cell in the editor.
type editLineStyles struct{ base, selected, cursor lipgloss.Style }

// renderEditLine draws one line of the editor in exactly width columns. cursor is the rune index of the cursor
// cell (-1 for none; len(text) is the cell behind the last rune) and the line is scrolled so that it is
// visible. Runes from selFrom up to selTo are drawn as selected.
func renderEditLine(text []rune, width, cursor, selFrom, selTo int, styles editLineStyles) string {
	if width <= 0 {
		return ""
	}
	items := editItems(text)
	items = append(items, item{text: " ", width: 1}) // the cell behind the last rune holds the cursor and marks a selected line break
	start := 0
	if cursor >= 0 {
		cursor = min(cursor, len(items)-1)
		for used := widthOf(items[start : cursor+1]); used > width && start < cursor; start++ {
			used -= items[start].width
		}
	}
	var out strings.Builder
	used := 0
	for i := start; i < len(items) && used+items[i].width <= width; i++ {
		style := styles.base
		switch {
		case i == cursor:
			style = styles.cursor
		case i >= selFrom && i < selTo:
			style = styles.selected
		}
		out.WriteString(style.Render(items[i].text))
		used += items[i].width
	}
	if used < width {
		out.WriteString(styles.base.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}
