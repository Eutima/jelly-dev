package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eutima/jelly-dev/internal/search"
)

const (
	previewMinWindow = 16 // below this window height the content search shows no preview
	previewMaxRows   = 12 // the header and 11 lines of code: five before and five after the match
)

// previewCache keeps the lines of the file shown last, so moving the selection inside one file reads it once; it is dropped when the file
// changes on disk (size or time). Errors are not kept. It is a pointer in the model, which is copied on every key, and is only used by
// View on the Bubble Tea goroutine.
type previewCache struct {
	path  string
	size  int64
	mtime time.Time
	lines []string
}

// previewLineRunes is the longest piece of a line the preview wraps: a minified file has lines of megabytes, which would make every redraw
// slow. The window of the matching line is centred on the match.
const previewLineRunes = 2000

// previewHeight is the number of lines of the code preview under the results of a content search (0: none).
func (m SearchModel) previewHeight() int {
	if m.mode != searchText || m.height < previewMinWindow {
		return 0
	}
	return min(previewMaxRows, m.height/3)
}

// errNotRegular says a path is no longer a file (it was one when the search read it).
var errNotRegular = errors.New("not a regular file")

// errPreviewTooLarge says a file is bigger than what the content search reads.
var errPreviewTooLarge = errors.New("file too large for a preview")

// previewFile returns the lines of a file of the project.
func (m SearchModel) previewFile(path string) ([]string, error) {
	full := filepath.Join(m.root, filepath.FromSlash(path))
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() { // a pipe or device swapped in after the search would block the window
		return nil, errNotRegular
	}
	if info.Size() > search.MaxFileBytes {
		return nil, errPreviewTooLarge
	}
	if cache := m.preview; cache != nil && cache.path == path && cache.size == info.Size() && cache.mtime.Equal(info.ModTime()) {
		return cache.lines, nil
	}
	data, err := os.ReadFile(full) // #nosec G304 -- a file of the user's own project
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n")
	if m.preview != nil {
		*m.preview = previewCache{path: path, size: info.Size(), mtime: info.ModTime(), lines: lines}
	}
	return lines, nil
}

// expandPreviewText turns tabs into four spaces and moves a match (column and length in runes of the original text) with them. It keeps at
// most previewLineRunes runes, around the column when it is given (col >= 0); the returned column is relative to the kept text.
func expandPreviewText(text string, col, length int) (runes []rune, start, width int) {
	original := []rune(text)
	from := 0
	if col >= 0 && len(original) > previewLineRunes {
		from = max(min(col-previewLineRunes/4, len(original)-previewLineRunes), 0)
	}
	to := min(from+previewLineRunes, len(original))
	start, width = -1, length
	for i := from; i < to; i++ {
		if i == col {
			start = len(runes)
		}
		if original[i] == '\t' {
			runes = append(runes, ' ', ' ', ' ', ' ')
			if col >= 0 && i >= col && i < col+length {
				width += 3 // a tab inside the match is wider when drawn
			}
			continue
		}
		runes = append(runes, original[i])
	}
	if col >= to {
		start = len(runes)
	}
	return runes, start, width
}

// previewMatchStillThere tells whether the text of the match is still at its place in the line: the file may have been edited since the search.
func previewMatchStillThere(line string, match search.TextMatch) bool {
	if match.Length == 0 {
		return true
	}
	found := []rune(match.Text)
	from := match.Col - match.Offset
	if from < 0 || from+match.Length > len(found) {
		return true // the search kept less of a very long line than the match: nothing to compare
	}
	current := []rune(line)
	if match.Col+match.Length > len(current) {
		return false
	}
	return string(current[match.Col:match.Col+match.Length]) == string(found[from:from+match.Length])
}

// previewRows draws the code around the selected result: a header, then the lines before and after the match (as many as the panel
// has room for, up to five on each side). The matching line looks like the selected result and shows the match; long lines wrap.
// There are always previewHeight rows, blank when nothing is selected, so the list does not jump while typing.
func (m SearchModel) previewRows() []string {
	height := m.previewHeight()
	if height == 0 {
		return nil
	}
	blank := strings.Repeat(" ", m.width)
	rows := make([]string, 0, height)
	if m.sel < 0 || m.sel >= len(m.textResults) {
		for len(rows) < height {
			rows = append(rows, blank)
		}
		return rows
	}
	match := m.textResults[m.sel]
	context := (height - 1) / 2
	rows = append(rows, m.theme.Header.Render(padRight(fmt.Sprintf(" %s:%d  (±%d lines)", sanitize(match.Path), match.Row+1, context), m.width)))
	lines, err := m.previewFile(match.Path)
	switch {
	case errors.Is(err, errPreviewTooLarge):
		rows = append(rows, m.theme.Equal.Faint(true).Render(padRight(" "+err.Error(), m.width)))
	case err != nil:
		rows = append(rows, m.theme.Equal.Faint(true).Render(padRight(" cannot read the file", m.width)))
	case match.Row >= len(lines) || !previewMatchStillThere(lines[match.Row], match):
		rows = append(rows, m.theme.Equal.Faint(true).Render(padRight(" the file changed: search again", m.width)))
	default:
		rows = append(rows, m.previewCode(lines, match, context, height-1)...)
	}
	for len(rows) < height {
		rows = append(rows, blank)
	}
	return rows[:height]
}

// previewCode draws the lines around the match in at most budget rows, the whole matching line always among them.
func (m SearchModel) previewCode(lines []string, match search.TextMatch, context, budget int) []string {
	first, last := max(match.Row-context, 0), min(match.Row+context, len(lines)-1)
	numWidth := len(fmt.Sprint(last + 1))
	var drawn []string
	matchFrom, matchTo := 0, 0
	for row := first; row <= last; row++ {
		prefix := fmt.Sprintf(" %*d │ ", numWidth, row+1)
		if row != match.Row {
			text, _, _ := expandPreviewText(lines[row], -1, 0)
			drawn = append(drawn, m.matchRowLines(prefix, text, -1, 0, m.theme.Equal, false)...)
			continue
		}
		text, start, width := expandPreviewText(lines[row], previewColumn(lines[row], match.Col), match.Length)
		matchFrom = len(drawn)
		drawn = append(drawn, m.matchRowLines(prefix, text, start, width, m.theme.Selected, true)...)
		matchTo = len(drawn)
	}
	from := 0
	if matchTo > budget {
		from = min(matchTo-budget, matchFrom) // the end of the match line must fit; a taller line shows from its start
	}
	return drawn[from:min(len(drawn), from+budget)]
}

// previewColumn is the rune column of a match in the original text, kept within the line.
func previewColumn(line string, col int) int { return max(min(col, len([]rune(line))), 0) }
