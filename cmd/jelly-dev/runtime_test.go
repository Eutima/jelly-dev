package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// withoutNpm runs a test with a PATH that has no npm, so the installer has something to do.
func withoutNpm(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func noEnv(string) string { return "" }

func TestRuntimeInstaller_RunsTheScriptForNodeOnly(t *testing.T) {
	withoutNpm(t)
	var calls int
	var gotEnv []string
	run := func(jdBin string, extraEnv []string, _ io.Reader, _, _ io.Writer) error {
		calls++
		gotEnv = extraEnv
		if jdBin == "" {
			t.Error("the script needs the path of jd")
		}
		return nil
	}
	install := runtimeInstaller(run, noEnv, time.Now)
	err := install(t.Context(), "node")
	if calls != 1 {
		t.Fatalf("the installer ran %d times", calls)
	}
	for _, want := range []string{"JD_DEPS=runtimes", "JD_LANGS=javascript", "JD_SKIP_GIT_CONFIG=1"} {
		if !strings.Contains(strings.Join(gotEnv, " "), want) {
			t.Errorf("the script lacks %s: %v", want, gotEnv)
		}
	}
	if err == nil || !strings.Contains(err.Error(), "npm is still not on the PATH") {
		t.Fatalf("the script ran but npm is not there (the PATH has nothing): %v", err)
	}
}

func TestRuntimeInstaller_RefusesOtherRuntimes_AndASkippedInstaller(t *testing.T) {
	withoutNpm(t)
	run := func(string, []string, io.Reader, io.Writer, io.Writer) error { t.Fatal("must not run"); return nil }
	if err := runtimeInstaller(run, noEnv, time.Now)(t.Context(), "go"); err == nil {
		t.Error("only Node.js is installed")
	}
	skip := func(key string) string {
		if key == envSkipDeps {
			return "1"
		}
		return ""
	}
	if err := runtimeInstaller(run, skip, time.Now)(t.Context(), "node"); err == nil || !strings.Contains(err.Error(), envSkipDeps) {
		t.Errorf("JD_SKIP_DEPS must say why nothing is installed: %v", err)
	}
}

func TestRuntimeInstaller_ReportsAFailureOnce_AndTriesAgainLater(t *testing.T) {
	withoutNpm(t)
	var calls int
	run := func(_ string, _ []string, _ io.Reader, stdout, _ io.Writer) error {
		calls++
		_, _ = stdout.Write([]byte("download failed: no network"))
		return errors.New("exit status 1")
	}
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	install := runtimeInstaller(run, noEnv, func() time.Time { return clock })
	first := install(t.Context(), "node")
	if first == nil || !strings.Contains(first.Error(), "no network") {
		t.Fatalf("the error carries the output of the script: %v", first)
	}
	if install(t.Context(), "node") == nil || calls != 1 {
		t.Fatalf("the next tool of the same run must not download again (calls %d)", calls)
	}
	clock = clock.Add(failureMemory + time.Second)
	_ = install(t.Context(), "node")
	if calls != 2 {
		t.Fatalf("a later try runs the script again (calls %d)", calls)
	}
}

func TestRuntimeInstaller_SucceedsWithoutTheScript_WhenNpmIsThere(t *testing.T) {
	// a PATH with a fake npm
	dir := t.TempDir()
	writeExecutable(t, dir, "npm")
	t.Setenv("PATH", dir)
	run := func(string, []string, io.Reader, io.Writer, io.Writer) error {
		t.Fatal("npm is there already")
		return nil
	}
	if err := runtimeInstaller(run, noEnv, time.Now)(t.Context(), "node"); err != nil {
		t.Fatal(err)
	}
}

func TestOutputNote_KeepsTheEndOfALongOutput(t *testing.T) {
	long := strings.Repeat("x", 2000) + "THE END"
	if got := outputNote(long); !strings.HasSuffix(got, "THE END") || len(got) > outputTail+10 {
		t.Fatalf("note %d bytes", len(got))
	}
	if outputNote("  \n") != "" {
		t.Fatal("no output, no note")
	}
}
