package search

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxFileBytes is the largest file whose content is searched.
	MaxFileBytes = 1 << 20
	// MaxMatches is the most matches a text search returns.
	MaxMatches  = 2000
	binaryProbe = 8000
)

// TextMatch is a line that contains the search text.
type TextMatch struct {
	Path   string // relative to the project, with slashes
	Row    int    // zero-based line
	Col    int    // rune index of the match in the line
	Length int    // runes matched
	Text   string // the line, without line ending (cut when very long)
	Offset int    // runes cut from the start of Text: the match is at Col-Offset in Text
}

// TextOptions tune a text search.
type TextOptions struct {
	Regex bool // the query is a regular expression (Go syntax), else plain text
	// CaseSensitive makes the search case-sensitive; without it a query that has capital letters is case-sensitive
	// anyway and one in lower case is not ("smart case").
	CaseSensitive bool
	Limit         int // 0 means MaxMatches
}

// matcher finds the matches in a line.
type matcher func(line string) (col, length int, ok bool)

func newMatcher(query string, opts TextOptions) (matcher, error) {
	smart := !opts.CaseSensitive && !hasUpper(query)
	if opts.Regex {
		pattern := query
		if smart {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		return func(line string) (int, int, bool) {
			loc := re.FindStringIndex(line)
			if loc == nil || loc[0] == loc[1] {
				return 0, 0, false
			}
			return utf8.RuneCountInString(line[:loc[0]]), utf8.RuneCountInString(line[loc[0]:loc[1]]), true
		}, nil
	}
	if smart {
		lower := strings.ToLower(query)
		return func(line string) (int, int, bool) {
			folded := strings.ToLower(line)
			if len(folded) != len(line) { // lowering changed the byte length: byte positions would be off
				return foldedIndex(line, lower)
			}
			index := strings.Index(folded, lower)
			if index < 0 {
				return 0, 0, false
			}
			return utf8.RuneCountInString(line[:index]), utf8.RuneCountInString(lower), true
		}, nil
	}
	return func(line string) (int, int, bool) {
		index := strings.Index(line, query)
		if index < 0 {
			return 0, 0, false
		}
		return utf8.RuneCountInString(line[:index]), utf8.RuneCountInString(query), true
	}, nil
}

// foldedIndex is a slower case-insensitive search that works on runes, for lines where lower-casing changes the byte length.
func foldedIndex(line, lowerQuery string) (int, int, bool) {
	runes, query := []rune(line), []rune(lowerQuery)
	for start := 0; start+len(query) <= len(runes); start++ {
		match := true
		for i, q := range query {
			if unicode.ToLower(runes[start+i]) != q {
				match = false
				break
			}
		}
		if match {
			return start, len(query), true
		}
	}
	return 0, 0, false
}

func hasUpper(text string) bool {
	for _, r := range text {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// Text searches the content of files (paths relative to root, as Files returns them) for a query and returns the
// matching lines in the order of the files, at most opts.Limit. Binary files and files over MaxFileBytes are skipped.
// truncated reports that there were more matches.
func Text(ctx context.Context, root string, files []string, query string, opts TextOptions) (matches []TextMatch, truncated bool, err error) {
	if query == "" {
		return nil, false, nil
	}
	find, err := newMatcher(query, opts)
	if err != nil {
		return nil, false, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = MaxMatches
	}
	results := make([][]TextMatch, len(files))
	var next int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < max(runtime.NumCPU(), 2); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				index := next
				next++
				mu.Unlock()
				if index >= len(files) || ctx.Err() != nil {
					return
				}
				results[index] = searchFile(root, files[index], find, limit+1) // one more than needed shows that there are more
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	for _, found := range results {
		for _, match := range found {
			if len(matches) >= limit {
				return matches, true, nil
			}
			matches = append(matches, match)
		}
	}
	return matches, false, nil
}

// maxLineRunes is how much of a matching line is kept for the result.
const maxLineRunes = 300

func searchFile(root, rel string, find matcher, limit int) []TextMatch {
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data[:min(len(data), binaryProbe)], 0) >= 0 || !utf8.Valid(data) {
		return nil
	}
	var out []TextMatch
	for row, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		col, length, ok := find(line)
		if !ok {
			continue
		}
		text, offset := line, 0
		if runes := []rune(line); len(runes) > maxLineRunes {
			offset = max(col-maxLineRunes/3, 0)
			text = string(runes[offset:min(offset+maxLineRunes, len(runes))])
		}
		out = append(out, TextMatch{Path: rel, Row: row, Col: col, Length: length, Text: text, Offset: offset})
		if len(out) >= limit {
			break
		}
	}
	return out
}
