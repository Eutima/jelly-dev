package ui

import "testing"

func TestIndentFor(t *testing.T) {
	for _, c := range []struct {
		name  string
		path  string
		lines []string
		want  string
	}{
		{"typescript default", "a.ts", []string{"x"}, "  "},
		{"go default", "a.go", []string{"x"}, "\t"},
		{"python default", "a.py", nil, "    "},
		{"file with 4 spaces wins", "a.ts", []string{"class A {", "    x = 1", "        y"}, "    "},
		{"file with tabs wins", "a.py", []string{"if x:", "\ty"}, "\t"},
		{"makefile", "Makefile", []string{"a:", "    b"}, "\t"},
	} {
		if got := indentFor(c.path, c.lines); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
