// Command jelly-diff is a terminal diff and merge tool with a JetBrains-style UI.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/eutima/jelly-diff/internal/config"
	"github.com/eutima/jelly-diff/internal/diff"
	"github.com/eutima/jelly-diff/internal/source"
	"github.com/eutima/jelly-diff/internal/ui"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2

	clipboardTitle = "clipboard"
	usage          = `usage:
  jelly-diff <file-a> <file-b>       compare two files
  jelly-diff --clip <file>           compare a file with the clipboard (clipboard on the right)
  jelly-diff --clip-left <file>      compare the clipboard (left) with a file
  jelly-diff git [--staged] [<rev> | <a>..<b>] [-- <path>...]
                                     browse changes of the git repository in the current directory
  jelly-diff merge <base> <local> <remote> <merged>
                                     three-way merge; writes <merged> (git mergetool order)
  jelly-diff install-git [--apply] [--local]
                                     print (or with --apply write, overwriting existing values) the git diff/merge tool settings`
)

// uiRunner shows a model in the terminal and blocks until the user quits.
type uiRunner func(model tea.Model) error

// environment holds everything run needs from the outside world, so tests can replace it.
type environment struct {
	runUI         uiRunner
	readClipboard func() (string, error)
	openGit       func(dir string, spec source.GitSpec) (gitChanges, error)
	gitConfig     func(scope, key, value string) error
	getenv        func(string) string
	// enableAttributes makes the terminal output keep bold/reverse/strikethrough even when NO_COLOR
	// switched the color profile to "no styling at all"; the mono theme depends on them.
	enableAttributes func()
	workDir          string
}

// gitChanges is a repository view that can list changed files and load their texts.
type gitChanges interface {
	ui.ChangeSource
	List() ([]source.ChangedFile, error)
}

func openGit(dir string, spec source.GitSpec) (gitChanges, error) {
	changes, err := source.OpenGit(dir, spec)
	if err != nil {
		return nil, err
	}
	return changes, nil
}

func main() {
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jelly-diff: %v\n", err)
		os.Exit(exitFailure)
	}
	env := environment{runUI: runProgram, readClipboard: source.ReadClipboard, openGit: openGit, gitConfig: runGitConfig, getenv: os.Getenv, enableAttributes: enableTextAttributes, workDir: workDir}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, env))
}

func runProgram(model tea.Model) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// run executes the command and returns the process exit code.
func run(args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) > 0 && args[0] == "install-git" {
		return runInstallGit(args[1:], stdout, stderr, env) // needs no theme, so a bad theme must not block it
	}
	cfg, err := config.Load(env.getenv)
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	theme, ok := ui.NamedTheme(cfg.Theme)
	if !ok {
		printError(stderr, fmt.Errorf("unknown theme %q", cfg.Theme))
		return exitFailure
	}
	if cfg.Theme == config.ThemeMono {
		env.enableAttributes()
	}
	runUI := env.runUI
	env.runUI = func(model tea.Model) error { return runUI(ui.Themed(model, theme)) }
	if len(args) > 0 {
		switch args[0] {
		case "git":
			return runGit(args[1:], stdout, stderr, env)
		case "merge":
			return runMerge(args[1:], stdout, stderr, env)
		}
	}
	flags := flag.NewFlagSet("jelly-diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	clipRight := flags.Bool("clip", false, "")
	clipLeft := flags.Bool("clip-left", false, "")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "jelly-diff: %v\n%s\n", err, usage)
		return exitUsage
	}
	positional := flags.Args()
	for _, arg := range positional {
		if len(arg) > 1 && arg[0] == '-' {
			fmt.Fprintf(stderr, "jelly-diff: flags must come before file names: %s\n%s\n", arg, usage)
			return exitUsage
		}
	}

	switch {
	case *clipRight && *clipLeft:
		fmt.Fprintf(stderr, "jelly-diff: use either --clip or --clip-left\n%s\n", usage)
		return exitUsage
	case *clipRight || *clipLeft:
		if len(positional) != 1 {
			fmt.Fprintf(stderr, "%s\n", usage)
			return exitUsage
		}
		err = diffClipboard(positional[0], *clipLeft, stdout, env)
	case len(positional) == 2:
		err = diffFiles(positional[0], positional[1], stdout, env.runUI)
	default:
		fmt.Fprintf(stderr, "%s\n", usage)
		return exitUsage
	}
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	return exitOK
}

