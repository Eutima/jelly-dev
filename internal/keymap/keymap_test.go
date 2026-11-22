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
		{Tabs, []Sequence{{"ctrl+j"}}, 3}, {Terminal, []Sequence{{"alt+x"}, {"f4"}}, 4}, {Diff, nil, 5},
		{MergeOurs, []Sequence{{"alt+o"}}, 6}, {MergeWrap, []Sequence{{" "}}, 7},
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
		if got := m.Sequences(d.command); !reflect.DeepEqual(got, d.sequences()) {
			t.Errorf("%s: %v", d.command, got)
		}
		if m.Source(d.command) != "default" {
			t.Errorf("%s: source %q", d.command, m.Source(d.command))
		}
		for _, seq := range d.sequences() {
			if err := checkSequence(seq, d.scope); err != nil && d.scope != ScopeVim && d.scope != ScopeInsert {
				t.Errorf("%s: the default %s is not valid: %v", d.command, seq, err)
			}
			if len(seq) == 1 && (d.scope == ScopeIDE || d.scope == ScopeMerge || d.scope == ScopeDiff) {
				if got, ok := m.Lookup(d.scope, seq[0]); !ok || got != d.command {
					t.Errorf("%s: key %q looks up %q", d.command, seq[0], got)
				}
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
	write(filepath.Join(project, ".jdrc"), "tabs = ctrl+y\n")
	getenv := func(name string) string {
		if name == EnvConfigDir {
			return config
		}
		return ""
	}
	m, problems := Load(getenv, home, project)
	if m.Label(Tabs) != "ctrl+y" || m.Source(Tabs) != "./.jdrc:1" {
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
	a, _ := parse(t, "tabs = ctrl+j, alt+j\ndiff = none\nmerge.wrap = comma\nmerge.pane = hash\nvim.top = - t, ctrl+home\nvim.definition = space d\nvim.right = l, right\ninsert.complete = ctrl+space, f3\n")
	m, _ := Default().Apply(a, "~/.jdrc")
	text := m.Render()
	for _, want := range []string{"# ide:", "# merge:", "# diff:", "tabs", "= ctrl+j, alt+j", "# ~/.jdrc:1: switch tab", "diff", "= none", "= comma", "= hash", "= - t, ctrl+home", "= space d", "= ctrl+space, f3", "# default: show the command palette"} {
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
		if !reflect.DeepEqual(again.Sequences(command), m.Sequences(command)) {
			t.Errorf("%s: %v became %v", command, m.Sequences(command), again.Sequences(command))
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

func TestConflicts_AKeyTakenFromADefaultWithoutGivingThatCommandOtherKeysIsReported(t *testing.T) {
	a, _ := parse(t, "merge.ours = n\n")
	m, problems := Default().Apply(a, "~/.jdrc")
	if len(problems) != 0 {
		t.Fatal(problems) // Apply itself accepts it: jd rc still shows the mapping
	}
	conflicts := m.Conflicts()
	if len(conflicts) != 1 || !strings.Contains(conflicts[0].String(), "~/.jdrc line 1: merge.ours: n is the default key of merge.next, which would lose it") {
		t.Fatalf("conflicts: %v", conflicts)
	}
}

func TestConflicts_AreSettledByGivingTheOtherCommandKeysInAnyOrderOrInAnotherFile(t *testing.T) {
	for name, text := range map[string]string{
		"after":  "merge.ours = n\nmerge.next = tab\n",
		"before": "merge.next = tab\nmerge.ours = n\n",
		"none":   "merge.ours = n\nmerge.next = none\n",
		"swap":   "merge.ours = left\nmerge.theirs = right\n",
	} {
		a, problems := parse(t, text)
		m, more := Default().Apply(a, "f")
		if len(problems)+len(more) != 0 || len(m.Conflicts()) != 0 {
			t.Errorf("%s: problems %v %v conflicts %v", name, problems, more, m.Conflicts())
		}
	}
	home, _ := parse(t, "merge.ours = n\n")
	project, _ := parse(t, "merge.next = tab\n")
	m, _ := Default().Apply(home, "~/.jdrc")
	m, _ = m.Apply(project, "./.jdrc")
	if len(m.Conflicts()) != 0 {
		t.Fatalf("a later file settles it: %v", m.Conflicts())
	}
	moved, _ := parse(t, "merge.ours = right\n")
	m, _ = Default().Apply(home, "~/.jdrc")
	m, _ = m.Apply(moved, "./.jdrc")
	if conflicts := m.Conflicts(); len(conflicts) != 1 || !strings.Contains(conflicts[0].String(), "~/.jdrc line 1: merge.ours: n is the default key of merge.next") {
		t.Fatalf("the key moved on, but merge.next still lost it: %v", conflicts)
	}
}

func TestApply_ACommandSetTwiceInOneFileIsReported(t *testing.T) {
	a, _ := parse(t, "tabs = ctrl+j\ndiff = alt+x\ntabs = ctrl+k\n")
	m, problems := Default().Apply(a, "~/.jdrc")
	if len(problems) != 1 || !strings.Contains(problems[0].String(), "~/.jdrc line 3: tabs is set twice in this file (line 1 and line 3)") {
		t.Fatalf("problems: %v", problems)
	}
	if m.Label(Tabs) != "ctrl+j" {
		t.Fatalf("the first line stays: %q", m.Label(Tabs))
	}
}

func TestLoad_ReportsTheConflictsOfTheFiles(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".jdrc"), []byte("grep = ctrl+t\n"), 0o644); err != nil { // ctrl+t is the default of terminal
		t.Fatal(err)
	}
	_, problems := Load(func(string) string { return "" }, home, "")
	if len(problems) != 1 || !strings.Contains(problems[0].String(), "grep: ctrl+t is the default key of terminal") {
		t.Fatalf("problems: %v", problems)
	}
}

func TestParse_KeySequencesAreForTheVimCommandsOnly(t *testing.T) {
	got, problems := parse(t, "vim.top = g g, ctrl+home\nvim.definition = space d\n")
	if len(problems) != 0 || len(got) != 2 {
		t.Fatalf("%v %v", got, problems)
	}
	if want := []Sequence{{"g", "g"}, {"ctrl+home"}}; !reflect.DeepEqual(got[0].Keys, want) {
		t.Fatalf("vim.top: %v", got[0].Keys)
	}
	if want := (Sequence{" ", "d"}); !reflect.DeepEqual(got[1].Keys[0], want) {
		t.Fatalf("space is a key of a sequence: %v", got[1].Keys[0])
	}
	_, problems = parse(t, "tabs = ctrl+j ctrl+k\nmerge.next = n n\ninsert.tab = ctrl+x ctrl+y\n")
	if len(problems) != 3 {
		t.Fatalf("sequences outside the editor are refused: %v", problems)
	}
	for _, p := range problems {
		if !strings.Contains(p.String(), "only for the vim.* commands") {
			t.Errorf("%v", p)
		}
	}
}

func TestParse_RefusesTheEditorKeysThatCannotBeRebound(t *testing.T) {
	_, problems := parse(t, "vim.left = 3\nvim.right = esc\nvim.up = ctrl+c\nvim.down = ctrl+d\nvim.delete = 5 d\nvim.yank = g g, g\nvim.change = c, c\ninsert.tab = j\ninsert.newline = space\n")
	wants := []string{"count digit", "keeps it for itself", "keeps it for itself", "keeps it for itself", "count digit", "overlap", "listed twice", "typed text in insert mode", "typed text in insert mode"}
	if len(problems) != len(wants) {
		t.Fatalf("problems: %v", problems)
	}
	for i, want := range wants {
		if !strings.Contains(problems[i].String(), want) {
			t.Errorf("problem %d: %q lacks %q", i, problems[i], want)
		}
	}
	good, problems := parse(t, "insert.tab = alt+i\ninsert.deletetoend = ctrl+x\ninsert.complete = ctrl+space, f3\n")
	if len(problems) != 0 || good[2].Keys[0][0] != "ctrl+@" {
		t.Fatalf("ctrl+space is the NUL key of the terminal: %v %v", good, problems)
	}
}

func TestConflicts_AKeySequenceThatOverlapsADefaultTakesItAway(t *testing.T) {
	a, _ := parse(t, "vim.deletechar = g\n") // g starts g g, g d, g r, g t and g T
	m, problems := Default().Apply(a, "~/.jdrc")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	conflicts := m.Conflicts()
	if len(conflicts) != 5 || !strings.Contains(conflicts[0].String(), "vim.deletechar: g overlaps g g, a default key of vim.top") {
		t.Fatalf("conflicts: %v", conflicts)
	}
	fixed, _ := parse(t, "vim.deletechar = g\nvim.top = - t\nvim.definition = - d\nvim.references = - r\nvim.nexttab = - n\nvim.prevtab = - p\n")
	m, problems = Default().Apply(fixed, "~/.jdrc")
	if len(problems) != 0 || len(m.Conflicts()) != 0 {
		t.Fatalf("rebinding the five settles it: %v %v", problems, m.Conflicts())
	}
}

func TestConflicts_EditorAndInsertKeysThatTheProjectWindowHandlesFirstAreReported(t *testing.T) {
	a, _ := parse(t, "vim.undo = ctrl+t\ninsert.tab = shift+tab\ninsert.deletetoend = alt+d\n")
	m, _ := Default().Apply(a, "~/.jdrc")
	conflicts := m.Conflicts()
	// vim.undo = ctrl+t is the terminal key; insert.deletetoend = alt+d is the diff key; shift+tab in insert mode is left to the editor
	if len(conflicts) != 2 {
		t.Fatalf("conflicts: %v", conflicts)
	}
	for _, want := range []string{"~/.jdrc line 1: vim.undo: ctrl+t uses ctrl+t, which is the key of terminal in the project window", "~/.jdrc line 3: insert.deletetoend: alt+d uses alt+d, which is the key of diff in the project window"} {
		found := false
		for _, c := range conflicts {
			found = found || strings.Contains(c.String(), want)
		}
		if !found {
			t.Errorf("no conflict %q in %v", want, conflicts)
		}
	}
}

func TestEntriesCanonicalAndShadowed_DescribeWhatTheEditorHasToTranslate(t *testing.T) {
	a, _ := parse(t, "vim.left = ctrl+l\nvim.top = - t\n")
	m, _ := Default().Apply(a, "f")
	var left, top *Entry
	for _, e := range m.Entries(ScopeVim) {
		e := e
		switch e.Command {
		case VimLeft:
			left = &e
		case VimTop:
			top = &e
		}
	}
	if left == nil || !reflect.DeepEqual(left.Seq, Sequence{"ctrl+l"}) || !reflect.DeepEqual(left.Canonical, Sequence{"h"}) {
		t.Fatalf("vim.left: %+v", left)
	}
	if top == nil || !reflect.DeepEqual(top.Seq, Sequence{"-", "t"}) || !reflect.DeepEqual(top.Canonical, Sequence{"g", "g"}) {
		t.Fatalf("vim.top: %+v", top)
	}
	shadowed := m.Shadowed(ScopeVim)
	for _, want := range []Sequence{{"h"}, {"left"}, {"backspace"}, {"g", "g"}} {
		if !containsSeq(shadowed, want) {
			t.Errorf("%v must be shadowed: %v", want, shadowed)
		}
	}
	if containsSeq(shadowed, Sequence{"l"}) || containsSeq(shadowed, Sequence{"g", "d"}) {
		t.Fatalf("what is still bound is not shadowed: %v", shadowed)
	}
	if len(Default().Shadowed(ScopeVim)) != 0 || len(Default().Shadowed(ScopeInsert)) != 0 {
		t.Fatal("nothing is shadowed by default")
	}
}

func TestDefaults_OfTheEditorHaveNoConflictsAndEveryCommandHasACanonicalSequence(t *testing.T) {
	m := Default()
	if len(m.Conflicts()) != 0 {
		t.Fatalf("defaults: %v", m.Conflicts())
	}
	for _, d := range definitions {
		if (d.scope == ScopeVim || d.scope == ScopeInsert) && len(Canonical(d.command)) == 0 {
			t.Errorf("%s has no canonical sequence", d.command)
		}
	}
	for _, scope := range []Scope{ScopeVim, ScopeInsert} {
		entries := m.Entries(scope)
		for i, a := range entries {
			for _, b := range entries[i+1:] {
				if overlaps(a.Seq, b.Seq) {
					t.Errorf("%s: %s of %s overlaps %s of %s", scope, a.Seq, a.Command, b.Seq, b.Command)
				}
			}
		}
	}
}

func TestSequence_WrittenAndPretty(t *testing.T) {
	seq := Sequence{"g", "g"}
	if seq.String() != "g g" || seq.Pretty() != "gg" {
		t.Fatalf("%q %q", seq.String(), seq.Pretty())
	}
	seq = Sequence{" ", "d"}
	if seq.String() != "space d" || seq.Pretty() != "space d" {
		t.Fatalf("%q %q", seq.String(), seq.Pretty())
	}
	if (Sequence{"ctrl+@"}).String() != "ctrl+space" || Pretty("ctrl+@") != "ctrl+space" {
		t.Fatal("ctrl+@ is written ctrl+space")
	}
}

func TestConflicts_AWindowKeyInAnyPositionOfASequenceCouldNeverComplete(t *testing.T) {
	a, _ := parse(t, "vim.bottom = - alt+p\n")
	m, _ := Default().Apply(a, "~/.jdrc")
	conflicts := m.Conflicts()
	if len(conflicts) != 1 || !strings.Contains(conflicts[0].String(), "~/.jdrc line 1: vim.bottom: - alt+p uses alt+p, which is the key of palette in the project window") {
		t.Fatalf("conflicts: %v", conflicts)
	}
}

func TestConflicts_AWindowCommandThatTakesADefaultKeyOfTheEditorIsReportedAtTheWindowCommand(t *testing.T) {
	a, _ := parse(t, "find = ctrl+u\n") // ctrl+u is vim.halfup and insert.deletetostart by default
	m, _ := Default().Apply(a, "~/.jdrc")
	conflicts := m.Conflicts()
	if len(conflicts) != 2 {
		t.Fatalf("conflicts: %v", conflicts)
	}
	for _, want := range []string{"~/.jdrc line 1: find: ctrl+u is the default key of vim.halfup in the editor, which would stop working", "find: ctrl+u is the default key of insert.deletetostart in the editor"} {
		found := false
		for _, c := range conflicts {
			found = found || strings.Contains(c.String(), want)
		}
		if !found {
			t.Errorf("no conflict %q in %v", want, conflicts)
		}
	}
	fixed, _ := parse(t, "find = ctrl+u\nvim.halfup = alt+u\ninsert.deletetostart = alt+x\n")
	m, _ = Default().Apply(fixed, "~/.jdrc")
	if len(m.Conflicts()) != 0 {
		t.Fatalf("settled: %v", m.Conflicts())
	}
}
