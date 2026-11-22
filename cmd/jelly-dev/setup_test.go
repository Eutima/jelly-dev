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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
	"github.com/eutima/jelly-dev/internal/ui"
)

// installerFake records what the installer script was started for.
type installerFake struct {
	calls []string
	envs  [][]string // the extra environment of every call
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
	env.runInstaller = func(jdBin string, extraEnv []string, _ io.Reader, _, _ io.Writer) error {
		f.calls = append(f.calls, jdBin)
		f.envs = append(f.envs, extraEnv)
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
	if code := run([]string{"setup", "bogus"}, &stdout, &stderr, fake.env(false, "")); code != exitUsage {
		t.Fatalf("an unknown argument is a usage error: %d", code)
	}
	fake.err = errors.New("exit status 1")
	if code := run([]string{"setup"}, &stdout, &stderr, fake.env(false, "")); code != exitFailure {
		t.Fatalf("a failing installer must fail jd setup: %d", code)
	}
}

// The real embedded script, started the way jd starts it (the pip case): it must configure git with the installed jd, not
// download jd, and put the folder of the tools on the PATH of new shells (pip does not, and the tools go to ~/.local/bin).
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
	if err := runInstallerScript(fakeJd, nil, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s%s", err, stdout.String(), stderr.String())
	}
	calls, _ := os.ReadFile(log)
	if strings.TrimSpace(string(calls)) != "install-git --apply" {
		t.Fatalf("the installer must call the installed jd to set git up: %q\n%s%s", calls, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "Downloading") {
		t.Fatalf("jd is installed already, nothing is downloaded:\n%s", stdout.String())
	}
	want := "export PATH=\"" + filepath.Join(home, ".local", "bin") + ":$PATH\""
	for _, profile := range []string{".profile", ".zshrc"} {
		text, err := os.ReadFile(filepath.Join(home, profile))
		if err != nil || !strings.Contains(string(text), want) {
			t.Errorf("%s must put %s on the PATH: %q %v", profile, filepath.Join(home, ".local", "bin"), text, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); err == nil {
		t.Errorf("no tools were installed (JD_SKIP_DEPS), nothing may be created in ~/.local")
	}
}

// The curl | sh installer does the whole setup itself and leaves the mark, so jd does not run the script on its first start.
func TestRun_SetupMarkDoneKeepsTheFirstStartFromRunningTheInstaller(t *testing.T) {
	dir, fake := t.TempDir(), &installerFake{}
	env := fake.env(true, "")
	env.getenv = func(name string) string {
		if name == "JD_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	if code := run([]string{"setup", "--mark-done"}, io.Discard, io.Discard, env); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("marking must not run the installer: %v", fake.calls)
	}
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if len(fake.calls) != 0 {
		t.Fatalf("after the installer the first start has nothing to do: %v", fake.calls)
	}
}

func TestRun_SetupMarkDoneFailsWhenTheConfigFolderCannotBeWritten(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := testEnv(noUI)
	env.getenv = func(name string) string {
		if name == "JD_CONFIG_DIR" {
			return filepath.Join(blocker, "sub") // below a file: it cannot be created
		}
		return ""
	}
	var stderr bytes.Buffer
	if code := run([]string{"setup", "--mark-done"}, io.Discard, &stderr, env); code != exitFailure || stderr.Len() == 0 {
		t.Fatalf("an unwritable config folder must fail with a message: %d %q", code, stderr.String())
	}
}

// answerLanguages is a runUI that answers the language question with the given keys.
func answerLanguages(asked *int, keys ...tea.KeyMsg) uiRunner {
	return func(model tea.Model) error {
		if _, isEditor := model.(ui.EditorChoiceModel); isEditor { // the editor question comes first: nano, by enter; it is not the one counted here
			model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
			model.Update(keyEnter)
			return nil
		}
		*asked++
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
		for _, key := range keys {
			model, _ = model.Update(key)
		}
		return nil
	}
}

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyN     = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	keySpace = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
)

func TestSetup_AsksWhichLanguagesOnceAndPassesThemToTheInstaller(t *testing.T) {
	dir, fake, asked := t.TempDir(), &installerFake{}, 0
	env := fake.env(true, "")
	env.runUI = answerLanguages(&asked, keySpace, tea.KeyMsg{Type: tea.KeyDown}, keySpace, keyEnter) // nothing is checked: Python and HTML
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if asked != 1 || len(fake.envs) != 1 || fake.envs[0][0] != "JD_LANGS=python,html" {
		t.Fatalf("the question is asked and the answer goes to the installer: asked %d, env %v", asked, fake.envs)
	}
	if saved, ok := tools.LoadLanguages(dir); !ok || strings.Join(saved, ",") != "python,html" {
		t.Fatalf("the answer is saved: %v %v", saved, ok)
	}
	// jd setup later: nobody is asked again, the saved languages are used
	if code := run([]string{"setup"}, io.Discard, io.Discard, withConfigDir(env, dir)); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if asked != 1 || len(fake.envs) != 2 || fake.envs[1][0] != "JD_LANGS=python,html" {
		t.Fatalf("the question is asked only initially: asked %d, env %v", asked, fake.envs)
	}
}

func withConfigDir(env environment, dir string) environment {
	inner := env.getenv
	env.getenv = func(name string) string {
		if name == "JD_CONFIG_DIR" {
			return dir
		}
		return inner(name)
	}
	return env
}

func TestSetup_NoLanguageOrASkippedQuestionInstallsNothing(t *testing.T) {
	for name, keys := range map[string][]tea.KeyMsg{"none": {keyN, keyEnter}, "skipped": {keyEsc}} {
		dir, fake, asked := t.TempDir(), &installerFake{}, 0
		env := fake.env(true, "")
		env.runUI = answerLanguages(&asked, keys...)
		setupOnFirstStart(dir, io.Discard, io.Discard, env)
		if len(fake.envs) != 1 || fake.envs[0][0] != "JD_LANGS=none" {
			t.Fatalf("%s: the installer must get JD_LANGS=none: %v", name, fake.envs)
		}
		if saved, ok := tools.LoadLanguages(dir); !ok || len(saved) != 0 {
			t.Fatalf("%s: choosing nothing is remembered too: %v %v", name, saved, ok)
		}
	}
}

func TestSetup_OtherLanguagesAreChosenWithTheKeys(t *testing.T) {
	dir, fake, asked := t.TempDir(), &installerFake{}, 0
	env := fake.env(true, "")
	down := tea.KeyMsg{Type: tea.KeyDown}
	env.runUI = answerLanguages(&asked, down, down, down, down, down, down, down, down, down, keySpace, keyEnter) // down to Go, check it
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if fake.envs[0][0] != "JD_LANGS=go" {
		t.Fatalf("nothing was checked before: only Go: %v", fake.envs)
	}
}

func TestSetup_JdLangsFromTheEnvironmentOrNoTerminalAsksNobody(t *testing.T) {
	asked := 0
	// JD_LANGS says it all
	fake := &installerFake{}
	env := fake.env(true, "")
	env.runUI = answerLanguages(&asked, keyEnter)
	inner := env.getenv
	env.getenv = func(name string) string {
		if name == "JD_LANGS" {
			return "go, java"
		}
		return inner(name)
	}
	setupOnFirstStart(t.TempDir(), io.Discard, io.Discard, env)
	if asked != 0 || fake.envs[0][0] != "JD_LANGS=go,java" {
		t.Fatalf("JD_LANGS must be used without asking: asked %d, %v", asked, fake.envs)
	}
	// no terminal (jd setup in a script): the defaults, no question, nothing saved
	dir, fake2 := t.TempDir(), &installerFake{}
	env2 := withConfigDir(fake2.env(false, ""), dir)
	env2.runUI = answerLanguages(&asked, keyEnter)
	if code := run([]string{"setup"}, io.Discard, io.Discard, env2); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if asked != 0 || fake2.envs[0][0] != "JD_LANGS=python,html,css,json,yaml,docker,compose" {
		t.Fatalf("without a terminal nobody is asked and the defaults are used: asked %d, %v", asked, fake2.envs)
	}
	if _, ok := tools.LoadLanguages(dir); ok {
		t.Fatal("defaults that nobody chose are not saved: the question is still to come")
	}
	// a wrong JD_LANGS is reported and the question is asked instead
	fake3, asked3 := &installerFake{}, 0
	env3 := fake3.env(true, "")
	env3.runUI = answerLanguages(&asked3, keyEnter)
	inner3 := env3.getenv
	env3.getenv = func(name string) string {
		if name == "JD_LANGS" {
			return "cobol"
		}
		return inner3(name)
	}
	var stderr bytes.Buffer
	setupOnFirstStart(t.TempDir(), io.Discard, &stderr, env3)
	if asked3 != 1 || !strings.Contains(stderr.String(), `unknown language "cobol"`) {
		t.Fatalf("a wrong JD_LANGS is reported: asked %d %q", asked3, stderr.String())
	}
}

// ctrl+c on the question is "not now": nothing is saved, nothing is installed, no mark, the question comes again.
func TestSetup_CtrlCOnTheQuestionMeansNotNowAndIsAskedAgain(t *testing.T) {
	dir, fake, asked := t.TempDir(), &installerFake{}, 0
	env := fake.env(true, "")
	env.runUI = answerLanguages(&asked, tea.KeyMsg{Type: tea.KeyCtrlC})
	var stderr bytes.Buffer
	setupOnFirstStart(dir, io.Discard, &stderr, env)
	if len(fake.calls) != 0 {
		t.Fatalf("the installer must not run after ctrl+c: %v", fake.calls)
	}
	if _, ok := tools.LoadLanguages(dir); ok {
		t.Fatal("ctrl+c must not save a choice")
	}
	if _, err := os.Stat(filepath.Join(dir, setupMarker)); err == nil {
		t.Fatal("no mark: the setup is still to come")
	}
	if !strings.Contains(stderr.String(), "asks again") {
		t.Fatalf("it must say that it asks again: %q", stderr.String())
	}
	env.runUI = answerLanguages(&asked, keyEnter) // the next start
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if asked != 2 || len(fake.calls) != 1 {
		t.Fatalf("the next start asks again and installs: asked %d, calls %v", asked, fake.calls)
	}
	// jd setup: cancelled is not a failure
	fake2, asked2 := &installerFake{}, 0
	env2 := withConfigDir(fake2.env(true, ""), t.TempDir())
	env2.runUI = answerLanguages(&asked2, tea.KeyMsg{Type: tea.KeyCtrlC})
	var stderr2 bytes.Buffer
	if code := run([]string{"setup"}, io.Discard, &stderr2, env2); code != exitOK || len(fake2.calls) != 0 || !strings.Contains(stderr2.String(), "cancelled") {
		t.Fatalf("jd setup cancelled at the question: code %d, calls %v, %q", code, fake2.calls, stderr2.String())
	}
}

func TestSetup_JdLangsNoneIsRecognisedInAnyCase(t *testing.T) {
	for _, value := range []string{"none", "NONE", "None"} {
		fake, asked := &installerFake{}, 0
		env := fake.env(true, "")
		env.runUI = answerLanguages(&asked, keyEnter)
		inner := env.getenv
		env.getenv = func(name string) string {
			if name == "JD_LANGS" {
				return value
			}
			return inner(name)
		}
		setupOnFirstStart(t.TempDir(), io.Discard, io.Discard, env)
		if asked != 0 || fake.envs[0][0] != "JD_LANGS=none" {
			t.Fatalf("%s: no question, no language: asked %d, %v", value, asked, fake.envs)
		}
	}
}

// Nothing is checked when the question comes: enter on its own chooses no language (it is no "install the defaults").
func TestSetup_TheQuestionStartsWithNothingChecked(t *testing.T) {
	dir, fake, asked := t.TempDir(), &installerFake{}, 0
	env := fake.env(true, "")
	env.runUI = answerLanguages(&asked, keyEnter)
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if asked != 1 || len(fake.envs) != 1 || fake.envs[0][0] != "JD_LANGS=none" {
		t.Fatalf("enter without a check chooses no language: asked %d, %v", asked, fake.envs)
	}
	if saved, ok := tools.LoadLanguages(dir); !ok || len(saved) != 0 {
		t.Fatalf("and that is remembered: %v %v", saved, ok)
	}
}
