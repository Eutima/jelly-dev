package keymap

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func parse(t *testing.T, text string) ([]Assignment, []Problem) {
	t.Helper()
	return Parse(strings.NewReader(text), "test")
}

func TestParse_ReadsCommandsKeysCommentsAndNone(t *testing.T) {
	got, problems := parse(t, "# comment\n\ntabs = ctrl+J   # inline\nterminal = alt+x, F4\ndiff = none\nmerge.ours = Alt+o\nmerge.wrap = space\n")
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	want := []Assignment{
		{Tabs, []string{"ctrl+j"}, 3}, {Terminal, []string{"alt+x", "f4"}, 4}, {Diff, nil, 5},
		{MergeOurs, []string{"alt+o"}, 6}, {MergeWrap, []string{" "}, 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParse_ReportsBadLinesWithTheirNumberAndKeepsTheRest(t *testing.T) {
	got, problems := parse(t, "tabz = ctrl+j\ntabs ctrl+j\nterminal = banana\nfind = x\ngrep = ctrl+i\nprojects = ctrl+p, ctrl+p\npalette = alt+p\n")
	if len(got) != 1 || got[0].Command != Palette {
		t.Fatalf("only the last line is good: %+v", got)
	}
	wants := []string{"line 1: unknown command", "line 2: expected `command = key`", "line 3: terminal: unknown key", "line 4: find: x needs ctrl, alt, shift or a function key", "line 5: grep: ctrl+i cannot be told apart from tab", "line 6: projects: ctrl+p is listed twice"}
	if len(problems) != len(wants) {
		t.Fatalf("problems %v", problems)
	}
	for i, want := range wants {
		if !strings.Contains(problems[i].String(), want) {
			t.Errorf("problem %d: %q lacks %q", i, problems[i], want)
		}
	}
}

func TestNormalize_AcceptsTheNamesBubbleTeaUses(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+t": "ctrl+t", "CTRL+T": "ctrl+t", "alt+X": "alt+X", "f1": "f1", "F12": "f12", "esc": "esc", "Shift+Tab": "shift+tab",
		"ctrl+right": "ctrl+right", "ctrl+]": "ctrl+]", "G": "G", ":": ":", "space": " ", "comma": ",", "hash": "#", "alt+space": "alt+ ",
		"pgdown": "pgdown", "alt+up": "alt+up",
	} {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ctrl+", "ctrl+1", "f21", "f0", "banana", "ctrl+m", "ctrl+[", "ctrl+h", "alt+alt+j", "xx"} {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, want an error", in, got)
		}
	}
}

func TestDefault_HasEveryCommandWithItsKeysAndNoKeyTwiceInAScope(t *testing.T) {
	m := Default()
	for _, d := range definitions {
		if got := m.Keys(d.command); !reflect.DeepEqual(got, d.keys) {
			t.Errorf("%s: %v", d.command, got)
		}
		if m.Source(d.command) != "default" {
			t.Errorf("%s: source %q", d.command, m.Source(d.command))
		}
		for _, key := range d.keys {
			if normalized, err := Normalize(key); err != nil || normalized != key {
				t.Errorf("%s: the default key %q is not valid: %v", d.command, key, err)
			}
			if d.scope == ScopeIDE && !hasModifier(key) {
				t.Errorf("%s: the default key %q needs a modifier in the project window", d.command, key)
			}
			if got, ok := m.Lookup(d.scope, key); !ok || got != d.command {
				t.Errorf("%s: key %q looks up %q", d.command, key, got)
			}
		}
	}
	if got, ok := m.Lookup(ScopeMerge, "o"); !ok || got != MergePickOurs {
		t.Fatalf("o in the merge view: %q", got)
	}
	if _, ok := m.Lookup(ScopeIDE, "o"); ok {
		t.Fatal("scopes are separate")
	}
}

func TestApply_AnAssignedCommandLosesItsOldKeysAndTheNewOnesWork(t *testing.T) {
	a, _ := parse(t, "tabs = ctrl+j\nmerge.ours = alt+o\n")
	m, problems := Default().Apply(a, "~/.jdrc")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if m.Is(Tabs, "alt+t") || m.Is(Tabs, "shift+tab") || !m.Is(Tabs, "ctrl+j") || m.Label(Tabs) != "ctrl+j" {
		t.Fatalf("tabs: %v", m.Keys(Tabs))
	}
	if got, ok := m.Lookup(ScopeIDE, "alt+t"); ok {
		t.Fatalf("the old key still runs %q", got)
	}
	if m.Label(MergeOurs) != "alt+o" || m.Source(MergeOurs) != "~/.jdrc:2" {
		t.Fatalf("merge.ours: %q from %q", m.Label(MergeOurs), m.Source(MergeOurs))
	}
	if m.Label(Find) != "ctrl+f" { // untouched commands keep their keys
		t.Fatal("find changed")
	}
}

func TestApply_AKeyMovesFromADefaultButNotFromAnotherFilesChoice(t *testing.T) {
	a, _ := parse(t, "merge.ours = n\n") // n is the default of merge.next
	m, problems := Default().Apply(a, "~/.jdrc")
	owner, _ := m.Lookup(ScopeMerge, "n")
	if len(problems) != 0 || m.Is(MergeNext, "n") || !m.Is(MergeNext, "tab") || owner != MergeOurs {
		t.Fatalf("n goes to merge.ours: %v %v", m.Keys(MergeNext), problems)
	}
	b, _ := parse(t, "merge.both = n\n")
	m2, problems := m.Apply(b, "./.jdrc")
	if len(problems) != 1 || !strings.Contains(problems[0].String(), "n is used by merge.ours") || m2.Is(MergeBoth, "n") || !m2.Is(MergeBoth, "b") {
		t.Fatalf("a key chosen in a file is not taken away: %v", problems)
	}
}

func TestApply_TwoCommandsCanSwapKeys(t *testing.T) {
	a, _ := parse(t, "merge.ours = left\nmerge.theirs = right\n")
	m, problems := Default().Apply(a, "f")
	if len(problems) != 0 || !m.Is(MergeOurs, "left") || !m.Is(MergeTheirs, "right") {
		t.Fatalf("swap: %v %v", problems, m.Keys(MergeOurs))
	}
}

func TestApply_NoneUnbindsAndTheSameKeyInAnotherScopeIsFine(t *testing.T) {
	a, _ := parse(t, "diff = none\nmerge.undo = ctrl+t\nmerge.alltheirs = f5\n")
	m, problems := Default().Apply(a, "f")
	if len(problems) != 0 || len(m.Keys(Diff)) != 0 || m.Label(Diff) != "" || m.Key(Diff) != "" {
		t.Fatalf("diff must be unbound: %v %v", m.Keys(Diff), problems)
	}
	if !m.Is(Terminal, "ctrl+t") || !m.Is(MergeUndo, "ctrl+t") {
		t.Fatal("ctrl+t is free to use in each scope")
	}
}

func TestLoad_ReadsHomeThenConfigDirThenProjectAndReportsProblems(t *testing.T) {
	home, config, project := t.TempDir(), t.TempDir(), t.TempDir()
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".jdrc"), "tabs = ctrl+j\nfind = alt+f\nbogus = x\n")
	write(filepath.Join(config, "jdrc"), "find = alt+g\n")
	write(filepath.Join(project, ".jdrc"), "tabs = ctrl+k\n")
	getenv := func(name string) string {
		if name == EnvConfigDir {
			return config
		}
		return ""
	}
	m, problems := Load(getenv, home, project)
	if m.Label(Tabs) != "ctrl+k" || m.Source(Tabs) != "./.jdrc:1" {
		t.Fatalf("the project file wins: %q %q", m.Label(Tabs), m.Source(Tabs))
	}
	if m.Label(Find) != "alt+g" {
		t.Fatalf("the config folder overrides the home file: %q", m.Label(Find))
	}
	if len(problems) != 1 || !strings.Contains(problems[0].String(), "~/.jdrc line 3: unknown command") {
		t.Fatalf("problems %v", problems)
	}
	clean, problems := Load(func(string) string { return "" }, t.TempDir(), t.TempDir())
	if len(problems) != 0 || !reflect.DeepEqual(clean.Keys(Tabs), []string{"shift+tab", "alt+t"}) {
		t.Fatalf("no file, no change: %v", problems)
	}
	same, problems := Load(func(string) string { return "" }, home, home) // the project is the home folder: read once
	if len(problems) != 1 || same.Label(Tabs) != "ctrl+j" {
		t.Fatalf("the same file is read once: %v", problems)
	}
}

