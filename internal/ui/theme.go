package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the colors used by the views (JetBrains-like dark palette).
type Theme struct {
	Header       lipgloss.Style
	Footer       lipgloss.Style
	LineNumber   lipgloss.Style
	Equal        lipgloss.Style
	Added        lipgloss.Style
	Deleted      lipgloss.Style
	Modified     lipgloss.Style
	Filler       lipgloss.Style
	AddedWord    lipgloss.Style
	DeletedWord  lipgloss.Style
	ModifiedWord lipgloss.Style
	Conflict     lipgloss.Style
	ConflictWord lipgloss.Style
	Gutter       lipgloss.Style
	GutterCur    lipgloss.Style
}

// DarkTheme returns the default theme.
func DarkTheme() Theme {
	base := lipgloss.NewStyle()
	return Theme{
		Header:       base.Foreground(lipgloss.Color("#ced0d6")).Background(lipgloss.Color("#2b2d30")).Bold(true),
		Footer:       base.Foreground(lipgloss.Color("#868a91")).Background(lipgloss.Color("#2b2d30")),
		LineNumber:   base.Foreground(lipgloss.Color("#4e5157")),
		Equal:        base.Foreground(lipgloss.Color("#bcbec4")),
		Added:        base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#294436")),
		Deleted:      base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#4a3a3c")),
		Modified:     base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#2d3f5c")),
		Filler:       base.Background(lipgloss.Color("#26282b")),
		AddedWord:    base.Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#3b7a55")),
		DeletedWord:  base.Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#7d4a50")),
		Conflict:     base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#4f2f3a")),
		ConflictWord: base.Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#8c4256")),
		ModifiedWord: base.Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#3f67a5")),
		Gutter:       base.Foreground(lipgloss.Color("#4e5157")),
		GutterCur:    base.Foreground(lipgloss.Color("#56a8f5")).Bold(true),
	}
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
