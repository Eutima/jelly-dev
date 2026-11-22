package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrChangedOnDisk is returned by WriteWorktree when the file is not what the caller showed to the user any more.
var ErrChangedOnDisk = errors.New("the file changed on disk meanwhile - select it again to reload it")

// ErrReadOnlySide is returned when a file cannot be edited in this comparison.
var ErrReadOnlySide = errors.New("this comparison is read-only")

// Editable reports whether the new side of a changed file is a working tree file that jd may write: the comparison has the
// working tree on its new side and the file exists there as a regular file (not deleted, not in conflict).
func (g *GitChanges) Editable(file ChangedFile) bool {
	if g.newSide.kind != sideWorktree || g.twoRevs {
		return false
	}
	switch file.Status {
	case StatusDeleted, StatusUnmerged, StatusTypeChanged:
		return false
	}
	info, err := os.Lstat(g.worktreePath(file.Path))
	return err == nil && info.Mode().IsRegular()
}

// WriteWorktree replaces the working tree file with text. expected is the text the caller last read: when the file holds
// something else, nothing is written (ErrChangedOnDisk), so edits made by another program are never overwritten.
func (g *GitChanges) WriteWorktree(ctx context.Context, file ChangedFile, expected, text string) error {
	if !g.Editable(file) {
		return fmt.Errorf("%s: %w", file.Path, ErrReadOnlySide)
	}
	current, err := g.readWorktree(ctx, file.Path) // also refuses paths that leave the repository
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("%s: %w", file.Path, ErrChangedOnDisk)
	}
	return WriteAtomic(g.worktreePath(file.Path), []byte(text))
}

func (g *GitChanges) worktreePath(path string) string {
	return filepath.Join(g.root, filepath.FromSlash(path))
}
