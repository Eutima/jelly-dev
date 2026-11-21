package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/tools"
)

// languageTools installs the language servers and formatters of languages and tells what is installed.
type languageTools interface {
	InstallLanguages(ctx context.Context, languages []string, progress func(tool string, done bool, err error)) ([]tools.InstallResult, error)
	IsInstalled(tool string) bool
}

// runInstallLSP handles `jd install-lsp [--all] [--list] [<language>...]`: it installs the language server and the formatter of
// each language (the languages chosen so far when none is named) with the language's own package manager, without root
// rights; a missing runtime (Node.js, Python, Go, a JDK) is reported, `jd setup` installs those. Languages named here
// are added to the chosen ones, which the IDE keeps installed.
func runInstallLSP(args []string, stdout, stderr io.Writer, env environment) int {
	var named []string
	all, list := false, false
	for _, arg := range args {
		switch {
		case arg == "--all":
			all = true
		case arg == "--list":
			list = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "jd: unknown argument %q\n%s\n", arg, usage)
			return exitUsage
		default:
			named = append(named, arg)
		}
	}
	dir, err := project.ConfigDir(env.getenv)
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	chosen, hasChosen := tools.LoadLanguages(dir)
	if !hasChosen {
		chosen = tools.DefaultLanguages()
	}
	requested, err := tools.ParseLanguages(strings.Join(named, ","))
	if err != nil {
		fmt.Fprintf(stderr, "jd: %v\n", err)
		return exitUsage
	}
	if all {
		requested = tools.AllLanguages()
	}
	if env.newTools == nil {
		fmt.Fprintln(stderr, "jd: installing tools is not available here")
		return exitFailure
	}
	installer := env.newTools(dir)
	if list {
		printLanguages(stdout, installer, chosen)
		return exitOK
	}
	languages := chosen
	if len(requested) > 0 {
		languages, _ = tools.ParseLanguages(strings.Join(append(slices.Clone(chosen), requested...), ","))
		if err := tools.SaveLanguages(dir, languages); err != nil {
			fmt.Fprintf(stderr, "jd: cannot remember the languages: %v\n", err)
		}
		// the choice is saved before anything is installed (a failed install stays chosen, so the IDE and jd install-lsp try it
		// again); only what was named is installed now, the rest is kept as it is
		languages = requested
	}
	if len(languages) == 0 {
		fmt.Fprintln(stderr, "jd: no language chosen: name some, for example: jd install-lsp python html css json (or --all, --list)")
		return exitUsage
	}
	fmt.Fprintf(stdout, "Installing the language servers and formatters for: %s\n", strings.Join(languages, ", "))
	results, err := installer.InstallLanguages(context.Background(), languages, func(tool string, done bool, err error) {
		switch {
		case !done:
			fmt.Fprintf(stdout, "  %-30s installing ...\n", tool)
		case err != nil:
			fmt.Fprintf(stdout, "  %-30s FAILED: %v\n", tool, err)
		default:
			fmt.Fprintf(stdout, "  %-30s installed\n", tool)
		}
	})
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	return summarizeInstall(results, stdout, stderr)
}

// summarizeInstall prints what came of the installation and returns the exit code.
func summarizeInstall(results []tools.InstallResult, stdout, stderr io.Writer) int {
	installed, already, failed, runtimeMissing := 0, 0, 0, false
	for _, r := range results {
		var missing *tools.MissingRuntimeError
		switch {
		case r.Err != nil:
			failed++
			runtimeMissing = runtimeMissing || errors.As(r.Err, &missing)
		case r.Already:
			already++
		default:
			installed++
		}
	}
	fmt.Fprintf(stdout, "%d installed, %d were there already, %d failed\n", installed, already, failed)
	if runtimeMissing {
		fmt.Fprintln(stderr, "jd: a runtime is missing (Node.js, Python, Go or a JDK): `jd setup` installs the runtimes with your package manager")
	}
	if failed > 0 {
		return exitFailure
	}
	return exitOK
}

// printLanguages lists the languages with the tools they need and whether those are installed.
func printLanguages(stdout io.Writer, installer languageTools, chosen []string) {
	for _, l := range tools.Languages() {
		mark := " "
		if slices.Contains(chosen, l.ID) {
			mark = "*"
		}
		var parts []string
		for _, name := range l.Tools {
			state := "missing"
			if installer.IsInstalled(name) {
				state = "ok"
			}
			parts = append(parts, name+": "+state)
		}
		fmt.Fprintf(stdout, "%s %-11s %-26s %s\n", mark, l.ID, l.Name, strings.Join(parts, ", "))
	}
	fmt.Fprintln(stdout, "(* chosen; jd install-lsp <language>... installs and adds more)")
}
