package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/ui"
)

func rcOutput(t *testing.T, home, workDir string) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := environment{getenv: func(string) string { return "" }, homeDir: home, workDir: workDir}
	code := run([]string{"rc"}, &stdout, &stderr, env)
	return stdout.String(), code
}

func TestRC_PrintsTheDefaultsWhenNothingIsChanged(t *testing.T) {
	out, code := rcOutput(t, t.TempDir(), t.TempDir())
	if code != exitOK || !strings.Contains(out, "palette") || !strings.Contains(out, "= alt+p, f1") || !strings.Contains(out, "# default: show the command palette") {
		t.Fatalf("code %d:\n%s", code, out)
	}
	if strings.Contains(out, "# ~/") || strings.Contains(out, "Lines that were not used") {
		t.Fatalf("nothing is overridden:\n%s", out)
	}
	if strings.Count(out, "# default:") != len(keymap.Commands()) {
		t.Fatal("every command is listed with its default")
	}
}

func TestRC_ShowsWhatTheHomeAndProjectFilesChangeAndTheirProblems(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".jdrc"), []byte("tabs = ctrl+j\nbogus = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".jdrc"), []byte("diff = none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := rcOutput(t, home, project)
	for _, want := range []string{"= ctrl+j", "# ~/.jdrc:1: switch tab", "= none", "# ./.jdrc:1: show the git diff", "# ~/.jdrc line 2: unknown command"} {
		if code != exitOK || !strings.Contains(out, want) {
			t.Fatalf("the output lacks %q (code %d):\n%s", want, code, out)
		}
	}
}

func TestRC_RejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := environment{getenv: func(string) string { return "" }}
	if code := run([]string{"rc", "--defaults"}, &stdout, &stderr, env); code != exitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestKeysProjectDir_IsTheFolderOrTheFolderOfTheFileOrTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for args, want := range map[string]string{dir: dir, file: dir, "/does/not/exist": "work"} {
		if got := keysProjectDir([]string{args}, "work"); got != want {
			t.Errorf("%q: %q, want %q", args, got, want)
		}
	}
	if got := keysProjectDir(nil, "work"); got != "work" {
		t.Fatalf("no argument: %q", got)
	}
}

// brokenHome is a home folder whose .jdrc has a conflict (grep takes the default key of terminal) and an unknown command.
func brokenHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".jdrc"), []byte("grep = ctrl+t\nbogus = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestValidator_JdDoesNotStartWithAKeyMappingThatHasProblems(t *testing.T) {
	var stdout, stderr bytes.Buffer
	env := environment{
		getenv: func(string) string { return "" }, homeDir: brokenHome(t), workDir: t.TempDir(),
		runUI: func(tea.Model) error { t.Fatal("the UI must not start"); return nil },
	}
	for _, args := range [][]string{nil, {t.TempDir()}, {"git"}, {"merge", "a", "b"}, {"a", "b"}} {
		stderr.Reset()
		if code := run(args, &stdout, &stderr, env); code != exitUsage {
			t.Fatalf("%v: exit code %d, want %d", args, code, exitUsage)
		}
		for _, want := range []string{"jd: ~/.jdrc line 1: grep: ctrl+t is the default key of terminal", "jd: ~/.jdrc line 2: unknown command", "2 problem(s)", "jd rc"} {
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("%v: the message lacks %q:\n%s", args, want, stderr.String())
			}
		}
	}
}

func TestValidator_DoesNotBlockRcVersionHelpAndSetup(t *testing.T) {
	env := environment{getenv: func(string) string { return "" }, homeDir: brokenHome(t), workDir: t.TempDir(), namedTheme: ui.NamedTheme}
	for _, args := range [][]string{{"rc"}, {"--version"}, {"-h"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, env); code != exitOK {
			t.Errorf("%v: exit code %d\n%s", args, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	run([]string{"rc"}, &stdout, &stderr, env)
	if !strings.Contains(stdout.String(), "# Problems (jd does not start until they are fixed") || !strings.Contains(stdout.String(), "# ~/.jdrc line 1: grep: ctrl+t is the default key of terminal") {
		t.Fatalf("jd rc lists the problems:\n%s", stdout.String())
	}
}

func TestValidator_AFixedFileLetsJdStart(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".jdrc"), []byte("grep = ctrl+t\nterminal = alt+x\n"), 0o644); err != nil { // the terminal gets another key: settled
		t.Fatal(err)
	}
	env := environment{
		getenv: func(string) string { return "" }, homeDir: home, workDir: t.TempDir(), namedTheme: ui.NamedTheme,
		runUI: func(tea.Model) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	run([]string{"--clip", "x"}, &stdout, &stderr, env) // fails later (no clipboard), but the validator let it through
	if strings.Contains(stderr.String(), "problem(s)") {
		t.Fatalf("no problems, jd goes on:\n%s", stderr.String())
	}
}

func TestValidator_RefusesAVimKeyThatOverlapsADefaultAndShowsItInRc(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".jdrc"), []byte("vim.deletechar = g\ninsert.tab = j\nvim.left = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := environment{getenv: func(string) string { return "" }, homeDir: home, workDir: t.TempDir(), namedTheme: ui.NamedTheme,
		runUI: func(tea.Model) error { t.Fatal("the UI must not start"); return nil }}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, env); code != exitUsage {
		t.Fatalf("exit code %d", code)
	}
	for _, want := range []string{"insert.tab: j is typed text in insert mode", "vim.left: 3: a count digit", "vim.deletechar: g overlaps g g, a default key of vim.top"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("the message lacks %q:\n%s", want, stderr.String())
		}
	}
	stdout.Reset()
	if code := run([]string{"rc"}, &stdout, &stderr, env); code != exitOK || !strings.Contains(stdout.String(), "vim.deletechar") || !strings.Contains(stdout.String(), "# Problems") {
		t.Fatalf("jd rc shows the mapping and the problems:\n%s", stdout.String())
	}
}

func TestRC_PrintsTheVimAndInsertCommandsWithSequences(t *testing.T) {
	out, code := rcOutput(t, t.TempDir(), t.TempDir())
	for _, want := range []string{"# vim: the editor's normal and visual mode", "= g g", "= g d", "# insert: the editor's insert mode", "= ctrl+space, ctrl+n"} {
		if code != exitOK || !strings.Contains(out, want) {
			t.Fatalf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestValidator_HelpAnywhereOnTheCommandLineNeedsNoValidMapping(t *testing.T) {
	env := environment{getenv: func(string) string { return "" }, homeDir: brokenHome(t), workDir: t.TempDir(), namedTheme: ui.NamedTheme,
		runUI: func(tea.Model) error { return nil }}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"git", "--help"}, &stdout, &stderr, env); code == exitUsage && strings.Contains(stderr.String(), "problem(s)") {
		t.Fatalf("--help after a subcommand must not be blocked by the validator:\n%s", stderr.String())
	}
	if !isHelp([]string{"merge", "-h"}) || isHelp([]string{"merge", "a", "b"}) || !isHelp([]string{"help"}) || isHelp([]string{"x", "help"}) {
		t.Fatal("isHelp")
	}
}
