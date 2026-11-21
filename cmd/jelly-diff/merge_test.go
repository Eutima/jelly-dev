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
	if code := run([]string{"merge", "a", "b", "c"}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(noUI)); code != exitUsage {
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
	for _, want := range []string{"git config --global merge.tool 'jd'", "mergetool.jd.cmd", "nothing changed"} {
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

func pairFixture(t *testing.T, a, b string) (args []string, aPath string) {
	t.Helper()
	dir := t.TempDir()
	aPath, bPath := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for path, content := range map[string]string{aPath: a, bPath: b} {
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"merge", aPath, bPath}, aPath
}

func TestRun_WritesResultIntoFileA_WhenTheirsHunkTakenAndSaved(t *testing.T) {
	args, a := pairFixture(t, "1\nold\n3\n", "1\nnew\n3\n")
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("n", "t", "s")))
	if code != exitOK || readFile(t, a) != "1\nnew\n3\n" {
		t.Fatalf("code %d a %q", code, readFile(t, a))
	}
}

func TestRun_KeepsFileAContent_WhenAllOursTaken(t *testing.T) {
	args, a := pairFixture(t, "1\nold\n3\nx\n", "1\nnew\n3\n")
	code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
		return nil
	}))
	if code != exitOK || readFile(t, a) != "1\nold\n3\nx\n" {
		t.Fatalf("code %d a %q", code, readFile(t, a))
	}
}

func TestRun_FailsAndLeavesFileAUntouched_WhenNothingSaved(t *testing.T) {
	args, a := pairFixture(t, "1\nold\n", "1\nnew\n")
	var stderr bytes.Buffer
	code := run(args, &bytes.Buffer{}, &stderr, testEnv(pressKeys("n", "t")))
	if code != exitFailure || readFile(t, a) != "1\nold\n" || !strings.Contains(stderr.String(), "not saved") {
		t.Fatalf("code %d a %q err %q", code, readFile(t, a), stderr.String())
	}
}

func TestRun_ReportsNoDifferencesAndWritesNothing_WhenFilesAreIdentical(t *testing.T) {
	args, a := pairFixture(t, "same\r\n", "same\r\n")
	var stdout bytes.Buffer
	code := run(args, &stdout, &bytes.Buffer{}, testEnv(func(tea.Model) error { t.Fatal("no UI expected"); return nil }))
	if code != exitOK || !strings.Contains(stdout.String(), "no differences") || readFile(t, a) != "same\r\n" {
		t.Fatalf("code %d out %q", code, stdout.String())
	}
}

func TestRun_KeepsCRLFOfFileA_WhenSaving(t *testing.T) {
	args, a := pairFixture(t, "1\r\nold\r\n", "1\nnew\n")
	if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(pressKeys("n", "t", "s"))); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if got := readFile(t, a); got != "1\r\nnew\r\n" {
		t.Fatalf("a %q", got)
	}
}

func TestRun_RefusesToMergeDirectoryWithFile(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, "f", "x\n")
	var stderr bytes.Buffer
	if code := run([]string{"merge", dir, file}, &bytes.Buffer{}, &stderr, testEnv(noUI)); code != exitFailure ||
		!strings.Contains(stderr.String(), "directory with a file") {
		t.Fatalf("code %d err %q", code, stderr.String())
	}
}

func dirFixture(t *testing.T) (aDir, bDir string) {
	t.Helper()
	root := t.TempDir()
	aDir, bDir = filepath.Join(root, "a"), filepath.Join(root, "b")
	files := map[string]string{
		"a/both.txt": "1\nold\n3\n", "b/both.txt": "1\nnew\n3\n",
		"a/same.txt": "same\n", "b/same.txt": "same\n",
		"a/only_a.txt":     "keep me\n",
		"b/sub/only_b.txt": "from b\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return aDir, bDir
}

// dirKeys feeds keys to the directory merge model: "enter" is the enter key, other strings are typed runes.
func dirKeys(keys ...string) uiRunner {
	return func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
		for _, key := range keys {
			switch key {
			case "enter":
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			default:
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			}
		}
		return nil
	}
}

