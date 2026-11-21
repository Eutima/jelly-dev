package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// TextEdit replaces a range of a text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// ErrCannotFormat is returned when the language server does not format documents.
var ErrCannotFormat = errors.New("the language server cannot format this file")

// Format asks the server to format a whole document.
func (c *Client) Format(ctx context.Context, path string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	params := map[string]any{
		"textDocument": map[string]string{"uri": PathToURI(path)},
		"options":      map[string]any{"tabSize": tabSize, "insertSpaces": insertSpaces},
	}
	var edits []TextEdit
	if err := c.call(ctx, "textDocument/formatting", params, &edits); err != nil {
		var rpc *rpcError
		if errors.As(err, &rpc) && rpc.Code == -32601 { // method not found
			return nil, ErrCannotFormat
		}
		return nil, err
	}
	return edits, nil
}

// ApplyEdits applies text edits to a text. Positions count UTF-16 code units per line, as the protocol says; the edits
// refer to the original text and must not overlap.
func ApplyEdits(text string, edits []TextEdit) (string, error) {
	if len(edits) == 0 {
		return text, nil
	}
	runes := []rune(text)
	starts := []int{0} // rune index where each line starts
	for i, r := range runes {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	offset := func(pos Position) (int, error) {
		if pos.Line < 0 || pos.Line >= len(starts) {
			if pos.Line == len(starts) && pos.Character == 0 { // just behind the last line
				return len(runes), nil
			}
			return 0, fmt.Errorf("edit at line %d is outside the text", pos.Line+1)
		}
		end := len(runes)
		if pos.Line+1 < len(starts) {
			end = starts[pos.Line+1] - 1 // before the line break
		}
		line := string(runes[starts[pos.Line]:end])
		return starts[pos.Line] + RuneColumn(line, pos.Character), nil
	}
	type span struct {
		from, to int
		text     string
	}
	spans := make([]span, 0, len(edits))
	for _, edit := range edits {
		from, err := offset(edit.Range.Start)
		if err != nil {
			return "", err
		}
		to, err := offset(edit.Range.End)
		if err != nil {
			return "", err
		}
		if to < from {
			return "", errors.New("an edit ends before it starts")
		}
		spans = append(spans, span{from, to, edit.NewText})
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].from < spans[j].from })
	var out strings.Builder
	last := 0
	for _, s := range spans {
		if s.from < last {
			return "", errors.New("the edits overlap")
		}
		out.WriteString(string(runes[last:s.from]))
		out.WriteString(s.text)
		last = s.to
	}
	out.WriteString(string(runes[last:]))
	return out.String(), nil
}

// Format formats a file's text: with the formatter command of its server entry when that is installed, else
// with the language server. tabSize and insertSpaces tell the server how to indent.
func (m *Manager) Format(ctx context.Context, path, text string) (string, error) {
	spec, languageID, ok := specFor(m.servers, path)
	if !ok {
		return "", ErrNoServer
	}
	if len(spec.Formatter) > 0 {
		if _, err := m.lookPath(spec.Formatter[0]); err == nil {
			return runFormatter(ctx, spec.Formatter, path, text, rootFor(spec, m.root, path))
		}
	}
	c, _, err := m.client(ctx, path)
	if err != nil {
		return "", err
	}
	if err := c.Sync(path, languageID, text); err != nil {
		return "", err
	}
	tabSize, insertSpaces := indentationOf(text)
	edits, err := c.Format(ctx, path, tabSize, insertSpaces)
	if err != nil {
		return "", err
	}
	return ApplyEdits(text, edits)
}

// indentationOf guesses how a text is indented: with tabs when more lines start with a tab than with spaces, else with
// spaces, as many as the least indented line has (2 to 8, 4 when nothing is indented).
func indentationOf(text string) (tabSize int, insertSpaces bool) {
	tabs, spaced, least := 0, 0, 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case strings.HasPrefix(line, "\t"):
			tabs++
		case trimmed != "" && len(line) > len(trimmed):
			spaced++
			if indent := len(line) - len(trimmed); least == 0 || indent < least {
				least = indent
			}
		}
	}
	if tabs > spaced {
		return 4, false
	}
	if least < 2 || least > 8 {
		least = 4
	}
	return least, true
}

// runFormatter pipes the text through an external formatter; "{file}" in its arguments is replaced by the file name.
func runFormatter(ctx context.Context, command []string, path, text, dir string) (string, error) {
	args := make([]string, len(command)-1)
	for i, arg := range command[1:] {
		args[i] = strings.ReplaceAll(arg, "{file}", path)
	}
	cmd := exec.CommandContext(ctx, command[0], args...) // #nosec G204 -- the command comes from the user's own configuration
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		if first, _, found := strings.Cut(message, "\n"); found {
			message = first
		}
		return "", fmt.Errorf("%s: %s", command[0], message)
	}
	return stdout.String(), nil
}
