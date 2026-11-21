package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/source"
)

// NamedTheme returns a built-in theme by name, drawn with lipgloss's default renderer (the real terminal).
func NamedTheme(name string) (Theme, bool) { return NamedThemeFor(lipgloss.DefaultRenderer(), name) }

// NamedThemeFor returns a built-in theme by name whose styles are rendered by r. A renderer with its own
// color profile lets tests produce colored output without touching the process-wide profile.
func NamedThemeFor(r *lipgloss.Renderer, name string) (Theme, bool) {
	switch name {
	case config.ThemeDark:
		return DarkThemeFor(r), true
	case config.ThemeLight:
		return LightThemeFor(r), true
	case config.ThemeMono:
		return MonoThemeFor(r), true
	}
	return Theme{}, false
}

func hex(color string) lipgloss.Color { return lipgloss.Color(color) }

// DarkTheme returns the default theme (JetBrains-like dark palette).
func DarkTheme() Theme { return DarkThemeFor(lipgloss.DefaultRenderer()) }

// DarkThemeFor is DarkTheme with styles rendered by r.
func DarkThemeFor(r *lipgloss.Renderer) Theme {
	base := r.NewStyle()
	text := base.Foreground(hex("#bcbec4"))
	word := base.Foreground(hex("#ffffff"))
	return Theme{
		Header:         word.Background(hex("#2b2d30")).Bold(true),
		Footer:         base.Foreground(hex("#f0f0f0")).Background(hex("#2b2d30")),
		LineNumber:     base.Foreground(hex("#4e5157")),
		Equal:          text,
		Added:          word.Background(hex("#294436")),
		Deleted:        word.Background(hex("#4a3a3c")),
		Modified:       word.Background(hex("#2d3f5c")),
		Filler:         base.Background(hex("#26282b")),
		AddedWord:      word.Background(hex("#3b7a55")),
		DeletedWord:    word.Background(hex("#7d4a50")),
		ModifiedWord:   word.Background(hex("#3f67a5")),
		Conflict:       word.Background(hex("#4f2f3a")),
		ConflictWord:   word.Background(hex("#8c4256")),
		Gutter:         base.Foreground(hex("#4e5157")),
		GutterCur:      base.Foreground(hex("#56a8f5")).Bold(true),
		Selected:       word.Background(hex("#2f4d78")),
		SelectedDim:    word.Background(hex("#3a3d41")),
		StatusAdded:    base.Foreground(hex("#6aab73")).Bold(true),
		StatusDeleted:  base.Foreground(hex("#c75450")).Bold(true),
		StatusModified: base.Foreground(hex("#56a8f5")).Bold(true),
		Jelly:          base.Foreground(hex("#e0679b")),
		Ours:           word.Background(hex("#2d3f5c")),
		Theirs:         word.Background(hex("#23423f")),
		Warning:        base.Foreground(hex("#e5a550")).Bold(true),
		ConflictMark:   base.Foreground(hex("#e0679b")).Bold(true),
		painter:        newPainter(darkSyntaxStyle),
	}
}

// LightTheme returns a palette for light terminal backgrounds.
func LightTheme() Theme { return LightThemeFor(lipgloss.DefaultRenderer()) }

// LightThemeFor is LightTheme with styles rendered by r.
func LightThemeFor(r *lipgloss.Renderer) Theme {
	base := r.NewStyle()
	text := base.Foreground(hex("#1e1f22"))
	return Theme{
		Header:         base.Foreground(hex("#1e1f22")).Background(hex("#dfe1e5")).Bold(true),
		Footer:         base.Foreground(hex("#6c707e")).Background(hex("#dfe1e5")),
		LineNumber:     base.Foreground(hex("#8c909e")),
		Equal:          text,
		Added:          text.Background(hex("#c6f0cf")),
		Deleted:        text.Background(hex("#f2d0d0")),
		Modified:       text.Background(hex("#cfdcf7")),
		Filler:         base.Background(hex("#ececec")),
		AddedWord:      text.Background(hex("#8fd9a0")),
		DeletedWord:    text.Background(hex("#e8a0a0")),
		ModifiedWord:   text.Background(hex("#a3bef0")),
		Conflict:       text.Background(hex("#f4cdd5")),
		ConflictWord:   text.Background(hex("#e59aab")),
		Gutter:         base.Foreground(hex("#8c909e")),
		GutterCur:      base.Foreground(hex("#2470b3")).Bold(true),
		Selected:       text.Background(hex("#a6c8ff")),
		SelectedDim:    text.Background(hex("#dfe1e5")),
		StatusAdded:    base.Foreground(hex("#1a7f37")).Bold(true),
		StatusDeleted:  base.Foreground(hex("#c62828")).Bold(true),
		StatusModified: base.Foreground(hex("#1565c0")).Bold(true),
		Jelly:          base.Foreground(hex("#d63384")),
		Ours:           text.Background(hex("#cfdcf7")),
		Theirs:         text.Background(hex("#c2e8e2")),
		Warning:        base.Foreground(hex("#9a5b00")).Bold(true),
		ConflictMark:   base.Foreground(hex("#c2185b")).Bold(true),
		painter:        newPainter(lightSyntaxStyle),
	}
}

