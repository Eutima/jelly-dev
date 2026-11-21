package lsp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func managerWith(installed ...string) *Manager {
	m := NewManager("/project", DefaultServers())
	m.lookPath = func(name string) (string, error) {
		for _, have := range installed {
			if have == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	return m
}

func TestKnownFormatters_AreUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range KnownFormatters() {
		if f.Name == "" || len(f.Command) == 0 || f.Detail == "" || len(f.Extensions) == 0 {
			t.Errorf("incomplete formatter %+v", f)
		}
		if seen[f.Name] {
			t.Errorf("%s is listed twice", f.Name)
		}
		seen[f.Name] = true
	}
}

func TestFormatterChoices_ListsTheOnesForTheFileFirstAndInstalledBeforeMissing(t *testing.T) {
	m := managerWith("black", "shfmt", "prettier")
	names := func(path string) []string {
		var out []string
		for _, c := range m.FormatterChoices(path) {
			out = append(out, c.Name)
		}
		return out
	}
	py := names("/project/app.py")
	if py[0] != "black" || py[1] != "ruff format" {
		t.Fatalf("for Python the Python formatters come first, the installed one before the missing one: %v", py[:3])
	}
	if sh := names("/project/run.sh"); sh[0] != "shfmt" {
		t.Fatalf("for a shell script shfmt comes first: %v", sh[:2])
	}
	if other := names("/project/notes.txt"); other[0] != "prettier" || other[1] != "black" { // made for nothing here: installed ones first
		t.Fatalf("without a match the installed ones come first: %v", other[:3])
	}
	for _, c := range m.FormatterChoices("/project/app.py") {
		if c.Installed != (c.Name == "black" || c.Name == "shfmt" || c.Name == "prettier") {
			t.Errorf("%s: installed flag %v", c.Name, c.Installed)
		}
	}
}

func TestFormatWith_RunsTheChosenFormatterAndRefusesAMissingOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses tr")
	}
	if _, err := exec.LookPath("tr"); err != nil {
		t.Skip("tr is not installed")
	}
	m := NewManager(t.TempDir(), nil)
	out, err := m.FormatWith(context.Background(), "/p/x.txt", "abc\n", FormatterChoice{Name: "upper", Command: []string{"tr", "a-z", "A-Z"}})
	if err != nil || out != "ABC\n" {
		t.Fatalf("the text goes through the formatter: %q %v", out, err)
	}
	_, err = m.FormatWith(context.Background(), "/p/x.txt", "abc", FormatterChoice{Name: "nope", Command: []string{"definitely-not-installed-formatter"}})
	if err == nil || !strings.Contains(err.Error(), "nope is not installed") {
		t.Fatalf("a missing formatter must be named: %v", err)
	}
	if _, err = m.FormatWith(context.Background(), "/p/x.txt", "abc", FormatterChoice{}); err == nil {
		t.Fatal("no formatter chosen is an error")
	}
}

func TestRememberFormatter_IsUsedByFormatAndSurvivesARestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses tr")
	}
	if _, err := exec.LookPath("tr"); err != nil {
		t.Skip("tr is not installed")
	}
	file := filepath.Join(t.TempDir(), "config", "formatters.json")
	first := NewManager(t.TempDir(), DefaultServers())
	if err := first.UseFormatterFile(file); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Format(context.Background(), "/p/notes.zzz", "abc\n"); !errors.Is(err, ErrNoServer) {
		t.Fatalf("a file type without server and without choice has no formatter: %v", err)
	}
	if err := first.RememberFormatter("/p/notes.ZZZ", FormatterChoice{Name: "upper", Command: []string{"tr", "a-z", "A-Z"}}); err != nil {
		t.Fatal(err)
	}
	if out, err := first.Format(context.Background(), "/p/other.zzz", "abc\n"); err != nil || out != "ABC\n" {
		t.Fatalf("the choice applies to every file of the type: %q %v", out, err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the choices are stored privately: %v %v", info, err)
	}
	second := NewManager(t.TempDir(), DefaultServers()) // a later session
	if err := second.UseFormatterFile(file); err != nil {
		t.Fatal(err)
	}
	if out, err := second.Format(context.Background(), "/p/again.zzz", "abc\n"); err != nil || out != "ABC\n" {
		t.Fatalf("the choice is still there: %q %v", out, err)
	}
	if err := second.RememberFormatter("/p/Makefile", FormatterChoice{Name: "x", Command: []string{"tr"}}); err == nil {
		t.Fatal("a file without extension cannot remember a formatter")
	}
}

func TestUseFormatterFile_ReportsABrokenFileAndIgnoresAMissingOne(t *testing.T) {
	m := NewManager("/p", nil)
	if err := m.UseFormatterFile(filepath.Join(t.TempDir(), "none.json")); err != nil {
		t.Fatalf("a missing file is fine: %v", err)
	}
	broken := filepath.Join(t.TempDir(), "formatters.json")
	if err := os.WriteFile(broken, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.UseFormatterFile(broken); err == nil || !strings.Contains(err.Error(), "formatters.json") {
		t.Fatalf("a broken file must be named: %v", err)
	}
	if kept, err := os.ReadFile(broken + ".bak"); err != nil || string(kept) != "{nope" {
		t.Fatalf("what the user wrote must be kept as a backup: %q %v", kept, err)
	}
}

func TestFormatWith_AFormatterThatPrintsNothingIsAnErrorNotAnEmptyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses true")
	}
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("true is not installed")
	}
	m := NewManager(t.TempDir(), nil)
	_, err := m.FormatWith(context.Background(), "/p/x.txt", "abc\n", FormatterChoice{Name: "silent", Command: []string{"true"}})
	if err == nil || !strings.Contains(err.Error(), "printed nothing") {
		t.Fatalf("empty output for a text with content must fail: %v", err)
	}
	if out, err := m.FormatWith(context.Background(), "/p/x.txt", "", FormatterChoice{Name: "silent", Command: []string{"true"}}); err != nil || out != "" {
		t.Fatalf("an empty text may stay empty: %q %v", out, err)
	}
}
