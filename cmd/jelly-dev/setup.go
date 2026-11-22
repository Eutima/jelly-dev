package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/tools"
	"github.com/eutima/jelly-dev/internal/ui"
	"github.com/eutima/jelly-dev/scripts"
)

const (
	// envSkipSetup stops jd from running the installer script on its first start.
	envSkipSetup = "JD_SKIP_SETUP"
	setupMarker  = "setup-done"
	// envJDBin tells the installer script where jd is installed: it then does not download jd again.
	envJDBin = "JD_BIN"
	// envLangs tells the installer script (and the user, ahead of time) which languages to install tools for.
	envLangs = "JD_LANGS"
)

// installerRunner runs the installer script for the jd binary at jdBin, with the terminal of jd (the script may ask for
// the sudo password).
type installerRunner func(jdBin string, extraEnv []string, stdin io.Reader, stdout, stderr io.Writer) error

// runInstallerScript writes the installer script that is part of this release to a temporary file and runs it with the
// shell (PowerShell on Windows). Everything the installer reads from the environment still counts: JD_SKIP_DEPS, JD_DEPS,
// JD_SKIP_GIT_CONFIG and JD_DRY_RUN.
func runInstallerScript(jdBin string, extraEnv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	content, pattern, shell := scripts.InstallSh, "jd-install-*.sh", []string{"sh"}
	if runtime.GOOS == "windows" {
		content, pattern, shell = scripts.InstallPs1, "jd-install-*.ps1", []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File"}
	}
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	cmd := exec.Command(shell[0], append(shell[1:], file.Name())...) // #nosec G204 -- fixed shell, the script is embedded in this binary
	cmd.Env = append(append(os.Environ(), envJDBin+"="+jdBin), extraEnv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}

// errSetupDeclined is returned when the user answered the language question with "not now": nothing runs and nothing is
// remembered, so the question comes again.
var errSetupDeclined = errors.New("not now")

// runInstallerForSelf is what both the first start and `jd setup` do.
func runInstallerForSelf(configDir string, stdout, stderr io.Writer, env environment, askEditorAgain bool) error {
	run := env.runInstaller
	if run == nil {
		return nil
	}
	if err := chooseEditor(configDir, stderr, env, askEditorAgain); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find jd: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	languages, ok := languagesForSetup(configDir, stderr, env)
	if !ok {
		return errSetupDeclined
	}
	return run(exe, []string{envLangs + "=" + languagesValue(languages)}, os.Stdin, stdout, stderr)
}

// markSetupDone records that the installer script did its work, so the first start does not run it again.
func markSetupDone(configDir string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, setupMarker), []byte("the installer script did the setup\n"), 0o600)
}

// runSetup handles `jd setup`: it runs the installer script now, whatever happened on the first start. `jd setup
// --mark-done` only records that the installer did its work (install.sh and install.ps1 call it when they installed jd).
func runSetup(args []string, stdout, stderr io.Writer, env environment) int {
	dir, dirErr := project.ConfigDir(env.getenv)
	if len(args) == 1 && args[0] == "--mark-done" {
		err := dirErr
		if err == nil {
			err = markSetupDone(dir)
		}
		if err != nil {
			printError(stderr, err)
			return exitFailure
		}
		return exitOK
	}
	if len(args) > 0 {
		fmt.Fprintf(stderr, "jd: unknown argument %q\n%s\n", args[0], usage)
		return exitUsage
	}
	if env.runInstaller == nil {
		fmt.Fprintln(stderr, "jd: setup is not available here")
		return exitFailure
	}
	if dirErr != nil {
		printError(stderr, dirErr)
		return exitFailure
	}
	if err := runInstallerForSelf(dir, stdout, stderr, env, true); err != nil {
		if errors.Is(err, errSetupDeclined) {
			fmt.Fprintln(stderr, "jd: setup cancelled; run jd setup when you want it")
			return exitOK
		}
		printError(stderr, fmt.Errorf("installer: %w", err))
		return exitFailure
	}
	return exitOK
}