// MonoTheme uses only text attributes (bold, strikethrough, reverse, underline), for NO_COLOR and monochrome
// terminals. Added text is bold, deleted text is struck through, changed words are reversed.
func MonoTheme() Theme { return MonoThemeFor(lipgloss.DefaultRenderer()) }

// MonoThemeFor is MonoTheme with styles rendered by r.
func MonoThemeFor(r *lipgloss.Renderer) Theme {
	base := r.NewStyle()
	return Theme{
		Header:         base.Reverse(true).Bold(true),
		Footer:         base.Reverse(true),
		LineNumber:     base.Faint(true),
		Equal:          base,
		Added:          base.Bold(true),
		Deleted:        base.Strikethrough(true),
		Modified:       base,
		Filler:         base.Faint(true),
		AddedWord:      base.Bold(true).Reverse(true),
		DeletedWord:    base.Strikethrough(true).Reverse(true),
		ModifiedWord:   base.Reverse(true),
		Conflict:       base.Bold(true).Underline(true),
		ConflictWord:   base.Bold(true).Underline(true).Reverse(true),
		Gutter:         base.Faint(true),
		GutterCur:      base.Bold(true),
		Selected:       base.Reverse(true),
		SelectedDim:    base.Underline(true),
		StatusAdded:    base.Bold(true),
		StatusDeleted:  base.Strikethrough(true),
		StatusModified: base.Bold(true),
		Jelly:          base.Bold(true),
		Ours:           base,
		Theirs:         base,
		Warning:        base.Bold(true),
		ConflictMark:   base.Bold(true).Underline(true),
	}
}

// statusStyle returns the style of a file status letter in the change list.
func (t Theme) statusStyle(status byte) lipgloss.Style {
	switch status {
	case source.StatusAdded, source.StatusUntracked:
		return t.StatusAdded
	case source.StatusDeleted:
		return t.StatusDeleted
	default:
		return t.StatusModified
	}
}

// WithTheme returns the model drawn with the given theme.
func (m DiffModel) WithTheme(theme Theme) DiffModel {
	m.theme = theme
	return m
}

// WithTheme returns the model drawn with the given theme.
func (m GitModel) WithTheme(theme Theme) GitModel {
	m.theme = theme
	m.diff = m.diff.WithTheme(theme)
	return m
}

// WithTheme returns the model drawn with the given theme.
func (m MergeModel) WithTheme(theme Theme) MergeModel {
	m.theme = theme
	return m
}

// WithTheme returns the model drawn with the given theme; merge viewers opened later use it too.
func (m DirMergeModel) WithTheme(theme Theme) DirMergeModel {
	m.theme = theme
	return m
}

// Themed applies theme to any of the jelly-dev models and returns other models unchanged.
func Themed(model tea.Model, theme Theme) tea.Model {
	switch typed := model.(type) {
	case DiffModel:
		return typed.WithTheme(theme)
	case GitModel:
		return typed.WithTheme(theme)
	case MergeModel:
		return typed.WithTheme(theme)
	case DirMergeModel:
		return typed.WithTheme(theme)
	case EditorModel:
		return typed.WithTheme(theme)
	case ProjectModel:
		return typed.WithTheme(theme)
	case WelcomeModel:
		return typed.WithTheme(theme)
	}
	return model
}
