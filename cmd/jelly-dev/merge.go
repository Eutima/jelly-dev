package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/merge"
	"github.com/eutima/jelly-dev/internal/source"
	"github.com/eutima/jelly-dev/internal/ui"
)

const (
	mergeArgCount  = 4 // BASE LOCAL REMOTE MERGED, the order of git mergetool's variables
	pairArgCount   = 2 // A B: merge B into A without git
	defaultDirMode = 0o755
	lf             = "\n"
	crlf           = "\r\n"
)

// runMerge handles `jelly-dev merge BASE LOCAL REMOTE MERGED` (the git mergetool entry point) and
// `jelly-dev merge A B` (two files or directories without git; the result goes into A).
func runMerge(args []string, stdout, stderr io.Writer, env environment) int {
	var err error
	switch len(args) {
	case mergeArgCount:
		err = mergeFiles(args[0], args[1], args[2], args[3], stdout, env)
	case pairArgCount:
		err = mergePair(args[0], args[1], stdout, env)
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

// mergePair merges file b into file a (or directory b into directory a) without a common ancestor.
func mergePair(aPath, bPath string, stdout io.Writer, env environment) error {
	aIsDir, bIsDir := isDirectory(aPath), isDirectory(bPath)
	switch {
	case aIsDir && bIsDir:
		return mergeDirectories(aPath, bPath, stdout, env)
	case aIsDir != bIsDir:
		return fmt.Errorf("cannot merge a directory with a file: %s, %s", aPath, bPath)
	}
	a, err := source.ReadTextFile(aPath)
	if err != nil {
		return err
	}
	b, err := source.ReadTextFile(bPath)
	if err != nil {
		return err
	}
	chunks := merge.TwoWay(diff.SplitLines(a), diff.SplitLines(b))
	titles := ui.MergeTitles{
		Name: filepath.Base(aPath), Ours: filepath.Base(aPath), Result: filepath.Base(aPath), Theirs: filepath.Base(bPath),
	}
	lineEnding := lineEndingOf(a)
	save := func(lines []string) error { return writeResult(aPath, lines, lineEnding) }
	model, outcome := ui.NewMergeModel(titles, chunks, save)
	if model.ChangeCount() == 0 {
		fmt.Fprintln(stdout, "no differences")
		return nil
	}
	return showMerge(model.WithHighlight(aPath, bPath).WithAssistants(env.assistants()...), outcome, env)
}

// lineEndingOf returns the line ending the result is written with: CRLF when the text uses it, else LF.
func lineEndingOf(text string) string {
	if strings.Contains(text, crlf) {
		return crlf
	}
	return lf
}

// showMerge runs the merge viewer; it is an error when the user left without saving the result.
func showMerge(model ui.MergeModel, outcome *ui.MergeOutcome, env environment) error {
	if err := env.runUI(model); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	if !outcome.Saved {
		return errors.New("merge not saved")
	}
	return nil
}

func mergeFiles(basePath, localPath, remotePath, mergedPath string, stdout io.Writer, env environment) error {
	base, err := readBase(basePath)
	if err != nil {
		return err
	}
	local, err := source.ReadTextFile(localPath)
	if err != nil {
		return err
	}
	remote, err := source.ReadTextFile(remotePath)
	if err != nil {
		return err
	}
	lineEnding := lineEndingOf(local)
	save := func(lines []string) error { return writeResult(mergedPath, lines, lineEnding) }

	chunks := merge.Merge(diff.SplitLines(base), diff.SplitLines(local), diff.SplitLines(remote))
	titles := ui.MergeTitles{
		Name: filepath.Base(mergedPath), Ours: filepath.Base(localPath), Result: filepath.Base(mergedPath), Theirs: filepath.Base(remotePath),
	}
	model, outcome := ui.NewMergeModel(titles, chunks, save)
	if model.ChangeCount() == 0 {
		fmt.Fprintln(stdout, "nothing to merge")
		return save(model.Result())
	}
	return showMerge(model.WithHighlight(mergedPath, localPath, remotePath).WithAssistants(env.assistants()...), outcome, env)
}

// mergeDirectories lists the files that differ between two directories; each opens in the merge viewer and is
// saved into directory a. A file that exists on one side only is merged against an empty counterpart.
func mergeDirectories(aDir, bDir string, stdout io.Writer, env environment) error {
	changes, err := source.OpenDirs(aDir, bDir)
	if err != nil {
		return err
	}
	var files []source.ChangedFile
	err = env.busy("comparing directories ...", func() (listErr error) {
		files, listErr = changes.List()
		return listErr
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(stdout, "no differences")
		return nil
	}
	open := func(file source.ChangedFile) (ui.MergeModel, *ui.MergeOutcome, error) {
		aPath, bPath := changes.Paths(file.Path)
		// Read what exists now, not what the list said: a file created in a since (for example by an earlier save)
		// must never be treated as empty.
		a, err := readIfExists(aPath)
		if err != nil {
			return ui.MergeModel{}, nil, err
		}
		b, err := readIfExists(bPath)
		if err != nil {
			return ui.MergeModel{}, nil, err
		}
		chunks := merge.TwoWay(diff.SplitLines(a), diff.SplitLines(b))
		lineEnding := lineEndingOf(a) // a's own line ending; b's when the file is new
		if a == "" {
			lineEnding = lineEndingOf(b)
		}
		save := func(lines []string) error {
			if err := os.MkdirAll(filepath.Dir(aPath), defaultDirMode); err != nil {
				return fmt.Errorf("write %s: %w", aPath, err)
			}
			return writeResult(aPath, lines, lineEnding)
		}
		titles := ui.MergeTitles{
			Name: file.Path, Ours: filepath.Join(aDir, file.Path), Result: file.Path, Theirs: filepath.Join(bDir, file.Path),
		}
		model, outcome := ui.NewMergeModel(titles, chunks, save)
		if model.ChangeCount() == 0 {
			return ui.MergeModel{}, nil, errors.New("no line differences (the files are equal, empty, or differ only in line endings)")
		}
		return model.WithHighlight(aPath, bPath).WithAssistants(env.assistants()...), outcome, nil
	}
	if err := env.runUI(ui.NewDirMergeModel(aDir, bDir, files, open)); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	return nil
}

// readIfExists reads a text file; a file that does not exist counts as empty.
func readIfExists(path string) (string, error) {
	text, err := source.ReadTextFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return text, err
}

// readBase reads the common ancestor; a missing file (add/add conflict) counts as empty.
func readBase(path string) (string, error) {
	text, err := source.ReadTextFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return text, err
}

// writeResult writes the merged lines, each terminated by lineEnding.
func writeResult(path string, lines []string, lineEnding string) error {
	var text strings.Builder
	for _, line := range lines {
		text.WriteString(line)
		text.WriteString(lineEnding)
	}
	return writeAtomic(path, []byte(text.String()))
}

// writeAtomic replaces path through a temporary file in the same directory, keeping the file mode.
func writeAtomic(path string, data []byte) error { return source.WriteAtomic(path, data) }
