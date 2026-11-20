package ui

import "github.com/charmbracelet/lipgloss"

// Theme holds the colors used by the views (JetBrains-like dark palette).
type Theme struct {
	Header     lipgloss.Style
	Footer     lipgloss.Style
	LineNumber lipgloss.Style
	Equal      lipgloss.Style
	Added      lipgloss.Style
	Deleted    lipgloss.Style
	Modified   lipgloss.Style
	Filler     lipgloss.Style
	Gutter     lipgloss.Style
	GutterCur  lipgloss.Style
}

// DarkTheme returns the default theme.
func DarkTheme() Theme {
	base := lipgloss.NewStyle()
	return Theme{
		Header:     base.Foreground(lipgloss.Color("#ced0d6")).Background(lipgloss.Color("#2b2d30")).Bold(true),
		Footer:     base.Foreground(lipgloss.Color("#868a91")).Background(lipgloss.Color("#2b2d30")),
		LineNumber: base.Foreground(lipgloss.Color("#4e5157")),
		Equal:      base.Foreground(lipgloss.Color("#bcbec4")),
		Added:      base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#294436")),
		Deleted:    base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#4a3a3c")),
		Modified:   base.Foreground(lipgloss.Color("#bcbec4")).Background(lipgloss.Color("#2d3f5c")),
		Filler:     base.Background(lipgloss.Color("#26282b")),
		Gutter:     base.Foreground(lipgloss.Color("#4e5157")),
		GutterCur:  base.Foreground(lipgloss.Color("#56a8f5")).Bold(true),
	}
}
