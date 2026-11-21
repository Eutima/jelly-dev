// Package lsp is a small Language Server Protocol client: it starts language servers, keeps their view of the open
// files up to date and asks them for definitions and references.
package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Position is a place in a text; Character counts UTF-16 code units, as the protocol says.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a stretch of text.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range in a file.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Place is a location the editor can jump to: a file path and a zero-based line and rune column, with the text of the line.
type Place struct {
	Path string
	Row  int
	Col  int
	Text string
}

// PathToURI returns the file: URI of a path.
func PathToURI(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed // C:/x becomes /C:/x
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// URIToPath returns the path of a file: URI; ok is false for other schemes.
func URIToPath(uri string) (string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	path := parsed.Path
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), true
}

// UTF16Column converts a rune index of a line into UTF-16 code units.
func UTF16Column(line []rune, col int) int {
	col = min(max(col, 0), len(line))
	units := 0
	for _, r := range line[:col] {
		units += len(utf16.Encode([]rune{r}))
	}
	return units
}

// RuneColumn converts UTF-16 code units of a line into a rune index (clamped to the line).
func RuneColumn(line string, units int) int {
	count := 0
	for index, r := range []rune(line) {
		if count >= units {
			return index
		}
		count += len(utf16.Encode([]rune{r}))
	}
	return len([]rune(line))
}
