package ui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/eutima/jelly-dev/internal/config"
)

// rendererFor returns a renderer with its own color profile. Themes built from it emit escape sequences
// without changing the process-wide profile, so tests stay independent and parallel-safe.
func rendererFor(profile termenv.Profile) *lipgloss.Renderer {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(profile)
	return renderer
}

func trueColorTheme(t *testing.T, name string) Theme {
	t.Helper()
	theme, ok := NamedThemeFor(rendererFor(termenv.TrueColor), name)
	if !ok {
		t.Fatalf("unknown theme %q", name)
	}
	return theme
}

func sizedDiff(theme Theme) DiffModel {
	model := NewDiffModel("a", "b", []string{"let foo = 1"}, []string{"let bar = 1"}).WithTheme(theme)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 6})
	return updated.(DiffModel)
}

func TestNamedTheme_ReturnsThemes_WhenNameKnown(t *testing.T) {
	for _, name := range []string{config.ThemeDark, config.ThemeLight, config.ThemeMono} {
		if _, ok := NamedTheme(name); !ok {
			t.Fatalf("theme %s missing", name)
		}
	}
	if _, ok := NamedTheme("neon"); ok {
		t.Fatal("unknown theme must not resolve")
	}
}

func TestMonoTheme_UsesNoColorsButAttributes_WhenRendered(t *testing.T) {
	t.Parallel()
	view := sizedDiff(trueColorTheme(t, config.ThemeMono)).View()
	if strings.Contains(view, "38;2") || strings.Contains(view, "48;2") || strings.Contains(view, "38;5") {
		t.Fatalf("mono theme emitted colors: %q", view)
	}
	if !strings.Contains(view, "\x1b[7m") && !strings.Contains(view, ";7m") && !strings.Contains(view, "[7;") {
		t.Fatalf("mono theme must mark changed words with reverse video: %q", view)
	}
}

func TestDarkAndLightThemes_EmitDifferentColors_WhenRendered(t *testing.T) {
	t.Parallel()
	dark, light := sizedDiff(trueColorTheme(t, config.ThemeDark)).View(), sizedDiff(trueColorTheme(t, config.ThemeLight)).View()
	if !strings.Contains(dark, "48;2") || !strings.Contains(light, "48;2") || dark == light {
		t.Fatalf("themes must emit distinct background colors")
	}
}

func TestThemed_AppliesThemeToAllModels_WhenWrapped(t *testing.T) {
	mono := MonoTheme()
	diffModel := Themed(NewDiffModel("a", "b", nil, nil), mono).(DiffModel)
	gitModel := Themed(NewGitModel(nil, &fakeSource{}), mono).(GitModel)
	mergeModel, _ := NewMergeModel(MergeTitles{}, nil, nil)
	themedMerge := Themed(mergeModel, mono).(MergeModel)
	if !diffModel.theme.Selected.GetReverse() || !gitModel.theme.Selected.GetReverse() ||
		!themedMerge.theme.Selected.GetReverse() {
		t.Fatal("theme not applied")
	}
}

func TestGitView_MarksSelectedRowWithoutColor_WhenMonoTheme(t *testing.T) {
	t.Parallel()
	model, _ := newGitTestModel(100, 12)
	view := model.WithTheme(trueColorTheme(t, config.ThemeMono)).View()
	if strings.Contains(view, "48;2") || !strings.Contains(view, "7m") {
		t.Fatalf("selected row must use reverse video only: %q", view)
	}
}

func TestMonoTheme_ShowsNothingExtra_WhenProfileIsAscii(t *testing.T) {
	t.Parallel()
	// Documents why the CLI must raise the profile for NO_COLOR: in the Ascii profile lipgloss drops attributes.
	ascii, _ := NamedThemeFor(rendererFor(termenv.Ascii), config.ThemeMono)
	if view := sizedDiff(ascii).View(); strings.Contains(view, "\x1b[") {
		t.Fatalf("expected plain text, got %q", view)
	}
	ansi, _ := NamedThemeFor(rendererFor(termenv.ANSI), config.ThemeMono)
	if view := sizedDiff(ansi).View(); !strings.Contains(view, "7m") {
		t.Fatalf("expected reverse video with the ANSI profile, got %q", view)
	}
}

func TestMonoTheme_MarksDeletionsAsStruckThrough_WhenRendered(t *testing.T) {
	t.Parallel()
	ansi, _ := NamedThemeFor(rendererFor(termenv.ANSI), config.ThemeMono)
	model := NewDiffModel("a", "b", []string{"gone"}, nil).WithTheme(ansi)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	if view := updated.(DiffModel).View(); !strings.Contains(view, "9m") {
		t.Fatalf("expected strikethrough: %q", view)
	}
}

// On terminals with a small palette the colours are mapped to the nearest of 16, and grey text on a coloured bar or on a
// highlighted row became unreadable: everything that is highlighted in the dark theme has white (or near white) text.
func TestDarkTheme_HighlightedTextIsWhite(t *testing.T) {
	theme := DarkTheme()
	for name, style := range map[string]lipgloss.Style{
		"header": theme.Header, "selected": theme.Selected, "selected (not focused)": theme.SelectedDim, "footer": theme.Footer,
	} {
		got, ok := style.GetForeground().(lipgloss.Color)
		if !ok || (got != "#ffffff" && got != "#f0f0f0") {
			t.Errorf("%s text must be white, it is %v", name, style.GetForeground())
		}
	}
}

func TestTopBar_ShortcutHintsAreNotFaint(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	for _, part := range model.topBar().parts {
		if part.style.GetFaint() {
			t.Fatalf("the hint %q on the blue bar must not be faint", part.text)
		}
	}
}
