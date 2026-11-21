package ui

import (
	"io"
	"math"
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

func TestDetectThemeName_ChoosesByTheNumberOfColoursAndTheBackground(t *testing.T) {
	tests := []struct {
		profile termenv.Profile
		dark    bool
		want    string
	}{
		{termenv.TrueColor, true, config.ThemeDark},
		{termenv.TrueColor, false, config.ThemeLight},
		{termenv.ANSI256, true, config.ThemeDark},
		{termenv.ANSI256, false, config.ThemeLight},
		{termenv.ANSI, true, config.ThemeANSI}, // 16 colours: fixed colours would be mapped to the terminal's palette
		{termenv.ANSI, false, config.ThemeANSI},
		{termenv.Ascii, true, config.ThemeMono},
		{termenv.Ascii, false, config.ThemeMono},
	}
	for _, tt := range tests {
		if got := DetectThemeName(tt.profile, tt.dark); got != tt.want {
			t.Errorf("profile %v dark %v: %s, want %s", tt.profile, tt.dark, got, tt.want)
		}
	}
}

func TestNamedThemeFor_AutoFollowsTheColourProfileOfTheRenderer(t *testing.T) {
	sixteen, ok := NamedThemeFor(rendererFor(termenv.ANSI), config.ThemeAuto)
	if !ok || !sixteen.Header.GetReverse() || sixteen.painter != nil {
		t.Fatalf("auto on a 16 colour terminal is the ANSI theme (reverse header, no syntax colours): ok %v", ok)
	}
	none, _ := NamedThemeFor(rendererFor(termenv.Ascii), config.ThemeAuto)
	if none.Added.GetBold() != true || none.Deleted.GetStrikethrough() != true {
		t.Fatal("auto without colours is the mono theme")
	}
	truecolor, _ := NamedThemeFor(rendererFor(termenv.TrueColor), config.ThemeAuto)
	if truecolor.painter == nil {
		t.Fatal("auto on a true colour terminal has syntax colours")
	}
	for _, name := range []string{config.ThemeANSI, config.ThemeContrast} {
		if _, ok := NamedThemeFor(rendererFor(termenv.TrueColor), name); !ok {
			t.Errorf("%s is a theme", name)
		}
	}
}

// luminance and contrast follow WCAG: text needs a ratio of 4.5 against its background.
func luminance(hex string) float64 {
	channel := func(i int) float64 {
		var v int
		for _, c := range hex[1+2*i : 3+2*i] {
			v = v*16 + strings.IndexRune("0123456789abcdef", c)
		}
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(0) + 0.7152*channel(1) + 0.0722*channel(2)
}

func contrast(a, b string) float64 {
	la, lb := luminance(strings.ToLower(a)), luminance(strings.ToLower(b))
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// themeStyles lists the styles of a theme by name.
func themeStyles(theme Theme) map[string]lipgloss.Style {
	return map[string]lipgloss.Style{
		"header": theme.Header, "footer": theme.Footer, "lineNumber": theme.LineNumber, "equal": theme.Equal, "added": theme.Added,
		"deleted": theme.Deleted, "modified": theme.Modified, "filler": theme.Filler, "addedWord": theme.AddedWord,
		"deletedWord": theme.DeletedWord, "modifiedWord": theme.ModifiedWord, "conflict": theme.Conflict, "conflictWord": theme.ConflictWord,
		"gutter": theme.Gutter, "gutterCur": theme.GutterCur, "selected": theme.Selected, "selectedDim": theme.SelectedDim,
		"statusAdded": theme.StatusAdded, "statusDeleted": theme.StatusDeleted, "statusModified": theme.StatusModified, "jelly": theme.Jelly,
		"ours": theme.Ours, "theirs": theme.Theirs, "warning": theme.Warning, "conflictMark": theme.ConflictMark,
	}
}

// Text that is placed on a coloured background must be readable on it: every style of the colour themes that sets both a text colour
// and a background has a contrast of at least 4.5. (Line numbers and gutters are decoration and need 3.)
func TestThemes_TextOnAColouredBackgroundIsReadable(t *testing.T) {
	for _, name := range []string{config.ThemeDark, config.ThemeLight} {
		for style, s := range themeStyles(trueColorTheme(t, name)) {
			fg, bg := s.GetForeground(), s.GetBackground()
			f, fok := fg.(lipgloss.Color)
			b, bok := bg.(lipgloss.Color)
			if !fok || !bok {
				continue
			}
			need := 4.5
			if style == "lineNumber" || style == "gutter" || style == "filler" {
				need = 3
			}
			if got := contrast(string(f), string(b)); got < need {
				t.Errorf("%s theme, %s: %s on %s has a contrast of %.1f, below %.1f", name, style, f, b, got, need)
			}
		}
	}
}

// The themes for terminals with few colours set no fixed background together with a text colour: what the terminal's palette makes of
// such a pair is not known, it can be grey on grey. The contrast theme has nothing dim either.
func TestThemes_ANSIAndContrastSetNoColourPairs(t *testing.T) {
	for _, name := range []string{config.ThemeANSI, config.ThemeContrast} {
		theme, _ := NamedThemeFor(rendererFor(termenv.ANSI), name)
		for style, s := range themeStyles(theme) {
			if _, noBackground := s.GetBackground().(lipgloss.NoColor); !noBackground {
				t.Errorf("%s theme, %s: sets a background colour %v", name, style, s.GetBackground())
			}
			if name == config.ThemeContrast && s.GetFaint() {
				t.Errorf("contrast theme, %s: dim text", style)
			}
		}
		if !theme.Selected.GetReverse() || !theme.Header.GetReverse() || !theme.Warning.GetReverse() {
			t.Errorf("%s theme: what is highlighted is drawn in reverse video", name)
		}
	}
}

func TestContrastTheme_DeletedLinesAreStruckThroughNotJustRed(t *testing.T) {
	theme, _ := NamedThemeFor(rendererFor(termenv.ANSI), config.ThemeContrast)
	if !theme.Deleted.GetStrikethrough() || !theme.Deleted.GetBold() || theme.Added.GetStrikethrough() {
		t.Fatal("deleted lines are bold and struck through, added lines are not: they differ in more than the colour")
	}
}
