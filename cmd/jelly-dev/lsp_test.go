package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eutima/jelly-dev/internal/tools"
)

// toolsFake records what jd install-lsp installs.
type toolsFake struct {
	installed        map[string]bool
	fail             map[string]error
	asked            [][]string
	updatedLanguages [][]string
}

func (f *toolsFake) IsInstalled(tool string) bool { return f.installed[tool] }

func (f *toolsFake) UpdateLanguages(_ context.Context, languages []string, progress func(string, bool, error)) ([]tools.InstallResult, error) {
	f.updatedLanguages = append(f.updatedLanguages, languages)
	var results []tools.InstallResult
	for _, name := range tools.ToolsFor(languages) {
		progress(name, false, nil)
		err := f.fail[name]
		progress(name, true, err)
		results = append(results, tools.InstallResult{Tool: name, Updated: f.installed[name] && err == nil, Err: err})
	}
	return results, nil
}

func (f *toolsFake) InstallLanguages(_ context.Context, languages []string, progress func(string, bool, error)) ([]tools.InstallResult, error) {
	f.asked = append(f.asked, languages)
	var results []tools.InstallResult
	for _, name := range tools.ToolsFor(languages) {
		if f.installed[name] {
			results = append(results, tools.InstallResult{Tool: name, Already: true})
			continue
		}
		progress(name, false, nil)
		err := f.fail[name]
		progress(name, true, err)
		results = append(results, tools.InstallResult{Tool: name, Err: err})
	}
	return results, nil
}