func TestRun_MergesFileIntoDirectoryA_WhenOpenedTakenAndSaved(t *testing.T) {
	aDir, bDir := dirFixture(t)
	// the list is sorted: both.txt, only_a.txt, sub/only_b.txt
	code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(dirKeys("enter", "n", "t", "s", "q")))
	if code != exitOK || readFile(t, filepath.Join(aDir, "both.txt")) != "1\nnew\n3\n" {
		t.Fatalf("code %d both.txt %q", code, readFile(t, filepath.Join(aDir, "both.txt")))
	}
	if readFile(t, filepath.Join(bDir, "both.txt")) != "1\nnew\n3\n" {
		t.Fatal("directory b must never change")
	}
}

func TestRun_CreatesFileInDirectoryA_WhenFileExistsOnlyInB(t *testing.T) {
	aDir, bDir := dirFixture(t)
	code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(dirKeys("G", "enter", "n", "t", "s", "q")))
	if got := readFile(t, filepath.Join(aDir, "sub", "only_b.txt")); code != exitOK || got != "from b\n" {
		t.Fatalf("code %d created file %q", code, got)
	}
}

func TestRun_KeepsFileOnlyInA_WhenOursTakenForIt(t *testing.T) {
	aDir, bDir := dirFixture(t)
	code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(dirKeys("j", "enter", "n", "o", "s", "q")))
	if got := readFile(t, filepath.Join(aDir, "only_a.txt")); code != exitOK || got != "keep me\n" {
		t.Fatalf("code %d only_a.txt %q", code, got)
	}
}

func TestRun_LeavesDirectoryAUntouched_WhenNothingSaved(t *testing.T) {
	aDir, bDir := dirFixture(t)
	code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(dirKeys("enter", "n", "t", "q", "q", "q")))
	if got := readFile(t, filepath.Join(aDir, "both.txt")); code != exitOK || got != "1\nold\n3\n" {
		t.Fatalf("code %d both.txt %q", code, got)
	}
}

func TestRun_ReportsNoDifferences_WhenDirectoriesMatch(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	code := run([]string{"merge", a, b}, &stdout, &bytes.Buffer{}, testEnv(func(tea.Model) error { t.Fatal("no UI expected"); return nil }))
	if code != exitOK || !strings.Contains(stdout.String(), "no differences") {
		t.Fatalf("code %d out %q", code, stdout.String())
	}
}

// scriptedRun feeds keys to the UI; after the key at index step it calls between (to change files while the tool runs).
func scriptedRun(keys []string, step int, between func(), view *string) uiRunner {
	return func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
		for i, key := range keys {
			if key == "enter" {
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			} else {
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			}
			if i == step && between != nil {
				between()
			}
		}
		*view = model.View()
		return nil
	}
}

func TestRun_DoesNotOverwriteFileInA_WhenFileOnlyInBWasSavedAndIsOpenedAgain(t *testing.T) {
	root := t.TempDir()
	aDir, bDir := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, dir := range []string{aDir, bDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	aFile := filepath.Join(aDir, "only_b.txt")
	if err := os.WriteFile(filepath.Join(bDir, "only_b.txt"), []byte("from b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var view string
	keys := []string{"enter", "n", "t", "s", "q", "enter", "n", "o", "s", "q"}
	changeA := func() { // after the first save the file exists in a; someone edits it while the list still says "only in b"
		if err := os.WriteFile(aFile, []byte("changed in a\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(scriptedRun(keys, 4, changeA, &view)))
	if got := readFile(t, aFile); code != exitOK || got != "changed in a\n" {
		t.Fatalf("code %d a %q: the existing file in a must be merged, not treated as empty", code, got)
	}
}

func TestRun_ListsErrorAndContinues_WhenFileIsBinaryOrOnlyLineEndingsDiffer(t *testing.T) {
	root := t.TempDir()
	aDir, bDir := filepath.Join(root, "a"), filepath.Join(root, "b")
	files := map[string]string{
		"a/bin.dat": "x\n", "b/bin.dat": "x\x00\x01\n",
		"a/eol.txt": "same\r\n", "b/eol.txt": "same\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var view string
	// sorted: bin.dat, eol.txt
	if code := run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(scriptedRun([]string{"enter"}, -1, nil, &view))); code != exitOK || !strings.Contains(view, "Cannot open bin.dat") {
		t.Fatalf("code %d view:\n%s", code, view)
	}
	if run([]string{"merge", aDir, bDir}, &bytes.Buffer{}, &bytes.Buffer{},
		testEnv(scriptedRun([]string{"j", "enter"}, -1, nil, &view))); !strings.Contains(view, "no line differences") {
		t.Fatalf("view:\n%s", view)
	}
}
