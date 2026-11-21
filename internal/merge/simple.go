package merge

import (
	"regexp"
	"strings"
)

// lineBreakToken ends every line in the token stream. It must not be "\n": diff.Lines joins its input with
// newlines. Lines containing it (or a carriage return, which diff.Lines would swallow) are not merged by word.
const lineBreakToken = "\x00"

// tokenPattern splits a line into runs of word characters, runs of whitespace and single other characters.
var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}_]+|\s+|.`)

// ResolveSimple settles a conflict chunk that is not a real conflict and returns the merged lines.
// ok is false when a person has to decide. It tries, in order:
//  1. one side differs from the base only in whitespace: take the other side;
//  2. both sides are equal ignoring whitespace: take ours;
//  3. both sides edited different words: merge them word by word, without any word-level conflict.
//
// Chunks that are not conflicts are returned as they are. A conflict chunk that had shared edge lines
// trimmed carries a base that does not line up with its sides; this only makes the function give up
// more often, never merge wrongly.
func ResolveSimple(c Chunk) (lines []string, ok bool) {
	if resolved, done := c.Resolved(); done {
		return resolved, true
	}
	switch {
	case sameIgnoringSpace(c.Ours, c.Base):
		return c.Theirs, true
	case sameIgnoringSpace(c.Theirs, c.Base):
		return c.Ours, true
	case sameIgnoringSpace(c.Ours, c.Theirs):
		return c.Ours, true
	}
	return mergeWords(c)
}

// mergeWords runs the three-way merge on words instead of lines.
func mergeWords(c Chunk) ([]string, bool) {
	if len(c.Base) == 0 {
		return nil, false // both sides added text: there is no common word to anchor on
	}
	for _, side := range [][]string{c.Base, c.Ours, c.Theirs} {
		for _, line := range side {
			if strings.ContainsAny(line, lineBreakToken+"\r") {
				return nil, false
			}
		}
	}
	chunks := Merge(tokensOf(c.Base), tokensOf(c.Ours), tokensOf(c.Theirs))
	if HasConflicts(chunks) {
		return nil, false
	}
	return linesOf(Assemble(chunks, nil)), true
}

func sameIgnoringSpace(a, b []string) bool {
	return strings.Join(strings.Fields(strings.Join(a, " ")), " ") == strings.Join(strings.Fields(strings.Join(b, " ")), " ")
}

// tokensOf splits lines into words, whitespace runs and single characters; each line ends with a break token.
func tokensOf(lines []string) []string {
	var tokens []string
	for _, line := range lines {
		tokens = append(tokens, tokenPattern.FindAllString(line, -1)...)
		tokens = append(tokens, lineBreakToken)
	}
	return tokens
}

// linesOf is the inverse of tokensOf.
func linesOf(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	text := strings.Join(tokens, "")
	return strings.Split(strings.TrimSuffix(text, lineBreakToken), lineBreakToken)
}