func TestRender_ListsEveryCommandWithItsSourceAndParsesBackToTheSameMapping(t *testing.T) {
	a, _ := parse(t, "tabs = ctrl+j, alt+j\ndiff = none\nmerge.wrap = comma\nmerge.pane = hash\n")
	m, _ := Default().Apply(a, "~/.jdrc")
	text := m.Render()
	for _, want := range []string{"# ide:", "# merge:", "# diff:", "tabs", "= ctrl+j, alt+j", "# ~/.jdrc:1: switch tab", "diff", "= none", "= comma", "= hash", "# default: show the command palette"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output lacks %q:\n%s", want, text)
		}
	}
	for _, command := range Commands() {
		if !strings.Contains(text, string(command)+" ") {
			t.Errorf("%s is not listed", command)
		}
	}
	back, problems := Parse(strings.NewReader(text), "rc")
	if len(problems) != 0 {
		t.Fatalf("the output must be a valid file: %v", problems)
	}
	again, problems := Default().Apply(back, "rc")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	for _, command := range Commands() {
		if !reflect.DeepEqual(again.Keys(command), m.Keys(command)) {
			t.Errorf("%s: %v became %v", command, m.Keys(command), again.Keys(command))
		}
	}
}

func TestRender_WithoutAnyFileIsTheDefaults(t *testing.T) {
	text := Default().Render()
	if strings.Contains(text, "# ~/") || strings.Contains(text, "# ./") || !strings.Contains(text, "alt+p, f1") || strings.Count(text, "# default:") != len(definitions) {
		t.Fatalf("every command comes from the defaults:\n%s", text)
	}
}