func lspEnv(dir string, fake *toolsFake) environment {
	env := testEnv(noUI)
	env.getenv = func(name string) string {
		if name == "JD_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	env.newTools = func(string) languageTools { return fake }
	return env
}

func TestInstallLSP_InstallsTheNamedLanguagesAndAddsThemToTheChosenOnes(t *testing.T) {
	dir, fake := t.TempDir(), &toolsFake{installed: map[string]bool{"prettier": true}}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"install-lsp", "go", "java"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if len(fake.asked) != 1 || strings.Join(fake.asked[0], ",") != "go,java" {
		t.Fatalf("only the named languages are installed now: %v", fake.asked)
	}
	if saved, _ := tools.LoadLanguages(dir); strings.Join(saved, ",") != "python,html,css,json,yaml,docker,compose,go,java" {
		t.Fatalf("they are added to the defaults (nothing was chosen before): %v", saved)
	}
	for _, want := range []string{"Installing the language servers and formatters for: go, java", "gopls", "jdtls", "installed", "3 installed, 0 were there already, 0 failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("the output lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestInstallLSP_WithoutLanguagesUsesTheChosenOnesAndAllMeansAll(t *testing.T) {
	dir, fake := t.TempDir(), &toolsFake{}
	if err := tools.SaveLanguages(dir, []string{"python"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"install-lsp"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitOK || strings.Join(fake.asked[0], ",") != "python" {
		t.Fatalf("the chosen languages: code %d, %v, %s", code, fake.asked, stderr.String())
	}
	if code := run([]string{"install-tools", "--all"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitOK || len(fake.asked[1]) != len(tools.AllLanguages()) {
		t.Fatalf("--all (and the alias install-tools) installs every language: code %d, %v", code, fake.asked)
	}
	if saved, _ := tools.LoadLanguages(dir); len(saved) != len(tools.AllLanguages()) {
		t.Fatalf("--all is remembered: %v", saved)
	}
}

func TestInstallLSP_FailuresGiveANonZeroExitAndAHintForMissingRuntimes(t *testing.T) {
	fake := &toolsFake{fail: map[string]error{"pyright": &tools.MissingRuntimeError{Tool: "pyright", Needs: "npm (Node.js)"}}}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"install-lsp", "python"}, &stdout, &stderr, lspEnv(t.TempDir(), fake)); code != exitFailure {
		t.Fatalf("a failed tool must fail the command: %d", code)
	}
	if !strings.Contains(stdout.String(), "pyright") || !strings.Contains(stdout.String(), "FAILED") || !strings.Contains(stdout.String(), "1 failed") {
		t.Fatalf("the failure is shown:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "jd setup") {
		t.Fatalf("a missing runtime points to jd setup: %q", stderr.String())
	}
	other := &toolsFake{fail: map[string]error{"black": errors.New("offline")}}
	stderr.Reset()
	if code := run([]string{"install-lsp", "python"}, &stdout, &stderr, lspEnv(t.TempDir(), other)); code != exitFailure || strings.Contains(stderr.String(), "jd setup") {
		t.Fatalf("another failure has no runtime hint: %d %q", code, stderr.String())
	}
}

func TestInstallLSP_ListShowsWhatIsChosenAndInstalledAndBadInputIsRefused(t *testing.T) {
	dir, fake := t.TempDir(), &toolsFake{installed: map[string]bool{"pyright": true}}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"install-lsp", "--list"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	list := stdout.String()
	if !strings.Contains(list, "* python") || !strings.Contains(list, "pyright: ok") || !strings.Contains(list, "black: missing") || strings.Contains(list, "* go ") {
		t.Fatalf("the list shows the defaults as chosen and the state of the tools:\n%s", list)
	}
	if len(fake.asked) != 0 {
		t.Fatal("--list installs nothing")
	}
	stderr.Reset()
	if code := run([]string{"install-lsp", "cobol"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitUsage || !strings.Contains(stderr.String(), `unknown language "cobol"`) {
		t.Fatalf("an unknown language is a usage error: %d %q", code, stderr.String())
	}
	if code := run([]string{"install-lsp", "--bogus"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitUsage {
		t.Fatalf("an unknown flag is a usage error: %d", code)
	}
	none := t.TempDir()
	if err := tools.SaveLanguages(none, nil); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"install-lsp"}, &stdout, &stderr, lspEnv(none, fake)); code != exitUsage {
		t.Fatalf("with no language chosen and none named there is nothing to do: %d", code)
	}
}

func TestInstallLSP_UpdateBringsTheInstalledToolsToTheirNewestVersions(t *testing.T) {
	dir, fake := t.TempDir(), &toolsFake{installed: map[string]bool{"pyright": true, "black": true, "debugpy": true}}
	var stdout, stderr bytes.Buffer
	if err := tools.SaveLanguages(dir, []string{"python"}); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"install-lsp", "--update"}, &stdout, &stderr, lspEnv(dir, fake)); code != exitOK {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if len(fake.updatedLanguages) != 1 || strings.Join(fake.updatedLanguages[0], ",") != "python" || len(fake.asked) != 0 {
		t.Fatalf("--update updates the chosen languages and installs nothing new: %v %v", fake.updatedLanguages, fake.asked)
	}
	for _, want := range []string{"Updating the language servers and formatters for: python", "pyright", "updating ...", "3 updated, 0 installed, 0 failed"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("the output lacks %q:\n%s", want, stdout.String())
		}
	}
	// named languages are updated and added to the chosen ones; a failing update gives a non-zero exit
	failing := &toolsFake{installed: map[string]bool{"gopls": true}, fail: map[string]error{"gopls": &tools.ManualInstallError{Tool: "gopls", Hint: "brew upgrade gopls", Update: true}}}
	stdout.Reset()
	if code := run([]string{"install-lsp", "--update", "go"}, &stdout, &stderr, lspEnv(dir, failing)); code != exitFailure {
		t.Fatalf("a failed update fails the command: %d", code)
	}
	if !strings.Contains(stdout.String(), "FAILED") || !strings.Contains(stdout.String(), "has to be updated with your package manager") || !strings.Contains(stdout.String(), "1 failed") {
		t.Fatalf("the failure is shown:\n%s", stdout.String())
	}
	if saved, _ := tools.LoadLanguages(dir); !strings.Contains(strings.Join(saved, ","), "go") {
		t.Fatalf("an updated language is added to the chosen ones: %v", saved)
	}
}
