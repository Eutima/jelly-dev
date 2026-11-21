package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the styles used by the views.
type Theme struct {
	Header         lipgloss.Style
	Footer         lipgloss.Style
	LineNumber     lipgloss.Style
	Equal          lipgloss.Style
	Added          lipgloss.Style
	Deleted        lipgloss.Style
	Modified       lipgloss.Style
	Filler         lipgloss.Style
	AddedWord      lipgloss.Style
	DeletedWord    lipgloss.Style
	ModifiedWord   lipgloss.Style
	Conflict       lipgloss.Style
	ConflictWord   lipgloss.Style
	Gutter         lipgloss.Style
	GutterCur      lipgloss.Style
	Selected       lipgloss.Style // selected row of a focused list
	SelectedDim    lipgloss.Style // selected row of a list that does not have the focus
	StatusAdded    lipgloss.Style
	StatusDeleted  lipgloss.Style
	StatusModified lipgloss.Style
	Jelly          lipgloss.Style // the waiting animation
	Ours           lipgloss.Style // lines changed by the local side (merge view)
	Theirs         lipgloss.Style // lines changed by the remote side (merge view)
	Warning        lipgloss.Style // unsaved state, refused actions, failures (merge view)
	ConflictMark   lipgloss.Style // marker and counter of unresolved conflicts (merge view)
	painter        *syntaxPainter // syntax colors, nil for themes without them
}

// renderCell draws one pane cell exactly cellWidth columns wide.
func (t Theme) renderCell(cell Cell, numWidth, cellWidth int) string {
	style := t.styleFor(cell.Kind)
	if cell.Kind == CellFiller {
		return style.Render(strings.Repeat(" ", cellWidth))
	}
	// A pane narrower than the number column drops the number instead of showing a cut-off one.
	num := numWidth
	if cellWidth < numWidth {
		num = 0
	}
	textWidth := cellWidth - num
	number := ""
	if num > 0 {
		number = fmt.Sprintf("%*d ", num-1, cell.No)
	}
	text := renderSyntaxText(cell.Text, cell.Spans, cell.Syntax, t.painter, textWidth, style, t.wordStyleFor(cell.Kind))
	return t.LineNumber.Background(style.GetBackground()).Render(number) + text
}

// renderCellLines draws a pane cell as exactly height lines of cellWidth columns. With wrap the text continues on
// further lines (only the first one carries the line number); without it the text is cut with "…".
func (t Theme) renderCellLines(cell Cell, numWidth, cellWidth, height int, wrap bool) []string {
	style := t.styleFor(cell.Kind)
	blank := style.Render(strings.Repeat(" ", cellWidth))
	var lines []string
	switch {
	case cell.Kind == CellFiller:
	case !wrap:
		lines = []string{t.renderCell(cell, numWidth, cellWidth)}
	default:
		num := numWidth
		if cellWidth < numWidth {
			num = 0
		}
		wrapped := renderSyntaxLines(cell.Text, cell.Spans, cell.Syntax, t.painter, cellWidth-num, style, t.wordStyleFor(cell.Kind))
		for i, text := range wrapped {
			number := strings.Repeat(" ", num)
			if i == 0 && num > 0 {
				number = fmt.Sprintf("%*d ", num-1, cell.No)
			}
			lines = append(lines, t.LineNumber.Background(style.GetBackground()).Render(number)+text)
		}
		if cellWidth-num <= 0 {
			lines = []string{t.renderCell(cell, numWidth, cellWidth)}
		}
	}
	for len(lines) < height {
		lines = append(lines, blank)
	}
	return lines
}

func (t Theme) styleFor(kind CellKind) lipgloss.Style {
	switch kind {
	case CellAdded:
		return t.Added
	case CellDeleted:
		return t.Deleted
	case CellModified:
		return t.Modified
	case CellConflict:
		return t.Conflict
	case CellOurs:
		return t.Ours
	case CellTheirs:
		return t.Theirs
	case CellCursor:
		return t.Selected
	case CellFiller:
		return t.Filler
	default:
		return t.Equal
	}
}

func (t Theme) wordStyleFor(kind CellKind) lipgloss.Style {
	switch kind {
	case CellAdded:
		return t.AddedWord
	case CellDeleted:
		return t.DeletedWord
	case CellModified:
		return t.ModifiedWord
	case CellConflict:
		return t.ConflictWord
	case CellCursor:
		return t.Selected
	default:
		return t.styleFor(kind)
	}
}
