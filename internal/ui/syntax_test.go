package ui

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/source"
)

func TestHighlightLines_ReturnsKeywordSpan_WhenGoSource(t *testing.T) {
	spans := highlightLines([]string{"main.go"}, []string{"package main", "", "func run() {}"})
	if len(spans) != 3 {
		t.Fatalf("spans for %d lines", len(spans))
	}
	var keyword *SyntaxSpan
	for i := range spans[2] {
		if spans[2][i].Token == chroma.Keyword || spans[2][i].Token.InCategory(chroma.Keyword) {
			keyword = &spans[2][i]
		}
	}
	if keyword == nil || keyword.Start != 0 || keyword.End != 4 {
		t.Fatalf("expected keyword span 0-4 for 'func', got %+v", spans[2])
	}
}

func TestHighlightLines_KeepsOffsetsInsideLines_WhenCommentSpansLines(t *testing.T) {
	lines := []string{"/* first", "   second */", "x := 1"}
	spans := highlightLines([]string{"a.go"}, lines)
	for i, lineSpans := range spans {
		for _, span := range lineSpans {
			if span.Start < 0 || span.End > len(lines[i]) || span.Start >= span.End {
				t.Fatalf("line %d: span %+v outside %q", i, span, lines[i])
			}
		}
	}
	if len(spans[0]) == 0 || len(spans[1]) == 0 {
		t.Fatalf("both comment lines must be colored: %+v", spans)
	}
}

func TestHighlightLines_ReturnsNil_WhenNoLexerOrTooLarge(t *testing.T) {
	if spans := highlightLines([]string{"notes.unknownext123"}, []string{"text"}); spans != nil {
		t.Fatalf("unknown extension must not be highlighted: %+v", spans)
	}
	if spans := highlightLines(nil, []string{"text"}); spans != nil {
		t.Fatal("no file name must not be highlighted")
	}
	huge := []string{strings.Repeat("x", maxHighlightBytes+1)}
	if spans := highlightLines([]string{"a.go"}, huge); spans != nil {
		t.Fatal("too large text must not be highlighted")
	}
}

func TestHighlightLines_UsesFirstFileNameWithLexer_WhenSeveralGiven(t *testing.T) {
	if spans := highlightLines([]string{"", "x.unknownext123", "b.go"}, []string{"func f() {}"}); spans == nil {
		t.Fatal("expected the .go name to be used")
	}
}

func TestWithHighlight_AssignsSpansByLineNumber_WhenGoFilesDiffer(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"package a", "var x = 1"}, []string{"package a", "var y = 2"}).
		WithHighlight("f.go")
	if len(model.rows[0].Left.Syntax) == 0 || len(model.rows[1].Right.Syntax) == 0 {
		t.Fatalf("rows lack syntax spans: %+v", model.rows)
	}
	if len(model.unified) == 0 || len(model.unified[0].Syntax) == 0 {
		t.Fatalf("unified rows lack syntax spans: %+v", model.unified)
	}
	plain := NewDiffModel("a", "b", []string{"package a"}, []string{"package a"})
	if len(plain.rows[0].Left.Syntax) != 0 {
		t.Fatal("rows must stay unhighlighted without WithHighlight")
	}
}

func TestWithHighlight_ReturnsModelUnchanged_WhenNoLexerMatches(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"x"}, []string{"y"}).WithHighlight("README.unknownext123")
	if len(model.rows[0].Left.Syntax) != 0 {
		t.Fatal("unexpected spans")
	}
}

func syntaxView(theme Theme, width int) string {
	model := NewDiffModel("a", "b", []string{"func old() {}"}, []string{"func newer() {}"}).
		WithTheme(theme).WithHighlight("f.go")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 6})
	return updated.(DiffModel).View()
}

func TestView_ColorsKeywords_WhenDarkThemeAndGoFile(t *testing.T) {
	t.Parallel()
	highlighted := syntaxView(trueColorTheme(t, config.ThemeDark), 80)
	plain := NewDiffModel("a", "b", []string{"func old() {}"}, []string{"func newer() {}"}).WithTheme(trueColorTheme(t, config.ThemeDark))
	updated, _ := plain.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	if highlighted == updated.(DiffModel).View() {
		t.Fatal("syntax highlighting must change the output")
	}
}

