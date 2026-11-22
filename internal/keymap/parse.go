package keymap

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// None is the value that unbinds a command.
const None = "none"

// Assignment is one valid line of a .jdrc file: the keys of a command (none for an unbound command).
type Assignment struct {
	Command Command
	Keys    []Sequence // normalized, in the order written; nil unbinds the command
	Line    int
}

// Problem is a line of a .jdrc file that was not used, and why.
type Problem struct {
	Source string // the file, as the user knows it (~/.jdrc)
	Line   int    // 0 when the problem is about the file itself
	Text   string
}

func (p Problem) String() string {
	if p.Line == 0 {
		return fmt.Sprintf("%s: %s", p.Source, p.Text)
	}
	return fmt.Sprintf("%s line %d: %s", p.Source, p.Line, p.Text)
}

// Parse reads a .jdrc file: `command = key[, key...]`, `#` starts a comment, blank lines are ignored. A line that cannot be used is
// reported and skipped; the rest still counts.
func Parse(r io.Reader, source string) ([]Assignment, []Problem) {
	var out []Assignment
	var problems []Problem
	bad := func(line int, format string, args ...any) {
		problems = append(problems, Problem{Source: source, Line: line, Text: fmt.Sprintf(format, args...)})
	}
	scanner := bufio.NewScanner(r)
	for number := 1; scanner.Scan(); number++ {
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			bad(number, "expected `command = key`")
			continue
		}
		command := Command(strings.ToLower(strings.TrimSpace(name)))
		definition, known := find(command)
		if !known {
			bad(number, "unknown command %q (jd rc lists them)", strings.TrimSpace(name))
			continue
		}
		keys, err := parseKeys(value, definition.scope)
		if err != nil {
			bad(number, "%s: %v", command, err)
			continue
		}
		out = append(out, Assignment{Command: command, Keys: keys, Line: number})
	}
	if err := scanner.Err(); err != nil {
		bad(0, "%v", err)
	}
	return out, problems
}

// stripComment removes a comment: a # at the start of the line or after white space (so `hash` is the name of the # key, not a comment).
func stripComment(line string) string {
	for i, r := range line {
		if r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

func parseKeys(value string, scope Scope) ([]Sequence, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, None) {
		return nil, nil
	}
	var out []Sequence
	for _, part := range strings.Split(value, ",") {
		seq, err := ParseSequence(part)
		if err != nil {
			return nil, err
		}
		if err := checkSequence(seq, scope); err != nil {
			return nil, err
		}
		for _, other := range out {
			switch {
			case seq.equal(other):
				return nil, fmt.Errorf("%s is listed twice", seq)
			case overlaps(seq, other):
				return nil, fmt.Errorf("%s and %s overlap: a key sequence may not be the start of another one", seq, other)
			}
		}
		out = append(out, seq)
	}
	return out, nil
}

// reservedFirst are the keys a key sequence of the editor may not start with: the editor needs them for itself.
var reservedFirst = map[string]bool{"esc": true, "ctrl+c": true, "ctrl+d": true}

// checkSequence applies the rules of a scope to one sequence.
func checkSequence(seq Sequence, scope Scope) error {
	if len(seq) > 1 && scope != ScopeVim {
		return fmt.Errorf("a key sequence (%s) is only for the vim.* commands", seq)
	}
	switch scope {
	case ScopeIDE:
		if !hasModifier(seq[0]) {
			return fmt.Errorf("%s needs ctrl, alt, shift or a function key in the project window (a bare key would be typed into the editor or the shell)", seq)
		}
	case ScopeInsert:
		if single(seq[0]) || seq[0] == " " {
			return fmt.Errorf("%s is typed text in insert mode; use ctrl, alt, a named key or a function key", seq)
		}
	case ScopeNano:
		if single(seq[0]) || seq[0] == " " {
			return fmt.Errorf("%s is typed text in the nano-style editor; use ctrl, alt, a named key or a function key", seq)
		}
	case ScopeVim:
		first := seq[0]
		if len(first) == 1 && first[0] >= '0' && first[0] <= '9' {
			return fmt.Errorf("%s: a count digit cannot start a key sequence", seq)
		}
		if reservedFirst[first] {
			return fmt.Errorf("%s cannot be rebound: the editor keeps it for itself", Written(first))
		}
	}
	return nil
}
