package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressKeys returns a UI runner that feeds the given keys to the merge model.
func pressKeys(keys ...string) uiRunner {
	return func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
		for _, key := range keys {
			model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
		return nil
	}
}

func mergeFixture(t *testing.T, base, local, remote string) (args []string, merged string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	merged = write("merged", "<<<<<<< markers\n")
	return []string{"merge", write("base", base), write("local", local), write("remote", remote), merged}, merged
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRun_WritesMergedFile_WhenConflictResolvedAndSaved(t *testing.T) {
	args, merged := mergeFixture(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("n", "t", "s")))
	if code != exitOK || readFile(t, merged) != "a\nTHEIRS\nc\n" {
		t.Fatalf("code %d merged %q", code, readFile(t, merged))
	}
}

func TestRun_FailsAndKeepsMergedFile_WhenNothingSaved(t *testing.T) {
	args, merged := mergeFixture(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	var stderr bytes.Buffer
	code := run(args, &bytes.Buffer{}, &stderr, testEnv(pressKeys("n", "t")))
	if code != exitFailure || readFile(t, merged) != "<<<<<<< markers\n" || !strings.Contains(stderr.String(), "not saved") {
		t.Fatalf("code %d merged %q stderr %q", code, readFile(t, merged), stderr.String())
	}
}

func TestRun_FailsAndKeepsMergedFile_WhenConflictUnresolvedAtSave(t *testing.T) {
	args, merged := mergeFixture(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("s")))
	if code != exitFailure || readFile(t, merged) != "<<<<<<< markers\n" {
		t.Fatalf("code %d merged %q", code, readFile(t, merged))
	}
}

func TestRun_KeepsCRLF_WhenLocalFileUsesCRLF(t *testing.T) {
	args, merged := mergeFixture(t, "a\r\nb\r\nc\r\n", "a\r\nOURS\r\nc\r\n", "a\r\nb\r\nc\r\n")
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("s")))
	if code != exitOK || readFile(t, merged) != "a\r\nOURS\r\nc\r\n" {
		t.Fatalf("code %d merged %q", code, readFile(t, merged))
	}
}

func TestRun_TreatsMissingBaseAsEmpty_WhenBothSidesAddFile(t *testing.T) {
	args, merged := mergeFixture(t, "", "x\n", "x\n")
	if err := os.Remove(args[1]); err != nil {
		t.Fatal(err)
	}
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("s")))
	if code != exitOK || readFile(t, merged) != "x\n" {
		t.Fatalf("code %d merged %q", code, readFile(t, merged))
	}
}

func TestRun_WritesWithoutUI_WhenNothingToMerge(t *testing.T) {
	args, merged := mergeFixture(t, "a\n", "a\n", "a\n")
	var stdout bytes.Buffer
	code := run(args, &stdout, &bytes.Buffer{}, testEnv(noUI))
	if code != exitOK || readFile(t, merged) != "a\n" || !strings.Contains(stdout.String(), "nothing to merge") {
		t.Fatalf("code %d merged %q", code, readFile(t, merged))
	}
}

func TestRun_Fails_WhenMergeInputIsBinaryOrUIFails(t *testing.T) {
	args, _ := mergeFixture(t, "a\n", "a\x00\n", "b\n")
	if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(noUI)); code != exitFailure {
		t.Fatalf("binary: code %d", code)
	}
	args, _ = mergeFixture(t, "a\nb\nc\n", "a\nX\nc\n", "a\nY\nc\n")
	failing := func(tea.Model) error { return errors.New("boom") }
	if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(failing)); code != exitFailure {
		t.Fatalf("ui failure: code %d", code)
	}
}

func TestRun_ReportsUsage_WhenMergeArgumentCountWrong(t *testing.T) {
	if code := run([]string{"merge", "a", "b"}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(noUI)); code != exitUsage {
		t.Fatalf("code %d", code)
	}
}

func TestWriteAtomic_KeepsFileMode_WhenReplacingExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 || readFile(t, path) != "new" {
		t.Fatalf("mode %v err %v", info.Mode(), err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".jelly-diff-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
}

func TestWriteAtomic_WritesThroughSymlink_WhenTargetIsLink(t *testing.T) {
	dir := t.TempDir()
	real, link := filepath.Join(dir, "real"), filepath.Join(dir, "link")
	if err := os.WriteFile(real, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 || readFile(t, real) != "new" {
		t.Fatalf("link replaced or content wrong: %v %v", info, err)
	}
}

func TestInstallGit_PrintsSettingsWithoutWriting_WhenApplyMissing(t *testing.T) {
	var stdout bytes.Buffer
	env := testEnv(noUI)
	env.gitConfig = func(string, string, string) error { return errors.New("must not be called") }
	if code := run([]string{"install-git"}, &stdout, &bytes.Buffer{}, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"git config --global merge.tool 'jelly-diff'", "mergetool.jelly-diff.cmd", "nothing changed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestInstallGit_WritesEverySetting_WhenApplyGiven(t *testing.T) {
	var keys []string
	env := testEnv(noUI)
	env.gitConfig = func(scope, key, value string) error {
		keys = append(keys, scope+" "+key+"="+value)
		return nil
	}
	if code := run([]string{"install-git", "--apply", "--local"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if len(keys) != len(gitSettings()) || !strings.HasPrefix(keys[0], "--local ") {
		t.Fatalf("keys %v", keys)
	}
}

func TestInstallGit_Fails_WhenGitConfigFailsOrArgumentUnknown(t *testing.T) {
	env := testEnv(noUI)
	env.gitConfig = func(string, string, string) error { return errors.New("boom") }
	if code := run([]string{"install-git", "--apply"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitFailure {
		t.Fatalf("failure: code %d", code)
	}
	if code := run([]string{"install-git", "--bogus"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitUsage {
		t.Fatalf("unknown: code %d", code)
	}
}

func TestRunGitConfig_StoresSettingsExactly_WhenAppliedToRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	config := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	env := testEnv(noUI)
	env.gitConfig = runGitConfig
	if code := run([]string{"install-git", "--apply"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
	for _, setting := range gitSettings() {
		out, err := exec.Command("git", "config", "--global", "--get", setting.key).Output()
		if err != nil || strings.TrimSpace(string(out)) != setting.value {
			t.Fatalf("%s = %q, err %v", setting.key, out, err)
		}
	}
}

// monokaiKeywordColor is the true-color escape of a keyword in the dark theme's monokai style.
const monokaiKeywordColor = "38;2;249;38;113"

func TestRun_HighlightsMergeView_WhenMergedFileIsGoSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	args := []string{"merge",
		write("base.go", "package a\nfunc f() {}\n"), write("local.go", "package a\nfunc g() {}\n"),
		write("remote.go", "package a\nfunc h() {}\n"), write("merged.go", "")}
	var view string
	runUI := func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
		view = model.View()
		return nil
	}
	env := testEnv(runUI)
	env.namedTheme = trueColorThemes
	if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitFailure && code != exitOK {
		t.Fatalf("unexpected exit code %d", code)
	}
	if view == "" {
		t.Fatal("the merge view was never shown")
	}
	if !strings.Contains(view, monokaiKeywordColor) {
		t.Fatalf("expected syntax colors in the merge view:\n%q", view)
	}
}
