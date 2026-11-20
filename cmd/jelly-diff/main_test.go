package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-diff/internal/ui"
)

func noUI(tea.Model) error { return errors.New("ui must not start") }

func testEnv(runUI uiRunner) environment {
	return environment{runUI: runUI, readClipboard: func() (string, error) { return "clip\n", nil }}
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
	env := environment{runUI: noUI, readClipboard: func() (string, error) { return "", errors.New("no tool") }}
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
