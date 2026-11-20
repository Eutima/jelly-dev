package ui

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const (
	// maxHighlightBytes bounds the text that is run through the lexer; bigger files stay unhighlighted.
	maxHighlightBytes = 512 << 10
	darkSyntaxStyle   = "monokai"
	lightSyntaxStyle  = "github"
)

// SyntaxSpan colors the bytes [Start, End) of a line as one kind of token.
type SyntaxSpan struct {
	Start int
	End   int
	Token chroma.TokenType
}

// paint is the foreground a token gets in a theme.
type paint struct {
	color string // "#rrggbb", empty for the theme's normal text color
	bold  bool
}

// syntaxPainter maps token types to colors of one chroma style.
type syntaxPainter struct {
	style *chroma.Style
}

// newPainter returns a painter for a chroma style name, or nil for "" (no highlighting).
func newPainter(styleName string) *syntaxPainter {
	if styleName == "" {
		return nil
	}
	return &syntaxPainter{style: styles.Get(styleName)}
}

// paintFor returns the foreground of a token; plain text keeps the theme color.
func (p *syntaxPainter) paintFor(token chroma.TokenType) paint {
	if p == nil || token == chroma.Text || token == chroma.TextWhitespace || token == chroma.Other {
		return paint{}
	}
	entry := p.style.Get(token)
	result := paint{bold: entry.Bold == chroma.Yes}
	if entry.Colour.IsSet() {
		result.color = entry.Colour.String()
	}
	return result
}

// highlightLines tokenizes lines with the lexer for the first filename that has one and returns the
// token spans of every line. It returns nil when there is no lexer, the text is too large, or lexing fails.
func highlightLines(filenames []string, lines []string) [][]SyntaxSpan {
	lexer := lexerFor(filenames)
	if lexer == nil || len(lines) == 0 {
		return nil
	}
	// chroma turns a lone "\r" into a line break, which would shift every later line; a space keeps the
	// byte offsets and the line count.
	text := strings.ReplaceAll(strings.Join(lines, "\n"), "\r", " ")
	if len(text) > maxHighlightBytes {
		return nil
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil
	}
	spans := make([][]SyntaxSpan, len(lines))
	line, column := 0, 0
	for _, token := range iterator.Tokens() {
		rest := token.Value
		for rest != "" && line < len(lines) {
			part, after, hasNewline := strings.Cut(rest, "\n")
			if end := min(column+len(part), len(lines[line])); part != "" && column < end {
				spans[line] = append(spans[line], SyntaxSpan{Start: column, End: end, Token: token.Type})
			}
			if !hasNewline {
				column += len(part)
				break
			}
			line, column = line+1, 0
			rest = after
		}
	}
	return spans
}

func lexerFor(filenames []string) chroma.Lexer {
	for _, name := range filenames {
		if name == "" {
			continue
		}
		if lexer := lexers.Match(filepath.Base(name)); lexer != nil {
			return lexer
		}
	}
	return nil
}

// WithHighlight returns the model with syntax highlighting; the lexer is chosen by the first file name
// that has one. Without a lexer, or for very large files, the model is returned unchanged.
func (m DiffModel) WithHighlight(filenames ...string) DiffModel {
	oldSpans := highlightLines(filenames, m.oldLines)
	newSpans := highlightLines(filenames, m.newLines)
	if oldSpans == nil && newSpans == nil {
		return m
	}
	m.rows = append([]Row(nil), m.rows...)
	for i := range m.rows {
		m.rows[i].Left.Syntax = lineSpans(oldSpans, m.rows[i].Left.No)
		m.rows[i].Right.Syntax = lineSpans(newSpans, m.rows[i].Right.No)
	}
	m.unified = append([]UnifiedRow(nil), m.unified...)
	for i := range m.unified {
		row := &m.unified[i]
		if row.Kind == CellAdded {
			row.Syntax = lineSpans(newSpans, row.NewNo)
		} else {
			row.Syntax = lineSpans(oldSpans, row.OldNo)
		}
	}
	return m
}

// lineSpans returns the spans of the one-based line number, or nil.
func lineSpans(spans [][]SyntaxSpan, number int) []SyntaxSpan {
	if number < 1 || number > len(spans) {
		return nil
	}
	return spans[number-1]
}
