package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/eutima/jelly-diff/internal/ui"
)

func noUI(tea.Model) error { return errors.New("ui must not start") }

func testEnv(runUI uiRunner) environment {
	return environment{
		runUI:            runUI,
		readClipboard:    func() (string, error) { return "clip\n", nil },
		openGit:          openGit,
		getenv:           func(string) string { return "" },
		enableAttributes: func() {},
		busy:             noBusy,
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_ReportsNoDifferences_WhenFilesMatch(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "x\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{a, b}, &stdout, &stderr, testEnv(noUI)); code != exitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no differences") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestRun_Fails_WhenArgumentCountWrong(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"only-one"}, &bytes.Buffer{}, &stderr, testEnv(noUI)); code != exitUsage {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRun_Fails_WhenFileMissing(t *testing.T) {
	a := writeFile(t, "a", "x\n")
	var stderr bytes.Buffer
	if code := run([]string{a, "/nonexistent/file"}, &bytes.Buffer{}, &stderr, testEnv(noUI)); code != exitFailure {
		t.Fatalf("code %d", code)
	}
}

func TestRun_Fails_WhenFileIsBinary(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "x\x00y")
	var stderr bytes.Buffer
	if code := run([]string{a, b}, &bytes.Buffer{}, &stderr, testEnv(noUI)); code != exitFailure {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "binary") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRun_StartsUI_WhenFilesDiffer(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "y\n")
	started := false
	runUI := func(tea.Model) error { started = true; return nil }
	if code := run([]string{a, b}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(runUI)); code != exitOK || !started {
		t.Fatalf("code %d, started %v", code, started)
	}
}

func TestRun_Fails_WhenUIFails(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "y\n")
	var stderr bytes.Buffer
	runUI := func(tea.Model) error { return errors.New("boom") }
	if code := run([]string{a, b}, &bytes.Buffer{}, &stderr, testEnv(runUI)); code != exitFailure {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

// wideView renders the model in a terminal wide enough to show full temp-file paths.
func wideView(model ui.DiffModel) string {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 300, Height: 8})
	return updated.(ui.DiffModel).View()
}

func captureModel(got *ui.DiffModel) uiRunner {
	return func(model tea.Model) error {
		*got = model.(ui.DiffModel)
		return nil
	}
}

func TestRun_PutsClipboardOnRight_WhenClipFlagGiven(t *testing.T) {
	a := writeFile(t, "a", "file\n")
	var model ui.DiffModel
	if code := run([]string{"--clip", a}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(captureModel(&model))); code != exitOK {
		t.Fatalf("code %d", code)
	}
	view := wideView(model)
	if left, right := strings.Index(view, a), strings.Index(view, "clipboard"); left < 0 || right < left {
		t.Fatalf("clipboard should be on the right:\n%s", view)
	}
}

func TestRun_PutsClipboardOnLeft_WhenClipLeftFlagGiven(t *testing.T) {
	a := writeFile(t, "a", "file\n")
	var model ui.DiffModel
	if code := run([]string{"--clip-left", a}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(captureModel(&model))); code != exitOK {
		t.Fatalf("code %d", code)
	}
	view := wideView(model)
	if left, right := strings.Index(view, "clipboard"), strings.Index(view, a); left < 0 || right < left {
		t.Fatalf("clipboard should be on the left:\n%s", view)
	}
}

func TestRun_Fails_WhenClipboardUnavailable(t *testing.T) {
	a := writeFile(t, "a", "file\n")
	env := testEnv(noUI)
	env.readClipboard = func() (string, error) { return "", errors.New("no tool") }
	var stderr bytes.Buffer
	if code := run([]string{"--clip", a}, &bytes.Buffer{}, &stderr, env); code != exitFailure {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "no tool") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestRun_ReportsUsage_WhenClipFlagsConflictOrFileMissing(t *testing.T) {
	a := writeFile(t, "a", "file\n")
	for _, args := range [][]string{{"--clip", "--clip-left", a}, {"--clip"}, {"--clip", a, a}, {"--bogus"}, {a, "--clip"}} {
		if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(noUI)); code != exitUsage {
			t.Fatalf("args %v: code %d", args, code)
		}
	}
}

