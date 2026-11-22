package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-dev/internal/diff"
)

const (
	tabWidth    = 4
	replacement = '·'
)

// segment is a run of display text that is either plain or highlighted (a changed word).
type segment struct {
	text      string
	highlight bool
	paint     paint // syntax foreground, zero for plain text
}

// displayCell is one rune prepared for display.
type displayCell struct {
	r         rune
	width     int
	highlight bool
	paint     paint
}

// isControl reports whether r must never reach the terminal as-is.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) ||
		r == '\u2028' || r == '\u2029'
}

// displayCells converts text into display cells: tabs are expanded to tab stops, control characters
// are replaced, and runes inside spans (byte offsets into text) are marked as highlighted.
func displayCells(text string, spans []diff.Span, syntax []SyntaxSpan, painter *syntaxPainter) []displayCell {
	var (
		cells      []displayCell
		column     int
		next       int // index of the first span that may still contain the current byte
		nextSyntax int // same for syntax spans
	)
	for offset, r := range text {
		for next < len(spans) && spans[next].End <= offset {
			next++
		}
		highlight := next < len(spans) && offset >= spans[next].Start
		for nextSyntax < len(syntax) && syntax[nextSyntax].End <= offset {
			nextSyntax++
		}
		var cellPaint paint
		if nextSyntax < len(syntax) && offset >= syntax[nextSyntax].Start {
			cellPaint = painter.paintFor(syntax[nextSyntax].Token)
		}
		switch {
		case r == '\t':
			for spaces := tabWidth - column%tabWidth; spaces > 0; spaces-- {
				cells = append(cells, displayCell{r: ' ', width: 1, highlight: highlight, paint: cellPaint})
				column++
			}
		case isControl(r):
			cells = append(cells, displayCell{r: replacement, width: 1, highlight: highlight, paint: cellPaint})
			column++
		default:
			width := runewidth.RuneWidth(r)
			cells = append(cells, displayCell{r: r, width: width, highlight: highlight, paint: cellPaint})
			column += width
		}
	}
	return cells
}

// sanitize returns text that is safe to print: tabs expanded, control characters replaced.
func sanitize(text string) string {
	var out strings.Builder
	for _, cell := range displayCells(text, nil, nil, nil) {
		out.WriteRune(cell.r)
	}
	return out.String()
}

// renderSegmentLine draws segments exactly width columns wide.
func renderSegmentLine(segments []segment, width int, base, word lipgloss.Style) string {
	var out strings.Builder
	used := 0
	for _, seg := range segments {
		style := base
		switch {
		case seg.highlight:
			style = word
		case seg.paint.color != "":
			style = base.Foreground(lipgloss.Color(seg.paint.color)).Bold(seg.paint.bold || base.GetBold())
		case seg.paint.bold:
			style = base.Bold(true)
		}
		out.WriteString(style.Render(seg.text))
		used += runewidth.StringWidth(seg.text)
	}
	if used < width {
		out.WriteString(base.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

// wrapCells splits display cells into lines of at most width columns (hard wrap; a wide rune that does not fit
// starts the next line). Text without cells gives one empty line.
func wrapCells(cells []displayCell, width int) [][]displayCell {
	if width <= 0 {
		return nil
	}
	lines := [][]displayCell{nil}
	used := 0
	for _, cell := range cells {
		if used+cell.width > width && used > 0 {
			lines, used = append(lines, nil), 0
		}
		last := len(lines) - 1
		lines[last] = append(lines[last], cell)
		used += cell.width
	}
	return lines
}

// wrappedHeight is the number of lines text takes when wrapped to width columns.
func wrappedHeight(text string, width int) int {
	if width <= 0 {
		return 1
	}
	used, lines := 0, 1
	for _, cell := range displayCells(text, nil, nil, nil) {
		if used+cell.width > width && used > 0 {
			lines, used = lines+1, 0
		}
		used += cell.width
	}
	return lines
}

// groupSegments joins neighbouring cells of the same look into segments.
func groupSegments(cells []displayCell) []segment {
	var segments []segment
	for _, cell := range cells {
		if last := len(segments) - 1; last >= 0 && segments[last].highlight == cell.highlight &&
			segments[last].paint == cell.paint {
			segments[last].text += string(cell.r)
			continue
		}
		segments = append(segments, segment{text: string(cell.r), highlight: cell.highlight, paint: cell.paint})
	}
	return segments
}

// renderSyntaxLines is renderSyntaxText for text that is wrapped instead of cut: it returns one string of
// exactly width columns per wrapped line.
func renderSyntaxLines(
	text string, spans []diff.Span, syntax []SyntaxSpan, painter *syntaxPainter, width int, base, word lipgloss.Style,
) []string {
	wrapped := wrapCells(displayCells(text, spans, syntax, painter), width)
	lines := make([]string, len(wrapped))
	for i, cells := range wrapped {
		lines[i] = renderSegmentLine(groupSegments(cells), width, base, word)
	}
	return lines
}

func padRight(text string, width int) string {
	if gap := width - runewidth.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return runewidth.Truncate(text, width, "")
}

// sanitizeEditable drops everything that must not end up in a typed line: line breaks and control characters.
// Tabs are kept.
func sanitizeEditable(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || !isControl(r) {
			return r
		}
		return -1
	}, text)
}

// wrapWords breaks text into lines of at most width columns at the spaces (a word longer than a line is cut), and into at most
// maxLines lines, a safety limit for texts longer than the window (callers pass messageCap). Every step uses up a piece of the text or completes a line, and it
// stops when maxLines lines are complete, so the work does not grow with the length of the text.
func wrapWords(text string, width, maxLines int) []string {
	if width <= 0 || maxLines < 1 {
		return []string{""}
	}
	words := strings.Fields(text)
	var lines []string
	var current strings.Builder
	currentWidth := 0
	done := false
	endLine := func() { // true when the line limit is reached
		lines = append(lines, current.String())
		current.Reset()
		currentWidth = 0
	}
	for w := 0; w < len(words) && !done; w++ {
		word := words[w]
		for word != "" {
			space := 0
			if currentWidth > 0 {
				space = 1
			}
			if wordWidth := runewidth.StringWidth(word); currentWidth+space+wordWidth <= width {
				if space > 0 {
					current.WriteByte(' ')
				}
				current.WriteString(word)
				currentWidth += space + wordWidth
				break
			}
			if currentWidth > 0 { // the word starts a new line
				endLine()
				if len(lines) >= maxLines {
					done = true
					break
				}
				continue
			}
			head := fitPrefix(word, width) // the word is longer than a line: cut it
			current.WriteString(head)
			currentWidth = runewidth.StringWidth(head)
			word = word[len(head):]
			endLine()
			if len(lines) >= maxLines {
				done = word != "" || w < len(words)-1
				break
			}
		}
		if len(lines) >= maxLines {
			break
		}
	}
	if current.Len() > 0 && len(lines) < maxLines {
		endLine()
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// fitPrefix returns the longest start of s that is at most width columns wide, at least its first rune (also when that one is wider,
// so that a cut always makes progress).
func fitPrefix(s string, width int) string {
	used := 0
	for i, r := range s {
		w := runewidth.RuneWidth(r)
		if used+w > width && i > 0 {
			return s[:i]
		}
		used += w
		if used >= width {
			return s[:i+utf8.RuneLen(r)]
		}
	}
	return s
}

// messageCap is the most lines a message may take: half the window, so that a very long text cannot push the screen out of shape.
func messageCap(height int) int { return max(height/2, 3) }
