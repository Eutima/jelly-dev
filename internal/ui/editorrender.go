package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// codeCell is one screen cell of a line of code: a character, or a part of an expanded tab.
type codeCell struct {
	text  string
	width int
	index int // index of the rune it belongs to; len(text) for the cell behind the last rune
	paint paint
}

// codeCells lays a line out in screen cells: tabs are expanded to tab stops, control characters are shown as "·"
// (the text itself is never changed), and the syntax colors are attached. The last cell is the one behind the text,
// where the cursor can stand.
func codeCells(text []rune, syntax []SyntaxSpan, painter *syntaxPainter) []codeCell {
	cells := make([]codeCell, 0, len(text)+1)
	column, byteOffset, nextSyntax := 0, 0, 0
	for index, r := range text {
		for nextSyntax < len(syntax) && syntax[nextSyntax].End <= byteOffset {
			nextSyntax++
		}
		var cellPaint paint
		if nextSyntax < len(syntax) && byteOffset >= syntax[nextSyntax].Start {
			cellPaint = painter.paintFor(syntax[nextSyntax].Token)
		}
		switch {
		case r == '\t':
			for spaces := tabWidth - column%tabWidth; spaces > 0; spaces-- {
				cells = append(cells, codeCell{text: " ", width: 1, index: index, paint: cellPaint})
				column++
			}
		case isControl(r):
			cells = append(cells, codeCell{text: string(replacement), width: 1, index: index, paint: cellPaint})
			column++
		default:
			width := max(runewidth.RuneWidth(r), 0)
			cells = append(cells, codeCell{text: string(r), width: width, index: index, paint: cellPaint})
			column += width
		}
		byteOffset += len(string(r))
	}
	return append(cells, codeCell{text: " ", width: 1, index: len(text)})
}

// cursorColumn is the screen column of the first cell of the rune at index.
func cursorColumn(text []rune, index int) int {
	column := 0
	for _, cell := range codeCells(text, nil, nil) {
		if cell.index >= index {
			break
		}
		column += cell.width
	}
	return column
}

// runeAtColumn is the index of the rune that covers a screen column (len(text) behind the end of the line).
func runeAtColumn(text []rune, column int) int {
	used := 0
	for _, cell := range codeCells(text, nil, nil) {
		if column < used+cell.width {
			return cell.index
		}
		used += cell.width
	}
	return len(text)
}

// codeLineStyles are the looks of a cell of the editor.
type codeLineStyles struct {
	base, selected, cursor, match lipgloss.Style
	matches                       [][2]int // rune ranges [from, to) of the search matches in the line
}

// renderCodeLine draws a line of code in exactly width columns, starting at screen column left. cursor is the rune
// index of the cursor cell (-1 for none); runes from selFrom up to selTo are drawn selected.
func (t Theme) renderCodeLine(text []rune, syntax []SyntaxSpan, left, width, cursor, selFrom, selTo int, styles codeLineStyles) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	column, used := 0, 0
	var run strings.Builder
	var runStyle lipgloss.Style
	runKey := ""
	flush := func() {
		if run.Len() > 0 {
			out.WriteString(runStyle.Render(run.String()))
			run.Reset()
		}
	}
	for _, cell := range codeCells(text, syntax, t.painter) {
		start := column
		column += cell.width
		if start < left {
			continue // scrolled out on the left
		}
		if used+cell.width > width {
			break
		}
		style, key := styles.base, "base"
		switch {
		case cell.index == cursor:
			style, key = styles.cursor, "cursor"
		case cell.index >= selFrom && cell.index < selTo:
			style, key = styles.selected, "selected"
		case inRanges(cell.index, styles.matches):
			style, key = styles.match, "match"
		case cell.paint.color != "":
			style = styles.base.Foreground(lipgloss.Color(cell.paint.color)).Bold(cell.paint.bold || styles.base.GetBold())
			key = "paint" + cell.paint.color + map[bool]string{true: "b", false: ""}[cell.paint.bold]
		case cell.paint.bold:
			style, key = styles.base.Bold(true), "bold"
		}
		if run.Len() > 0 && key != runKey {
			flush()
		}
		runStyle, runKey = style, key
		run.WriteString(cell.text)
		used += cell.width
	}
	flush()
	if used < width {
		out.WriteString(styles.base.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

// inRanges reports whether index lies in one of the ranges [from, to).
func inRanges(index int, ranges [][2]int) bool {
	for _, r := range ranges {
		if index >= r[0] && index < r[1] {
			return true
		}
	}
	return false
}
