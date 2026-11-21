package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFiles_SkipsDependencyFoldersAndIgnoredFiles(t *testing.T) {
	root := project(t, map[string]string{
		"src/main.go":                   "package main",
		"README.md":                     "hi",
		"node_modules/lib/index.js":     "x",
		"web/node_modules/lib/index.js": "x",
		".venv/lib/site.py":             "x",
		"venv/bin/activate":             "x",
		"__pycache__/a.pyc":             "x",
		".git/config":                   "x",
		"app.log":                       "x",
		"build/out.txt":                 "x",
		"docs/generated/page.html":      "x",
		"pkg.egg-info/PKG-INFO":         "x",
		".gitignore":                    "*.log\n/build/\n",
		".jdignore":                     "docs/generated/\n",
	})
	files, truncated, err := Files(context.Background(), root)
	if err != nil || truncated {
		t.Fatalf("%v %v", err, truncated)
	}
	want := []string{".gitignore", ".jdignore", "README.md", "src/main.go"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files %v, want %v", files, want)
	}
}

func TestFiles_StopsWhenContextCancelled(t *testing.T) {
	root := project(t, map[string]string{"a.txt": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Files(ctx, root); err == nil {
		t.Fatal("a cancelled search must stop")
	}
}

func paths(matches []FileMatch) []string {
	var out []string
	for _, m := range matches {
		out = append(out, m.Path)
	}
	return out
}

func TestMatchFiles_RanksNameMatchesFirst(t *testing.T) {
	files := []string{"docs/readme.md", "internal/ui/editor.go", "internal/ui/editor_test.go", "internal/editor/document.go", "cmd/main.go", "scripts/install.sh"}
	tests := []struct {
		query string
		want  []string
	}{
		{"editor", []string{"internal/ui/editor.go", "internal/ui/editor_test.go", "internal/editor/document.go"}},
		{"main", []string{"cmd/main.go"}},
		{"zzz", nil},
	}
	for _, tt := range tests {
		got := paths(MatchFiles(files, tt.query, 10))
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q: got %v, want %v", tt.query, got, tt.want)
		}
	}
	if got := paths(MatchFiles(files, "ins", 10)); len(got) == 0 || got[0] != "scripts/install.sh" {
		t.Errorf("ins: got %v", got)
	}
	if got := paths(MatchFiles(files, "uied", 10)); len(got) < 2 || got[0] != "internal/ui/editor.go" { // letters spread over the path
		t.Errorf("uied: got %v", got)
	}
	if got := paths(MatchFiles(files, "ed go", 10)); len(got) == 0 || !strings.HasSuffix(got[0], "editor.go") {
		t.Errorf("two words: got %v", got)
	}
	if got := MatchFiles(files, "", 3); len(got) != 3 || got[0].Path != files[0] {
		t.Fatalf("an empty query lists the files in order: %v", got)
	}
	if got := MatchFiles(files, "go", 2); len(got) != 2 {
		t.Fatalf("the limit applies: %v", got)
	}
}

func TestMatchFiles_PrefersExactFileName(t *testing.T) {
	got := paths(MatchFiles([]string{"a/maintenance.go", "b/main.go", "c/domain.go"}, "main.go", 5))
	if len(got) == 0 || got[0] != "b/main.go" {
		t.Fatalf("got %v", got)
	}
}

func TestText_FindsLinesWithPositions(t *testing.T) {
	root := project(t, map[string]string{
		"a.go":    "package main\n\nfunc Hello() {}\nvar hello = 1\n",
		"b.txt":   "nothing\nhello world\r\nÄhello\n",
		"bin.dat": "hello\x00binary",
	})
	files := []string{"a.go", "b.txt", "bin.dat"}
	matches, truncated, err := Text(context.Background(), root, files, "hello", TextOptions{})
	if err != nil || truncated {
		t.Fatalf("%v %v", err, truncated)
	}
	var got []string
	for _, m := range matches {
		got = append(got, fmt.Sprintf("%s:%d:%d:%s", m.Path, m.Row, m.Col, m.Text))
	}
	want := []string{"a.go:2:5:func Hello() {}", "a.go:3:4:var hello = 1", "b.txt:1:0:hello world", "b.txt:2:1:Ähello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestText_SmartCase(t *testing.T) {
	root := project(t, map[string]string{"a.txt": "Hello\nhello\nHELLO\n"})
	files := []string{"a.txt"}
	count := func(query string, opts TextOptions) int {
		m, _, err := Text(context.Background(), root, files, query, opts)
		if err != nil {
			t.Fatal(err)
		}
		return len(m)
	}
	if got := count("hello", TextOptions{}); got != 3 {
		t.Errorf("lower case query ignores case: %d", got)
	}
	if got := count("Hello", TextOptions{}); got != 1 {
		t.Errorf("a capital letter makes it case sensitive: %d", got)
	}
	if got := count("hello", TextOptions{CaseSensitive: true}); got != 1 {
		t.Errorf("case sensitive option: %d", got)
	}
}

func TestText_RegexAndInvalidRegex(t *testing.T) {
	root := project(t, map[string]string{"a.txt": "func a()\nfunc bb(x)\nvar c\n"})
	matches, _, err := Text(context.Background(), root, []string{"a.txt"}, `func \w+\(\w*\)`, TextOptions{Regex: true})
	if err != nil || len(matches) != 2 || matches[1].Length != len("func bb(x)") {
		t.Fatalf("%v %v", matches, err)
	}
	if _, _, err := Text(context.Background(), root, []string{"a.txt"}, `(`, TextOptions{Regex: true}); err == nil || !strings.Contains(err.Error(), "invalid regular expression") {
		t.Fatalf("got %v", err)
	}
}

func TestText_LimitsMatchesAndSkipsLargeFiles(t *testing.T) {
	root := project(t, map[string]string{"many.txt": strings.Repeat("needle\n", 50), "big.txt": strings.Repeat("needle ", MaxFileBytes/6)})
	matches, truncated, err := Text(context.Background(), root, []string{"big.txt", "many.txt"}, "needle", TextOptions{Limit: 10})
	if err != nil || !truncated || len(matches) != 10 {
		t.Fatalf("%d %v %v", len(matches), truncated, err)
	}
	for _, m := range matches {
		if m.Path == "big.txt" {
			t.Fatal("files over the size limit must be skipped")
		}
	}
}

func TestText_CutsVeryLongLinesAroundTheMatch(t *testing.T) {
	line := strings.Repeat("x", 1000) + "needle" + strings.Repeat("y", 1000)
	root := project(t, map[string]string{"a.txt": line})
	matches, _, _ := Text(context.Background(), root, []string{"a.txt"}, "needle", TextOptions{})
	if len(matches) != 1 || matches[0].Col != 1000 || len([]rune(matches[0].Text)) > maxLineRunes {
		t.Fatalf("%+v", matches)
	}
	if got := []rune(matches[0].Text)[matches[0].Col-matches[0].Offset]; got != 'n' {
		t.Fatalf("Col-Offset must point at the match, got %q", got)
	}
}

func TestText_StopsWhenContextCancelled(t *testing.T) {
	root := project(t, map[string]string{"a.txt": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Text(ctx, root, []string{"a.txt"}, "x", TextOptions{}); err == nil {
		t.Fatal("a cancelled search must stop")
	}
	if m, _, err := Text(context.Background(), root, []string{"a.txt"}, "", TextOptions{}); err != nil || m != nil {
		t.Fatal("an empty query finds nothing")
	}
}
