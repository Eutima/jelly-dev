package vimbuf

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// search is the state of the vim search: the last pattern, its direction and whether its matches are highlighted.
type search struct {
	pattern   string
	re        *regexp.Regexp
	backwards bool
	highlight bool
}

// CommandPrefix is the character shown before the text of the command line: ":" for a command, "/" or "?" for a search.
func (b *Buffer) CommandPrefix() string {
	if b.cmdPrefix == '/' || b.cmdPrefix == '?' {
		return string(b.cmdPrefix)
	}
	return ":"
}

// startSearch opens the command line for a search forwards ("/") or backwards ("?").
func (b *Buffer) startSearch(prefix rune) {
	b.mode, b.cmd, b.cmdPrefix = Command, "", prefix
	b.resetPending()
}

// compile turns a typed pattern into a regular expression; a pattern that is no valid expression is searched literally.
// The flag \c at the start makes it case-insensitive.
func compile(pattern string) *regexp.Regexp {
	prefix := ""
	if strings.HasPrefix(pattern, `\c`) {
		pattern, prefix = pattern[2:], "(?i)"
	}
	if re, err := regexp.Compile(prefix + pattern); err == nil {
		return re
	}
	return regexp.MustCompile(prefix + regexp.QuoteMeta(pattern))
}

// finishSearch runs the search typed on the command line.
func (b *Buffer) finishSearch(prefix rune, text string) {
	if text == "" {
		if b.find.re == nil {
			b.message = "E35: No previous regular expression"
			return
		}
		text = b.find.pattern
	}
	b.find = search{pattern: text, re: compile(text), backwards: prefix == '?', highlight: true}
	b.jumpToMatch(false)
}

// RepeatSearch jumps to the next match in the direction of the last search (n), or against it (N).
func (b *Buffer) RepeatSearch(reverse bool) {
	if b.find.re == nil {
		b.message = "E35: No previous regular expression"
		return
	}
	b.find.highlight = true
	b.jumpToMatch(reverse)
}

// ClearHighlight stops highlighting the matches of the last search (:noh).
func (b *Buffer) ClearHighlight() { b.find.highlight = false }

// Matches returns the rune columns [from, to) of the matches in line row; none unless the last search is highlighted.
func (b *Buffer) Matches(row int) [][2]int {
	if !b.find.highlight || b.find.re == nil || row < 0 || row >= len(b.lines) {
		return nil
	}
	return matchesIn(b.find.re, string(b.lines[row]))
}

// matchesIn returns the matches of re in text as rune ranges; empty matches are left out.
func matchesIn(re *regexp.Regexp, text string) [][2]int {
	var out [][2]int
	for _, loc := range re.FindAllStringIndex(text, -1) {
		if loc[1] > loc[0] {
			out = append(out, [2]int{utf8.RuneCountInString(text[:loc[0]]), utf8.RuneCountInString(text[:loc[1]])})
		}
	}
	return out
}

// jumpToMatch moves the cursor to the next match after (before) it, wrapping around the end of the text.
func (b *Buffer) jumpToMatch(reverse bool) {
	backwards := b.find.backwards != reverse
	lines := len(b.lines)
	for step := 0; step <= lines; step++ { // the cursor's own line is visited twice: after the cursor, then before it
		offset := step
		if backwards {
			offset = -step
		}
		row := ((b.row+offset)%lines + lines) % lines
		matches := matchesIn(b.find.re, string(b.lines[row]))
		col, ok := pick(matches, row == b.row, step == 0, step == lines, b.col, backwards)
		if !ok {
			continue
		}
		wrapped := (!backwards && row < b.row) || (backwards && row > b.row) || (row == b.row && step == lines)
		b.row, b.col = row, col
		prefix := "/"
		if b.find.backwards {
			prefix = "?"
		}
		b.message = prefix + b.find.pattern
		if wrapped {
			b.message = "search hit BOTTOM, continuing at TOP"
			if backwards {
				b.message = "search hit TOP, continuing at BOTTOM"
			}
		}
		return
	}
	b.message = fmt.Sprintf("E486: Pattern not found: %s", b.find.pattern)
}

// pick chooses the match of a line: on the cursor's line the first one after the cursor (the last one before it when
// searching backwards) at the first visit, and the other half of the line when the search came around to it again.
func pick(matches [][2]int, cursorLine, first, last bool, col int, backwards bool) (int, bool) {
	if len(matches) == 0 {
		return 0, false
	}
	switch {
	case !cursorLine:
		if backwards {
			return matches[len(matches)-1][0], true
		}
		return matches[0][0], true
	case backwards && first:
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i][0] < col {
				return matches[i][0], true
			}
		}
	case !backwards && first:
		for _, m := range matches {
			if m[0] > col {
				return m[0], true
			}
		}
	case last: // all the way around: the cursor's own line again, now from its other end
		if backwards {
			return matches[len(matches)-1][0], true
		}
		return matches[0][0], true
	}
	return 0, false
}
