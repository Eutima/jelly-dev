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
	text := renderText(cell.Text, cell.Spans, textWidth, style, t.wordStyleFor(cell.Kind))
	return t.LineNumber.Background(style.GetBackground()).Render(number) + text
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
	default:
		return t.styleFor(kind)
	}
}
