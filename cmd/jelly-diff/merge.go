package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eutima/jelly-diff/internal/diff"
	"github.com/eutima/jelly-diff/internal/merge"
	"github.com/eutima/jelly-diff/internal/source"
	"github.com/eutima/jelly-diff/internal/ui"
)

const (
	mergeArgCount = 4 // BASE LOCAL REMOTE MERGED, the order of git mergetool's variables
	defaultMode   = 0o644
	lf            = "\n"
	crlf          = "\r\n"
)

// runMerge handles `jelly-diff merge BASE LOCAL REMOTE MERGED` (the git mergetool entry point).
func runMerge(args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) != mergeArgCount {
		fmt.Fprintf(stderr, "%s\n", usage)
		return exitUsage
	}
	if err := mergeFiles(args[0], args[1], args[2], args[3], stdout, env); err != nil {
		printError(stderr, err)
		return exitFailure
	}
	return exitOK
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
	lineEnding := lf
	if strings.Contains(local, crlf) {
		lineEnding = crlf
	}
	save := func(lines []string) error { return writeResult(mergedPath, lines, lineEnding) }

	chunks := merge.Merge(diff.SplitLines(base), diff.SplitLines(local), diff.SplitLines(remote))
	titles := ui.MergeTitles{Ours: "ours: " + localPath, Result: "result: " + mergedPath, Theirs: "theirs: " + remotePath}
	model, outcome := ui.NewMergeModel(titles, chunks, save)
	if model.ChangeCount() == 0 {
		fmt.Fprintln(stdout, "nothing to merge")
		return save(model.Result())
	}
	model = model.WithHighlight(mergedPath, localPath, remotePath)
	if err := env.runUI(model); err != nil {
		return fmt.Errorf("run ui: %w", err)
	}
	if !outcome.Saved {
		return errors.New("merge not saved")
	}
	return nil
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
func writeAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := fs.FileMode(defaultMode)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".jelly-diff-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tempName := temp.Name()
	cleanup := func(cause error) error {
		temp.Close()
		os.Remove(tempName)
		return fmt.Errorf("write %s: %w", path, cause)
	}
	if _, err := temp.Write(data); err != nil {
		return cleanup(err)
	}
	if err := temp.Chmod(mode); err != nil {
		return cleanup(err)
	}
	if err := temp.Sync(); err != nil { // make sure the data is on disk before it replaces the old file
		return cleanup(err)
	}
	if err := temp.Close(); err != nil {
		return cleanup(err)
	}
	if err := os.Rename(tempName, target); err != nil {
		return cleanup(err)
	}
	return nil
}
