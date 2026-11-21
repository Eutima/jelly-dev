package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/eutima/jelly-dev/scripts"
)

const (
	// envSkipSetup stops jd from running the installer script on its first start.
	envSkipSetup = "JD_SKIP_SETUP"
	setupMarker  = "setup-done"
	// envJDBin tells the installer script where jd is installed: it then does not download jd again.
	envJDBin = "JD_BIN"
)

// installerRunner runs the installer script for the jd binary at jdBin, with the terminal of jd (the script may ask for
// the sudo password).
type installerRunner func(jdBin string, stdin io.Reader, stdout, stderr io.Writer) error

// runInstallerScript writes the installer script that is part of this release to a temporary file and runs it with the
// shell (PowerShell on Windows). Everything the installer reads from the environment still counts: JD_SKIP_DEPS, JD_DEPS,
// JD_SKIP_GIT_CONFIG and JD_DRY_RUN.
func runInstallerScript(jdBin string, stdin io.Reader, stdout, stderr io.Writer) error {
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
	cmd.Env = append(os.Environ(), envJDBin+"="+jdBin)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}

// runInstallerForSelf is what both the first start and `jd setup` do.
func runInstallerForSelf(stdout, stderr io.Writer, env environment) error {
	run := env.runInstaller
	if run == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find jd: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return run(exe, os.Stdin, stdout, stderr)
}

// runSetup handles `jd setup`: it runs the installer script now, whatever happened on the first start.
func runSetup(stdout, stderr io.Writer, env environment) int {
	if env.runInstaller == nil {
		fmt.Fprintln(stderr, "jd: setup is not available here")
		return exitFailure
	}
	if err := runInstallerForSelf(stdout, stderr, env); err != nil {
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
	if err := runInstallerForSelf(stdout, stderr, env); err != nil {
		fmt.Fprintf(stderr, "jd: the installer script failed (%v); run it again with: jd setup\n", err)
	}
	if env.getenv("JD_DRY_RUN") != "" {
		return // a dry run installed nothing: the real first start is still to come
	}
	if err := os.MkdirAll(configDir, 0o700); err == nil {
		err = os.WriteFile(marker, []byte("jd ran the installer script on its first start\n"), 0o600)
		if err != nil {
			fmt.Fprintf(stderr, "jd: warning: cannot remember that the installer ran (%v)\n", err)
		}
	} else {
		fmt.Fprintf(stderr, "jd: warning: cannot remember that the installer ran (%v)\n", err)
	}
}

// interactiveTerminal reports whether jd runs in a terminal it can talk to (not in a pipe, a cron job or CI).
func interactiveTerminal() bool {
	stdin, err := os.Stdin.Stat()
	return err == nil && stdin.Mode()&os.ModeCharDevice != 0 && stderrIsTerminal()
}
