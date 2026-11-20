package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func noUI(tea.Model) error { return errors.New("ui must not start") }

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_ReportsIdentical_WhenFilesMatch(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "x\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{a, b}, &stdout, &stderr, noUI); code != exitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "identical") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestRun_Fails_WhenArgumentCountWrong(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"only-one"}, &bytes.Buffer{}, &stderr, noUI); code != exitFailure {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRun_Fails_WhenFileMissing(t *testing.T) {
	a := writeFile(t, "a", "x\n")
	var stderr bytes.Buffer
	if code := run([]string{a, "/nonexistent/file"}, &bytes.Buffer{}, &stderr, noUI); code != exitFailure {
		t.Fatalf("code %d", code)
	}
}

func TestRun_Fails_WhenFileIsBinary(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "x\x00y")
	var stderr bytes.Buffer
	if code := run([]string{a, b}, &bytes.Buffer{}, &stderr, noUI); code != exitFailure {
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
	if code := run([]string{a, b}, &bytes.Buffer{}, &bytes.Buffer{}, runUI); code != exitOK || !started {
		t.Fatalf("code %d, started %v", code, started)
	}
}

func TestRun_Fails_WhenUIFails(t *testing.T) {
	a, b := writeFile(t, "a", "x\n"), writeFile(t, "b", "y\n")
	var stderr bytes.Buffer
	runUI := func(tea.Model) error { return errors.New("boom") }
	if code := run([]string{a, b}, &bytes.Buffer{}, &stderr, runUI); code != exitFailure {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}
