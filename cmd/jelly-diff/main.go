// Command jelly-diff is a terminal diff and merge tool with a JetBrains-style UI.
package main

import (
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
	usage       = "usage: jelly-diff <file-a> <file-b>"
)

// uiRunner shows a model in the terminal and blocks until the user quits.
type uiRunner func(model tea.Model) error

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, runProgram)) }

func runProgram(model tea.Model) error {
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// run executes the command and returns the process exit code.
func run(args []string, stdout, stderr io.Writer, runUI uiRunner) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, usage)
		return exitFailure
	}
	if err := diffFiles(args[0], args[1], stdout, runUI); err != nil {
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
	model := ui.NewDiffModel(leftPath, rightPath, diff.SplitLines(left), diff.SplitLines(right))
	if model.HunkCount() == 0 {
		fmt.Fprintln(stdout, "files are identical")
		return nil
	}
	if err := runUI(model); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	return nil
}