func TestPrettyAndWritten(t *testing.T) {
	for in, want := range map[string]string{"right": "→", "left": "←", "backspace": "⌫", " ": "space", "pgdown": "pgdn", "alt+left": "alt+←", "ctrl+t": "ctrl+t", "ctrl+right": "ctrl+→"} {
		if got := Pretty(in); got != want {
			t.Errorf("Pretty(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{" ": "space", ",": "comma", "#": "hash", "alt+ ": "alt+space", "ctrl+t": "ctrl+t"} {
		if got := Written(in); got != want {
			t.Errorf("Written(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExampleFile_IsWhatJdRcPrintsWithoutAnyFile(t *testing.T) {
	want := Default().Render()
	got, err := os.ReadFile(filepath.Join("..", "..", "docs", "jdrc.example"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("docs/jdrc.example is out of date: regenerate it with `jd rc` on a machine without a .jdrc")
	}
}

func TestLoad_SkipsAFileThatIsNotARegularFile(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink("/dev/null", filepath.Join(home, ".jdrc")); err != nil {
		t.Skip("no symbolic links here")
	}
	m, problems := Load(func(string) string { return "" }, home, "")
	if len(problems) != 1 || !strings.Contains(problems[0].String(), "not a regular file") || m.Label(Tabs) != "shift+tab" {
		t.Fatalf("a link to a device is skipped with a message: %v", problems)
	}
}
