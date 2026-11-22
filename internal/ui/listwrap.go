package ui

import "github.com/mattn/go-runewidth"

// A list whose rows may take more than one line (a long name wraps instead of being cut off) scrolls by rows: top is the first row shown,
// and the rows follow each other until the body is full.

// scrollToShow returns (count is the number of rows of the list) the top that shows the selected row completely, moving the old top as little as it can: up when the row is above it,
// down until the row ends on the last line of the body. A row taller than the body is shown from its first line. It looks at the rows
// above the selected one only as far as the body reaches, so a jump to the end of a long list stays cheap.
func scrollToShow(selected, top, body, count int, height func(row int) int) int {
	if selected < 0 || selected >= count {
		return max(top, 0) // nothing selected (an empty list): there is no row to show
	}
	top = max(min(top, selected), 0)
	used, first := height(selected), selected
	for first > 0 {
		above := height(first - 1)
		if used+above > body {
			break
		}
		used += above
		first--
	}
	return max(top, first)
}

// splitRowLines cuts text into lines of at most width columns: the continuation of a long text, nothing is dropped. It cuts at the column, not
// at a word, and does not clean the text: callers pass it through sanitize first. A rune wider than the width stands alone on its line (so the
// cut always makes progress), which is then wider than the row.
func splitRowLines(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	var lines []string
	var current []rune
	used := 0
	for _, r := range text {
		cell := runewidth.RuneWidth(r)
		if used+cell > width && len(current) > 0 {
			lines = append(lines, string(current))
			current, used = nil, 0
		}
		current = append(current, r)
		used += cell
	}
	return append(lines, string(current))
}

// drawRows draws the rows of a list from top on until body lines are filled; rowLines gives the lines of one row. It also returns, for every
// drawn line, the row it belongs to (a click on a line selects that row). Lines of the last row that do not fit are not drawn.
func drawRows(top, count, body int, rowLines func(row int) []string) (lines []string, rowOf []int) {
	for row := top; row < count && len(lines) < body; row++ {
		for _, line := range rowLines(row) {
			if len(lines) == body {
				break
			}
			lines = append(lines, line)
			rowOf = append(rowOf, row)
		}
	}
	return lines, rowOf
}
