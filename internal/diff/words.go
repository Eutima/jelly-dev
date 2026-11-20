package diff

import (
	"unicode"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	// maxTokens caps the tokens per line; longer lines (minified files) are reported as fully changed.
	maxTokens = 100000
	// firstSurrogate and surrogateCount describe the rune range that cannot be used as token ids.
	firstSurrogate = 0xD800
	surrogateCount = 0x800
)

const (
	classOther = iota
	classWord
	classSpace
)

// Span is a changed byte range [Start, End) inside a line.
type Span struct {
	Start int
	End   int
}

// Words returns the changed byte ranges of oldLine and newLine, compared token by token
// (words, whitespace runs and single punctuation characters).
func Words(oldLine, newLine string) (oldSpans, newSpans []Span) {
	oldTokens := tokenize(oldLine)
	newTokens := tokenize(newLine)
	if len(oldTokens) > maxTokens || len(newTokens) > maxTokens {
		return wholeLine(oldLine), wholeLine(newLine)
	}

	ids := map[string]rune{}
	encode := func(tokens []string) string {
		runes := make([]rune, len(tokens))
		for i, token := range tokens {
			id, ok := ids[token]
			if !ok {
				id = tokenID(len(ids))
				ids[token] = id
			}
			runes[i] = id
		}
		return string(runes)
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(encode(oldTokens), encode(newTokens), false)

	var oldTok, newTok, oldOff, newOff int
	for _, d := range diffs {
		count := utf8.RuneCountInString(d.Text)
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			oldOff += byteLen(oldTokens[oldTok : oldTok+count])
			newOff += byteLen(newTokens[newTok : newTok+count])
			oldTok += count
			newTok += count
		case diffmatchpatch.DiffDelete:
			n := byteLen(oldTokens[oldTok : oldTok+count])
			oldSpans = appendSpan(oldSpans, oldOff, oldOff+n)
			oldOff += n
			oldTok += count
		case diffmatchpatch.DiffInsert:
			n := byteLen(newTokens[newTok : newTok+count])
			newSpans = appendSpan(newSpans, newOff, newOff+n)
			newOff += n
			newTok += count
		}
	}
	return oldSpans, newSpans
}

// tokenID returns the rune used to represent the n-th distinct token, skipping the surrogate range.
func tokenID(n int) rune {
	id := rune(n + 1)
	if id >= firstSurrogate {
		id += surrogateCount
	}
	return id
}

func wholeLine(line string) []Span {
	if line == "" {
		return nil
	}
	return []Span{{Start: 0, End: len(line)}}
}

// appendSpan adds a span, merging it with the previous one when they touch.
func appendSpan(spans []Span, start, end int) []Span {
	if last := len(spans) - 1; last >= 0 && spans[last].End == start {
		spans[last].End = end
		return spans
	}
	return append(spans, Span{Start: start, End: end})
}

func byteLen(tokens []string) int {
	total := 0
	for _, token := range tokens {
		total += len(token)
	}
	return total
}

func tokenize(line string) []string {
	var (
		tokens []string
		start  = -1
		class  = classOther
	)
	for i, r := range line {
		c := classOther
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			c = classWord
		case unicode.IsSpace(r):
			c = classSpace
		}
		if c != classOther && c == class {
			continue
		}
		if start >= 0 {
			tokens = append(tokens, line[start:i])
		}
		start, class = i, c
	}
	if start >= 0 {
		tokens = append(tokens, line[start:])
	}
	return tokens
}
