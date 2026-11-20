// Package source loads the texts that are compared (files, clipboard, git).
package source

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

// ErrBinary is returned when a file looks like binary data.
var ErrBinary = errors.New("binary file")

// ReadTextFile returns the content of path, or an error if it cannot be read or looks binary.
func ReadTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%s: %w", path, ErrBinary)
	}
	return string(data), nil
}
