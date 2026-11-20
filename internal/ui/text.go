package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-diff/internal/diff"
)

const (
	tabWidth    = 4
	replacement = '·'
	ellipsis    = "…"
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

// layoutText fits text into width columns (adding "…" when it is cut) and groups it into segments.
func layoutText(text string, spans []diff.Span, syntax []SyntaxSpan, painter *syntaxPainter, width int) []segment {
	if width <= 0 {
		return nil
	}
	cells := displayCells(text, spans, syntax, painter)
	total := 0
	for _, cell := range cells {
		total += cell.width
	}
	limit := width
	cut := total > width
	if cut {
		limit = width - runewidth.StringWidth(ellipsis)
	}
	var (
		segments []segment
		used     int
	)
	for _, cell := range cells {
		if used+cell.width > limit {
			break
		}
		used += cell.width
		if last := len(segments) - 1; last >= 0 && segments[last].highlight == cell.highlight &&
			segments[last].paint == cell.paint {
			segments[last].text += string(cell.r)
			continue
		}
		segments = append(segments, segment{text: string(cell.r), highlight: cell.highlight, paint: cell.paint})
	}
	if cut {
		segments = append(segments, segment{text: ellipsis})
	}
	return segments
}

// renderText draws text exactly width columns wide using base style, and word style for highlights.
func renderText(text string, spans []diff.Span, width int, base, word lipgloss.Style) string {
	return renderSyntaxText(text, spans, nil, nil, width, base, word)
}

// renderSyntaxText is renderText with syntax colors: plain segments get the token foreground on top of
// base; changed words keep the word style so they stay readable.
func renderSyntaxText(
	text string, spans []diff.Span, syntax []SyntaxSpan, painter *syntaxPainter, width int, base, word lipgloss.Style,
) string {
	var out strings.Builder
	used := 0
	for _, seg := range layoutText(text, spans, syntax, painter, width) {
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

func padRight(text string, width int) string {
	if gap := width - runewidth.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return runewidth.Truncate(text, width, "")
}
