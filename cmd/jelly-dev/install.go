package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const toolName = "jd"

// gitSetting is one `git config` entry.
type gitSetting struct{ key, value string }

// gitSettings returns the configuration that makes git use jd as diff and merge tool.
func gitSettings() []gitSetting {
	return []gitSetting{
		{"diff.tool", toolName},
		{"difftool." + toolName + ".cmd", toolName + ` "$LOCAL" "$REMOTE"`},
		{"difftool.prompt", "false"},
		{"merge.tool", toolName},
		{"mergetool." + toolName + ".cmd", toolName + ` merge "$BASE" "$LOCAL" "$REMOTE" "$MERGED"`},
		{"mergetool." + toolName + ".trustExitCode", "true"},
	}
}

func runGitConfig(scope, key, value string) error {
	out, err := exec.Command("git", "config", scope, key, value).CombinedOutput() // #nosec G204 -- fixed settings
	if err != nil {
		return fmt.Errorf("git config %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runInstallGit prints the git configuration for jd and applies it with --apply.
func runInstallGit(args []string, stdout, stderr io.Writer, env environment) int {
	apply, scope := false, "--global"
	for _, arg := range args {
		switch arg {
		case "--apply":
			apply = true
		case "--local":
			scope = "--local"
		default:
			fmt.Fprintf(stderr, "jd: unknown argument %q\n%s\n", arg, usage)
			return exitUsage
		}
	}
	for _, setting := range gitSettings() {
		fmt.Fprintf(stdout, "git config %s %s '%s'\n", scope, setting.key, setting.value)
		if !apply {
			continue
		}
		if err := env.gitConfig(scope, setting.key, setting.value); err != nil {
			printError(stderr, err)
			return exitFailure
		}
	}
	if !apply {
		fmt.Fprintln(stdout, "# nothing changed; run `jd install-git --apply` to write these settings")
		fmt.Fprintln(stdout, "# note: --apply overwrites existing values of these keys (diff.tool, merge.tool, ...)")
	}
	return exitOK
}