// setupOnFirstStart runs the installer script the first time jd starts interactively. A pip install only copies the
// binary (a wheel cannot run code when it is installed), so this is what gives `pip install jelly-dev` the same result as
// `curl ... | sh`: the tools the IDE needs are installed and jd becomes git's diff and merge tool (existing values are
// overwritten, as the installer does). A marker file in the config folder records that it ran, so it asks nothing twice
// (`jd setup` runs it again). JD_SKIP_SETUP=1 turns it off; the installer's own switches (JD_SKIP_DEPS,
// JD_SKIP_GIT_CONFIG, JD_DEPS) apply.
func setupOnFirstStart(configDir string, stdout, stderr io.Writer, env environment) {
	if env.runInstaller == nil || env.interactive == nil || !env.interactive() || env.getenv(envSkipSetup) != "" {
		return
	}
	marker := filepath.Join(configDir, setupMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	fmt.Fprintf(stderr, "jd: first start: running the installer script (installs the tools the IDE needs and sets git up).\n")
	fmt.Fprintf(stderr, "jd: skip it with %s=1, or only parts with JD_SKIP_DEPS=1 / JD_SKIP_GIT_CONFIG=1; run it again with: jd setup\n", envSkipSetup)
	if err := runInstallerForSelf(configDir, stdout, stderr, env, false); errors.Is(err, errSetupDeclined) {
		fmt.Fprintln(stderr, "jd: setup skipped for now; it asks again at the next start (or run: jd setup)")
		return // no marker: the question comes again
	} else if err != nil {
		fmt.Fprintf(stderr, "jd: the installer script failed (%v); run it again with: jd setup\n", err)
	}
	if env.getenv("JD_DRY_RUN") != "" {
		return // a dry run installed nothing: the real first start is still to come
	}
	if err := markSetupDone(configDir); err != nil {
		fmt.Fprintf(stderr, "jd: warning: cannot remember that the installer ran (%v)\n", err)
	}
}

// interactiveTerminal reports whether jd runs in a terminal it can talk to (not in a pipe, a cron job or CI).
func interactiveTerminal() bool {
	stdin, err := os.Stdin.Stat()
	return err == nil && stdin.Mode()&os.ModeCharDevice != 0 && stderrIsTerminal()
}

// languagesValue is what JD_LANGS holds for the installer script: the ids, or "none" when no language was chosen (an empty
// value would mean that nothing was said and everything is installed).
func languagesValue(languages []string) string {
	if len(languages) == 0 {
		return "none"
	}
	return strings.Join(languages, ",")
}

// languagesForSetup decides which languages the installer script installs tools for: what the user said in JD_LANGS, else
// what was chosen before (languages.json), else the answer to the question that is asked now, once, when jd is used in a
// terminal (nothing is checked beforehand). Without a terminal to ask the defaults count. The answer is saved, so nobody is asked twice (jd install-lsp changes it).
func languagesForSetup(configDir string, stderr io.Writer, env environment) (languages []string, ok bool) {
	if said := env.getenv(envLangs); said != "" {
		languages, err := tools.ParseLanguages(said)
		if err == nil {
			return languages, true
		}
		fmt.Fprintf(stderr, "jd: %s: %v; asking instead\n", envLangs, err)
	}
	if saved, found := tools.LoadLanguages(configDir); found {
		return saved, true
	}
	chosen := tools.DefaultLanguages()
	if env.interactive == nil || !env.interactive() || env.runUI == nil {
		return chosen, true // nobody to ask (a script, CI): the defaults, and the question is still to come
	}
	model, outcome := ui.NewLanguagesModel(tools.Languages(), nil) // nothing is checked: the user chooses
	if err := env.runUI(model); err != nil {
		fmt.Fprintf(stderr, "jd: could not ask which languages: %v (using %s)\n", err, strings.Join(chosen, ", "))
		return chosen, true
	}
	if outcome.Aborted {
		return nil, false // not now
	}
	if !outcome.Answered {
		return chosen, true
	}
	chosen = outcome.Chosen // a dismissed question chooses no language: nothing is installed now
	if err := tools.SaveLanguages(configDir, chosen); err != nil {
		fmt.Fprintf(stderr, "jd: cannot remember the languages: %v\n", err)
	}
	return chosen, true
}

// exitHint is what jd prints when it ends: the PATH hint after a clean exit in a terminal, nothing otherwise (a script, a failure).
func exitHint(code int, exe, path, goos string, terminal bool) string {
	if code != exitOK || !terminal {
		return ""
	}
	return pathHint(exe, path, goos)
}

// pathHint is the message that tells how to put jd on the PATH, when the folder of the jd binary is not on it (a pip install as a normal
// user puts jd in ~/.local/bin, which many shells do not search, and `python3 -m jelly_dev` is how it was started). "" when jd is found
// or the PATH cannot be judged. path is the PATH of the shell the program was started from.
func pathHint(exe, path, goos string) string {
	if goos == "windows" || exe == "" || path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	folder := filepath.Dir(exe)
	for _, entry := range filepath.SplitList(path) {
		if entry == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(entry); err == nil {
			entry = resolved // a PATH entry that is a link to the folder counts
		}
		if filepath.Clean(entry) == folder {
			return ""
		}
	}
	return fmt.Sprintf("jd: jd is not on your PATH yet (it is in %s). Use it in this shell:  %s\n"+
		"jd: to keep it for new shells add that line to your ~/.profile (the first-start setup of jd does it when it ran).\n", folder, pathExport(folder))
}

// pathExport is the line that puts a folder on the PATH: in double quotes for an ordinary folder name, in single quotes when the name has
// characters the shell would treat as syntax (a space, $, a quote, a backtick ...).
func pathExport(folder string) string {
	if strings.IndexFunc(folder, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-~+@:,=", r))
	}) < 0 {
		return `export PATH="` + folder + `:$PATH"`
	}
	return `export PATH=` + "'" + strings.ReplaceAll(folder, "'", `'\''`) + "'" + `:"$PATH"`
}