func TestView_UsesDifferentSyntaxColors_WhenLightTheme(t *testing.T) {
	t.Parallel()
	if syntaxView(trueColorTheme(t, config.ThemeDark), 80) == syntaxView(trueColorTheme(t, config.ThemeLight), 80) {
		t.Fatal("dark and light syntax output must differ")
	}
}

func TestView_HasNoSyntaxColors_WhenMonoTheme(t *testing.T) {
	t.Parallel()
	if view := syntaxView(trueColorTheme(t, config.ThemeMono), 80); strings.Contains(view, "38;2") {
		t.Fatalf("mono theme must not emit colors: %q", view)
	}
}

func TestView_FitsWidthWithSyntax_WhenSizesVary(t *testing.T) {
	t.Parallel()
	for _, width := range []int{20, 41, 80} {
		for i, line := range strings.Split(syntaxView(trueColorTheme(t, config.ThemeDark), width), "\n") {
			if line != "" && lipgloss.Width(line) > width {
				t.Fatalf("width %d line %d is %d wide", width, i, lipgloss.Width(line))
			}
		}
	}
}

func TestGitView_HighlightsSelectedFile_WhenPathHasLexer(t *testing.T) {
	src := &fakeSource{texts: map[string][2]string{"main.go": {"func a() {}\n", "func b() {}\n"}}}
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "main.go"}}
	updated, _ := quietGit(files, src).Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	model := settle(updated.(GitModel))
	if len(model.diff.rows) == 0 || len(model.diff.rows[0].Left.Syntax) == 0 {
		t.Fatalf("git diff rows lack syntax spans: %+v", model.diff.rows)
	}
}

func TestPaintFor_KeepsThemeColor_WhenPlainTextOrNoPainter(t *testing.T) {
	var none *syntaxPainter
	if none.paintFor(chroma.Keyword) != (paint{}) {
		t.Fatal("nil painter must not paint")
	}
	painter := newPainter(darkSyntaxStyle)
	for _, token := range []chroma.TokenType{chroma.Text, chroma.TextWhitespace, chroma.Other} {
		if painter.paintFor(token) != (paint{}) {
			t.Fatalf("token %v must keep the theme color", token)
		}
	}
	if painter.paintFor(chroma.Keyword).color == "" {
		t.Fatal("keywords must be colored")
	}
}

func TestHighlightLines_KeepsLaterLinesAligned_WhenLineContainsLoneCarriageReturn(t *testing.T) {
	lines := []string{"x := 1\ry := 2", "func f() {}"}
	spans := highlightLines([]string{"a.go"}, lines)
	if len(spans) != 2 {
		t.Fatalf("spans for %d lines", len(spans))
	}
	found := false
	for _, span := range spans[1] {
		if span.Start == 0 && span.End == 4 && span.Token.InCategory(chroma.Keyword) {
			found = true // 'func' on the second line, not shifted by the "\r"
		}
	}
	if !found {
		t.Fatalf("second line lost its keyword span: %+v", spans[1])
	}
}

func TestRenderSyntaxText_KeepsWordStyle_WhenChangedWordIsAlsoKeyword(t *testing.T) {
	t.Parallel()
	theme := trueColorTheme(t, config.ThemeDark)
	spans := highlightLines([]string{"a.go"}, []string{"func f() {}"})
	word := theme.ModifiedWord
	out := renderSyntaxText("func f() {}", []diff.Span{{Start: 0, End: 4}}, spans[0], theme.painter, 20, theme.Modified, word)
	if !strings.Contains(out, "48;2;63;103;165") {
		t.Fatalf("changed word must keep its background: %q", out)
	}
	if strings.Contains(out, "38;2;249;38;113") {
		t.Fatalf("keyword color must not override the word style: %q", out)
	}
}

func TestSyntaxStyles_Exist_WhenThemesLoad(t *testing.T) {
	for _, name := range []string{darkSyntaxStyle, lightSyntaxStyle} {
		if styles.Get(name) == styles.Fallback || styles.Registry[name] == nil {
			t.Fatalf("chroma style %q is missing", name)
		}
	}
}
