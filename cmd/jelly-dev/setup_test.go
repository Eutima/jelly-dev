package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installerFake records what the installer script was started for.
type installerFake struct {
	calls []string
	err   error
}

func (f *installerFake) env(interactive bool, skip string) environment {
	env := testEnv(noUI)
	env.getenv = func(name string) string {
		if name == envSkipSetup {
			return skip
		}
		return ""
	}
	env.interactive = func() bool { return interactive }
	env.runInstaller = func(jdBin string, _ io.Reader, _, _ io.Writer) error {
		f.calls = append(f.calls, jdBin)
		return f.err
	}
	return env
}

func TestSetupOnFirstStart_RunsTheInstallerOnceAndLeavesAMarker(t *testing.T) {
	dir, fake := t.TempDir(), &installerFake{}
	var stdout, stderr bytes.Buffer
	setupOnFirstStart(dir, &stdout, &stderr, fake.env(true, ""))
	if len(fake.calls) != 1 || !filepath.IsAbs(fake.calls[0]) {
		t.Fatalf("the installer must run once with the path of jd: %v", fake.calls)
	}
	for _, want := range []string{"first start", envSkipSetup, "jd setup"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("the notice lacks %q: %q", want, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, setupMarker)); err != nil {
		t.Fatal("the first start must leave a marker")
	}
	setupOnFirstStart(dir, &stdout, &stderr, fake.env(true, ""))
	if len(fake.calls) != 1 {
		t.Fatalf("a later start must not run it again: %v", fake.calls)
	}
}

func TestSetupOnFirstStart_StaysOutOfPipesAndCanBeSkipped(t *testing.T) {
	dir, fake := t.TempDir(), &installerFake{}
	setupOnFirstStart(dir, io.Discard, io.Discard, fake.env(false, "")) // no terminal: a script, CI, git calling jd
	setupOnFirstStart(dir, io.Discard, io.Discard, fake.env(true, "1")) // JD_SKIP_SETUP=1
	setupOnFirstStart(dir, io.Discard, io.Discard, testEnv(noUI))       // an environment without the hooks
	if len(fake.calls) != 0 {
		t.Fatalf("the installer must not run: %v", fake.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, setupMarker)); err == nil {
		t.Fatal("a start that did not run it must not leave a marker")
	}
}

func TestSetupOnFirstStart_ADryRunKeepsTheFirstStartForTheRealRun(t *testing.T) {
	dir, fake := t.TempDir(), &installerFake{}
	env := fake.env(true, "")
	env.getenv = func(name string) string {
		if name == "JD_DRY_RUN" {
			return "1"
		}
		return ""
	}
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if len(fake.calls) != 1 {
		t.Fatalf("the dry run itself runs the script: %v", fake.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, setupMarker)); err == nil {
		t.Fatal("a dry run installs nothing: it must not use up the first start")
	}
}

func TestRunIDE_AMistypedFolderFailsBeforeTheInstallerRuns(t *testing.T) {
	fake := &installerFake{}
	env := fake.env(true, "")
	env.getenv = func(name string) string {
		if name == "JD_CONFIG_DIR" {
			return t.TempDir()
		}
		return ""
	}
	var stderr bytes.Buffer
	if code := run([]string{filepath.Join(t.TempDir(), "no", "such", "dir", "file.txt")}, io.Discard, &stderr, env); code != exitFailure {
		t.Fatalf("a path that cannot be opened must fail: %d", code)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("nothing may be installed for a mistyped path: %v", fake.calls)
	}
}

func TestSetupOnFirstStart_FailureSaysHowToRetryAndDoesNotNag(t *testing.T) {
	dir, fake := t.TempDir(), &installerFake{err: errors.New("exit status 1")}
	var stderr bytes.Buffer
	setupOnFirstStart(dir, io.Discard, &stderr, fake.env(true, ""))
	if !strings.Contains(stderr.String(), "failed") || !strings.Contains(stderr.String(), "jd setup") {
		t.Fatalf("a failure must say how to run it again: %q", stderr.String())
	}
	setupOnFirstStart(dir, io.Discard, io.Discard, fake.env(true, ""))
	if len(fake.calls) != 1 {
		t.Fatalf("it must not run at every start: %v", fake.calls)
	}
}

func TestRun_SetupRunsTheInstallerAlways(t *testing.T) {
	fake := &installerFake{}
	var stdout, stderr bytes.Buffer
	for i := 1; i <= 2; i++ {
		if code := run([]string{"setup"}, &stdout, &stderr, fake.env(false, "")); code != exitOK || len(fake.calls) != i {
			t.Fatalf("jd setup must run it every time: code %d calls %v %s", code, fake.calls, stderr.String())
		}
	}
	fake.err = errors.New("exit status 1")
	if code := run([]string{"setup"}, &stdout, &stderr, fake.env(false, "")); code != exitFailure {
		t.Fatalf("a failing installer must fail jd setup: %d", code)
	}
}

// The real embedded script, started the way jd starts it: it must configure git with the installed jd, and neither download
// jd nor touch the PATH settings of the shell.
func TestRunInstallerScript_ConfiguresGitWithTheInstalledJdOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covers install.sh")
	}
	for _, tool := range []string{"sh", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	home, bin := t.TempDir(), t.TempDir()
	log := filepath.Join(bin, "calls")
	fakeJd := filepath.Join(bin, "jd")
	if err := os.WriteFile(fakeJd, []byte("#!/bin/sh\necho \"$@\" >> '"+log+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("JD_SKIP_DEPS", "1")
	t.Setenv("SHELL", "/bin/zsh")
	var stdout, stderr bytes.Buffer
	if err := runInstallerScript(fakeJd, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s%s", err, stdout.String(), stderr.String())
	}
	calls, _ := os.ReadFile(log)
	if strings.TrimSpace(string(calls)) != "install-git --apply" {
		t.Fatalf("the installer must call the installed jd to set git up: %q\n%s%s", calls, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "Downloading") {
		t.Fatalf("jd is installed already, nothing is downloaded:\n%s", stdout.String())
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("the installer must not write shell profiles or tools here: %v", entries)
	}
}
