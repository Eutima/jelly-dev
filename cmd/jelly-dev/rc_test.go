package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eutima/jelly-dev/internal/keymap"
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