func diffFiles(leftPath, rightPath string, stdout io.Writer, runUI uiRunner) error {
	left, err := source.ReadTextFile(leftPath)
	if err != nil {
		return err
	}
	right, err := source.ReadTextFile(rightPath)
	if err != nil {
		return err
	}
	return show(leftPath, rightPath, left, right, stdout, runUI)
}

func diffClipboard(path string, clipboardOnLeft bool, stdout io.Writer, env environment) error {
	file, err := source.ReadTextFile(path)
	if err != nil {
		return err
	}
	clip, err := env.readClipboard()
	if err != nil {
		return err
	}
	if clipboardOnLeft {
		return show(clipboardTitle, path, clip, file, stdout, env.runUI)
	}
	return show(path, clipboardTitle, file, clip, stdout, env.runUI)
}

func show(leftTitle, rightTitle, left, right string, stdout io.Writer, runUI uiRunner) error {
	model := ui.NewDiffModel(leftTitle, rightTitle, diff.SplitLines(left), diff.SplitLines(right))
	if model.HunkCount() == 0 {
		fmt.Fprintln(stdout, "no differences")
		return nil
	}
	if err := runUI(model); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	return nil
}

// runGit handles `jelly-diff git [--staged] [<rev>] [-- <path>...]`.
func runGit(args []string, stdout, stderr io.Writer, env environment) int {
	var paths []string
	if dash := indexOf(args, "--"); dash >= 0 {
		args, paths = args[:dash], args[dash+1:]
	}
	flags := flag.NewFlagSet("jelly-diff git", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	staged := flags.Bool("staged", false, "")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "jelly-diff: %v\n%s\n", err, usage)
		return exitUsage
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(stderr, "jelly-diff: at most one revision allowed (flags must come before the revision, paths after --)\n%s\n", usage)
		return exitUsage
	}
	spec := source.GitSpec{Staged: *staged, Paths: paths}
	if flags.NArg() == 1 {
		spec.Rev = flags.Arg(0)
	}
	if err := browseGit(spec, stdout, env); err != nil {
		printError(stderr, err)
		return exitFailure
	}
	return exitOK
}

func browseGit(spec source.GitSpec, stdout io.Writer, env environment) error {
	changes, err := env.openGit(env.workDir, spec)
	if err != nil {
		return err
	}
	files, err := changes.List()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(stdout, "no changes")
		return nil
	}
	if err := env.runUI(ui.NewGitModel(files, changes)); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	return nil
}

func indexOf(items []string, wanted string) int {
	for i, item := range items {
		if item == wanted {
			return i
		}
	}
	return -1
}

// printError writes err to w with control characters replaced, so file names or git messages
// cannot inject terminal escape sequences.
func printError(w io.Writer, err error) {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '?'
		}
		return r
	}, err.Error())
	fmt.Fprintf(w, "jelly-diff: %s\n", clean)
}

// attributeProfile returns the color profile to use for the mono theme: terminals that can show text
// attributes keep them even though NO_COLOR reduced the profile to plain text.
func attributeProfile(current termenv.Profile, isTerminal bool, term string) termenv.Profile {
	if current == termenv.Ascii && isTerminal && term != "" && term != "dumb" {
		return termenv.ANSI
	}
	return current
}

func enableTextAttributes() {
	info, err := os.Stdout.Stat()
	isTerminal := err == nil && info.Mode()&os.ModeCharDevice != 0
	lipgloss.SetColorProfile(attributeProfile(lipgloss.ColorProfile(), isTerminal, os.Getenv("TERM")))
}
