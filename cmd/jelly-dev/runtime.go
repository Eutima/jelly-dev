package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eutima/jelly-dev/internal/tools"
)

const (
	// envSkipDeps turns off the installation of dependencies in the installer script.
	envSkipDeps = "JD_SKIP_DEPS"
	// failureMemory is how long a failed installation of a runtime is remembered: the tools of one run (eight npm tools, say) must
	// not each try the download again, a later try (:tools) may.
	failureMemory = 30 * time.Second
)

// outputTail is how many of the last bytes of the installer's output an error carries.
const outputTail = 600

// runtimeInstaller returns the hook that installs a runtime a language server needs, for tools.Installer. Only Node.js (and npm with
// it) is supported: it runs the installer script that is part of this release (pinned versions and checksums, below ~/.local without
// root rights), for the Node.js group only. A pip install of jd can then do the whole job when the first language server is added, also
// where no terminal asks the first-start question (a Docker build, a script).
func runtimeInstaller(run installerRunner, getenv func(string) string, now func() time.Time) func(ctx context.Context, runtime string) error {
	var mu sync.Mutex
	var failedAt time.Time
	var failure error
	return func(_ context.Context, runtime string) error {
		if runtime != "node" {
			return fmt.Errorf("jd does not install %s itself", runtime)
		}
		mu.Lock()
		defer mu.Unlock() // the tools of one run ask one after the other: the first one installs, the others find the result
		if failure != nil && now().Sub(failedAt) < failureMemory {
			return failure
		}
		if _, err := exec.LookPath("npm"); err == nil { // installed meanwhile (by another tool of this run, or by hand)
			failure = nil
			return nil
		}
		if err := installNode(run, getenv); err != nil {
			failedAt, failure = now(), err
			return err
		}
		failure = nil
		return nil
	}
}

// installNode runs the installer script for Node.js only: no jd download, no git configuration, no setup marker.
func installNode(run installerRunner, getenv func(string) string) error {
	if run == nil {
		return errors.New("the installer is not available here")
	}
	if getenv(envSkipDeps) != "" {
		return fmt.Errorf("%s is set", envSkipDeps)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find jd: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	var out bytes.Buffer
	extra := []string{"JD_DEPS=runtimes", envLangs + "=javascript", "JD_SKIP_GIT_CONFIG=1"}
	if err := run(exe, extra, nil, &out, &out); err != nil {
		return fmt.Errorf("the installer failed: %w%s", err, outputNote(out.String()))
	}
	tools.EnsurePath() // the installer put node and npm below ~/.local/bin (or where Windows keeps them): this process looks there too
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("the installer ran, but npm is still not on the PATH%s", outputNote(out.String()))
	}
	return nil
}

// outputNote is the end of the installer's output, for an error message.
func outputNote(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	if len(output) > outputTail {
		output = "..." + output[len(output)-outputTail:]
	}
	return ": " + output
}

// newInstaller is the installer of language servers and formatters for the config folder dir. When a server needs Node.js and it
// is missing, it is installed first (see runtimeInstaller).
func newInstaller(dir string, env environment) *tools.Installer {
	installer := tools.NewInstaller(dir)
	installer.UseRuntimeInstaller(runtimeInstaller(env.runInstaller, env.getenv, time.Now))
	return installer
}
