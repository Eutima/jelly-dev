// Package editor loads and saves the text files the editor works on.
package editor

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/source"
)

const (
	lf   = "\n"
	crlf = "\r\n"
)

// Document is the text of a file together with what is needed to write it back the way it was read.
type Document struct {
	Path         string
	Lines        []string
	LineEnding   string // "\n" or "\r\n"
	FinalNewline bool   // the file ended with a line break
	New          bool   // the file does not exist yet
}

// Load reads path. A file that does not exist gives an empty new document; anything else that cannot be read, is
// binary or too large is an error.
func Load(path string) (Document, error) {
	text, err := source.ReadTextFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Document{Path: path, LineEnding: lf, FinalNewline: true, New: true}, nil
		}
		return Document{}, err
	}
	doc := Document{Path: path, Lines: diff.SplitLines(text), LineEnding: lf, FinalNewline: text == "" || strings.HasSuffix(text, lf)}
	if strings.Contains(text, crlf) {
		doc.LineEnding = crlf
	}
	return doc, nil
}

// Save writes lines to path atomically with the line ending and final newline of the document and returns the
// document that describes the saved file.
func (d Document) Save(path string, lines []string) (Document, error) {
	var text strings.Builder
	for i, line := range lines {
		text.WriteString(line)
		if i < len(lines)-1 || d.FinalNewline {
			text.WriteString(d.LineEnding)
		}
	}
	if err := source.WriteAtomic(path, []byte(text.String())); err != nil {
		return d, err
	}
	d.Path, d.Lines, d.New = path, append([]string(nil), lines...), false
	return d, nil
}

// Exists reports whether path is an existing file or directory.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
