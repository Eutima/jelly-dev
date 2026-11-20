// Command jelly-diff is a terminal diff and merge tool with a JetBrains-style UI.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

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
  jelly-diff --clip-left <file>      compare the clipboard (left) with a file`
)

// uiRunner shows a model in the terminal and blocks until the user quits.
type uiRunner func(model tea.Model) error

// environment holds everything run needs from the outside world, so tests can replace it.
type environment struct {
	runUI         uiRunner
	readClipboard func() (string, error)
}

func main() {
	env := environment{runUI: runProgram, readClipboard: source.ReadClipboard}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, env))
}

func runProgram(model tea.Model) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// run executes the command and returns the process exit code.
func run(args []string, stdout, stderr io.Writer, env environment) int {
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

	var err error
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
		fmt.Fprintf(stderr, "jelly-diff: %v\n", err)
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
