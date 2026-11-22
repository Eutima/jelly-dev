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
	Keys    []string // normalized, in the order written
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

func parseKeys(value string, scope Scope) ([]string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, None) {
		return nil, nil
	}
	var keys []string
	seen := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		key, err := Normalize(part)
		if err != nil {
			return nil, err
		}
		if scope == ScopeIDE && !hasModifier(key) {
			return nil, fmt.Errorf("%s needs ctrl, alt, shift or a function key in the project window (a bare key would be typed into the editor or the shell)", Written(key))
		}
		if seen[key] {
			return nil, fmt.Errorf("%s is listed twice", Written(key))
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, nil
}
