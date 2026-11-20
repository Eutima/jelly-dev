package source

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const (
	gitDirName     = ".git"
	compareBufSize = 64 << 10
)

// ErrNotDirectory is returned when a directory comparison is started with something else.
var ErrNotDirectory = errors.New("not a directory")

// DirChanges lists and loads the differences between two directory trees.
// Regular files and symbolic links that point to regular files are compared (git's --dir-diff mode puts
// links to working tree files into its temporary directory); links to directories, dangling links, other
// special files and ".git" directories are skipped.
type DirChanges struct {
	leftLabel  string // as given by the user
	rightLabel string
	left       string // resolved: symbolic links in the given paths are followed
	right      string
}

// OpenDirs prepares the comparison of two directories.
func OpenDirs(left, right string) (*DirChanges, error) {
	var resolved [2]string
	for i, dir := range []string{left, right} {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: %w", dir, ErrNotDirectory)
		}
		// Walking does not descend into a root that is a symbolic link, so resolve it first.
		if resolved[i], err = filepath.EvalSymlinks(dir); err != nil {
			return nil, fmt.Errorf("open %s: %w", dir, err)
		}
	}
	return &DirChanges{leftLabel: left, rightLabel: right, left: resolved[0], right: resolved[1]}, nil
}

// Labels returns the display names of the left and the right directory.
func (d *DirChanges) Labels() (leftLabel, rightLabel string) { return d.leftLabel, d.rightLabel }

// List returns the files that differ, sorted by path: 'M' content differs, 'D' only on the left,
// 'A' only on the right.
func (d *DirChanges) List() ([]ChangedFile, error) {
	leftFiles, err := regularFiles(d.left)
	if err != nil {
		return nil, err
	}
	rightFiles, err := regularFiles(d.right)
	if err != nil {
		return nil, err
	}
	var files []ChangedFile
	for path, leftSize := range leftFiles {
		rightSize, inRight := rightFiles[path]
		if !inRight {
			files = append(files, ChangedFile{Status: StatusDeleted, Path: path})
			continue
		}
		same := leftSize == rightSize
		if same {
			if same, err = sameContent(d.fullPath(d.left, path), d.fullPath(d.right, path)); err != nil {
				return nil, err
			}
		}
		if !same {
			files = append(files, ChangedFile{Status: StatusModified, Path: path})
		}
	}
	for path := range rightFiles {
		if _, inLeft := leftFiles[path]; !inLeft {
			files = append(files, ChangedFile{Status: StatusAdded, Path: path})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Load returns the old (left) and new (right) text of a file; a missing side is empty.
func (d *DirChanges) Load(file ChangedFile) (oldText, newText string, err error) {
	if file.Status != StatusAdded {
		if oldText, err = ReadTextFile(d.fullPath(d.left, file.Path)); err != nil {
			return "", "", err
		}
	}
	if file.Status != StatusDeleted {
		if newText, err = ReadTextFile(d.fullPath(d.right, file.Path)); err != nil {
			return "", "", err
		}
	}
	return oldText, newText, nil
}

func (d *DirChanges) fullPath(root, path string) string {
	return filepath.Join(root, filepath.FromSlash(path))
}

// regularFiles returns the sizes of all regular files below root, keyed by slash separated relative path.
func regularFiles(root string) (map[string]int64, error) {
	files := map[string]int64{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == gitDirName && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		info, ok, err := regularInfo(path, entry)
		if err != nil || !ok {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = info.Size()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	return files, nil
}

// regularInfo returns the file info of a regular file or of a symbolic link to one; ok is false for
// anything else (dangling links, links to directories, special files).
func regularInfo(path string, entry fs.DirEntry) (info fs.FileInfo, ok bool, err error) {
	if entry.Type()&fs.ModeSymlink != 0 {
		info, statErr := os.Stat(path) // follows the link
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, false, nil
		}
		return info, true, nil
	}
	if !entry.Type().IsRegular() {
		return nil, false, nil
	}
	info, err = entry.Info()
	return info, err == nil, err
}

// sameContent compares two files byte by byte without loading them into memory.
func sameContent(leftPath, rightPath string) (bool, error) {
	left, err := openRegular(leftPath)
	if err != nil {
		return false, err
	}
	defer left.Close()
	right, err := openRegular(rightPath)
	if err != nil {
		return false, err
	}
	defer right.Close()

	leftBuf, rightBuf := make([]byte, compareBufSize), make([]byte, compareBufSize)
	for {
		leftN, leftErr := io.ReadFull(left, leftBuf)
		rightN, rightErr := io.ReadFull(right, rightBuf)
		if leftN != rightN || !bytes.Equal(leftBuf[:leftN], rightBuf[:rightN]) {
			return false, nil
		}
		// Invariant: ReadFull returns a nil error only for a full buffer, so equal counts with a nil error on
		// both sides mean both buffers were full and more data may follow.
		if leftErr == nil && rightErr == nil {
			continue
		}
		for _, err := range []error{leftErr, rightErr} {
			if err != nil && !atEnd(err) {
				return false, err
			}
		}
		// Same chunk on both sides: equal only if both files ended here (one may have grown meanwhile).
		return atEnd(leftErr) && atEnd(rightErr), nil
	}
}

func atEnd(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

// openRegular opens a file for reading; it refuses non-regular files and never blocks on a FIFO.
func openRegular(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = ErrNotRegular
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}