func TestRun_ReportsNoDifferences_WhenClipboardEqualsFile(t *testing.T) {
	a := writeFile(t, "a", "clip\n")
	var stdout bytes.Buffer
	if code := run([]string{"--clip", a}, &stdout, &bytes.Buffer{}, testEnv(noUI)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stdout.String(), "no differences") {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestRun_Fails_WhenClipFileMissingOrBinary(t *testing.T) {
	binary := writeFile(t, "bin", "x\x00y")
	for _, path := range []string{"/nonexistent/file", binary} {
		if code := run([]string{"--clip", path}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(noUI)); code != exitFailure {
			t.Fatalf("path %s: code %d", path, code)
		}
	}
}

// gitRepo creates a repository with one committed and one modified file and returns its path.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func gitEnv(dir string, runUI uiRunner) environment {
	env := testEnv(runUI)
	env.workDir = dir
	return env
}

func TestRun_ShowsChangedFiles_WhenGitSubcommandUsed(t *testing.T) {
	dir := gitRepo(t)
	var shown tea.Model
	runUI := func(model tea.Model) error { shown = model; return nil }
	if code := run([]string{"git"}, &bytes.Buffer{}, &bytes.Buffer{}, gitEnv(dir, runUI)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	model, ok := shown.(ui.GitModel)
	if !ok || model.FileCount() != 1 {
		t.Fatalf("unexpected model %#v", shown)
	}
}

func TestRun_ReportsNoChanges_WhenRepositoryClean(t *testing.T) {
	dir := gitRepo(t)
	var stdout bytes.Buffer
	if code := run([]string{"git", "--", "nothing-here"}, &stdout, &bytes.Buffer{}, gitEnv(dir, noUI)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stdout.String(), "no changes") {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestRun_Fails_WhenNotInGitRepository(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"git"}, &bytes.Buffer{}, &stderr, gitEnv(t.TempDir(), noUI)); code != exitFailure {
		t.Fatalf("code %d", code)
	}
}

func TestRun_Fails_WhenGitRevisionUnknown(t *testing.T) {
	dir := gitRepo(t)
	if code := run([]string{"git", "does-not-exist"}, &bytes.Buffer{}, &bytes.Buffer{}, gitEnv(dir, noUI)); code != exitFailure {
		t.Fatalf("code %d", code)
	}
}

func TestRun_ReportsUsage_WhenGitArgumentsInvalid(t *testing.T) {
	dir := gitRepo(t)
	for _, args := range [][]string{{"git", "a", "b"}, {"git", "--bogus"}} {
		if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}, gitEnv(dir, noUI)); code != exitUsage {
			t.Fatalf("args %v: code %d", args, code)
		}
	}
}

func TestPrintError_ReplacesControlCharacters_WhenMessageContainsEscapes(t *testing.T) {
	var out bytes.Buffer
	printError(&out, errors.New("bad \x1b]0;x\x07 path"))
	if strings.ContainsAny(out.String(), "\x1b\x07") || !strings.Contains(out.String(), "bad ") {
		t.Fatalf("output %q", out.String())
	}
}

