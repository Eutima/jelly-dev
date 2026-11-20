// Package source loads the texts that are compared (files, clipboard, git).
package source

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// MaxTextBytes is the largest file or blob that is loaded; bigger inputs are rejected.
const MaxTextBytes = 64 << 20

var (
	// ErrBinary is returned when a file looks like binary data.
	ErrBinary = errors.New("binary file")
	// ErrTooLarge is returned when a file exceeds MaxTextBytes.
	ErrTooLarge = errors.New("file too large")
	// ErrNotRegular is returned for directories, devices, sockets and similar.
	ErrNotRegular = errors.New("not a regular file")
	// ErrUnmerged is returned for files with unresolved merge conflicts.
	ErrUnmerged = errors.New("unmerged file")
	// ErrSubmodule is returned for submodule entries, which have no file content.
	ErrSubmodule = errors.New("submodule")
)

// ReadTextFile returns the content of path, or an error if it cannot be read, is not a regular
// file, is too large or looks binary. Symbolic links are followed.
func ReadTextFile(path string) (string, error) {
	// Check before opening: opening a FIFO would block until a writer appears.
	before, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if !before.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	// O_NONBLOCK keeps open from blocking if the file was swapped for a FIFO after the check.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return checkText(path, data)
}

// checkText rejects oversized and binary data and returns the data as a string.
func checkText(name string, data []byte) (string, error) {
	if len(data) > MaxTextBytes {
		return "", fmt.Errorf("%s: %w", name, ErrTooLarge)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s: %w", name, ErrBinary)
	}
	return string(data), nil
}