func TestRun_Fails_WhenThemeInvalid(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "y\n")
	env := testEnv(noUI)
	env.getenv = func(key string) string {
		if key == "JELLY_DIFF_THEME" {
			return "neon"
		}
		return ""
	}
	var stderr bytes.Buffer
	if code := run([]string{a, b}, &bytes.Buffer{}, &stderr, env); code != exitFailure || !strings.Contains(stderr.String(), "neon") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestRun_UsesMonoThemeAndEnablesAttributes_WhenNoColorSet(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	a, b := writeFile(t, "a", "let foo = 1\n"), writeFile(t, "b", "let bar = 1\n")
	var shown ui.DiffModel
	enabled := false
	env := testEnv(captureModel(&shown))
	env.enableAttributes = func() { enabled = true }
	env.getenv = func(key string) string {
		if key == "NO_COLOR" {
			return "1"
		}
		return ""
	}
	if code := run([]string{a, b}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitOK || !enabled {
		t.Fatalf("code %d enabled %v", code, enabled)
	}
	updated, _ := shown.Update(tea.WindowSizeMsg{Width: 60, Height: 6})
	view := updated.(ui.DiffModel).View()
	if strings.Contains(view, "48;2") || !strings.Contains(view, "7m") {
		t.Fatalf("mono theme not applied: %q", view)
	}
}

func TestRun_DoesNotEnableAttributes_WhenThemeIsNotMono(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "y\n")
	env := testEnv(func(tea.Model) error { return nil })
	env.enableAttributes = func() { t.Fatal("must not be called") }
	if code := run([]string{a, b}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
}

func TestAttributeProfile_KeepsAttributes_WhenTerminalAndNoColor(t *testing.T) {
	cases := []struct {
		name     string
		current  termenv.Profile
		terminal bool
		term     string
		want     termenv.Profile
	}{
		{"no color on terminal", termenv.Ascii, true, "xterm-256color", termenv.ANSI},
		{"not a terminal", termenv.Ascii, false, "xterm", termenv.Ascii},
		{"dumb terminal", termenv.Ascii, true, "dumb", termenv.Ascii},
		{"no TERM", termenv.Ascii, true, "", termenv.Ascii},
		{"colors available", termenv.TrueColor, true, "xterm", termenv.TrueColor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := attributeProfile(c.current, c.terminal, c.term); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestInstallGit_Works_WhenThemeInvalid(t *testing.T) {
	env := testEnv(noUI)
	env.getenv = func(string) string { return "neon" }
	if code := run([]string{"install-git"}, &bytes.Buffer{}, &bytes.Buffer{}, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
}

func TestRun_ShowsChangedFiles_WhenTwoDirectoriesDiffer(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	for dir, content := range map[string]string{left: "old\n", right: "new\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var shown tea.Model
	runUI := func(model tea.Model) error { shown = model; return nil }
	if code := run([]string{left, right}, &bytes.Buffer{}, &bytes.Buffer{}, testEnv(runUI)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if model, ok := shown.(ui.GitModel); !ok || model.FileCount() != 1 {
		t.Fatalf("unexpected model %#v", shown)
	}
}

func TestRun_ReportsNoDifferences_WhenDirectoriesIdentical(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	var stdout bytes.Buffer
	if code := run([]string{left, right}, &stdout, &bytes.Buffer{}, testEnv(noUI)); code != exitOK ||
		!strings.Contains(stdout.String(), "no differences") {
		t.Fatalf("code %d stdout %q", code, stdout.String())
	}
}

func TestRun_Fails_WhenDirectoryComparedWithFile(t *testing.T) {
	dir, file := t.TempDir(), writeFile(t, "f", "x\n")
	var stderr bytes.Buffer
	if code := run([]string{dir, file}, &bytes.Buffer{}, &stderr, testEnv(noUI)); code != exitFailure ||
		!strings.Contains(stderr.String(), "directory with a file") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestRun_PrintsVersion_WhenVersionFlagGiven(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		var stdout bytes.Buffer
		if code := run([]string{arg}, &stdout, &bytes.Buffer{}, testEnv(noUI)); code != exitOK ||
			!strings.HasPrefix(stdout.String(), "jelly-diff ") {
			t.Fatalf("%s: code %d stdout %q", arg, code, stdout.String())
		}
	}
}

func TestPrintVersion_PrintsToolNameAndVersion_WhenCalled(t *testing.T) {
	var stdout bytes.Buffer
	printVersion(&stdout)
	if !strings.HasPrefix(stdout.String(), toolName+" ") || !strings.HasSuffix(stdout.String(), "\n") {
		t.Fatalf("output %q", stdout.String())
	}
}

func TestResolveVersion_PrefersLinkedThenModuleVersion_WhenAvailable(t *testing.T) {
	module := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	cases := []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		want   string
	}{
		{"linked wins", "0.9.0", module, "0.9.0"},
		{"module version", "dev", module, "v1.2.3"},
		{"empty linked", "", module, "v1.2.3"},
		{"devel build", "dev", devel, "dev"},
		{"no build info", "dev", nil, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveVersion(c.linked, c.info); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
