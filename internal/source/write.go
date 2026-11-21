package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const defaultFileMode = 0o644

// WriteAtomic replaces path through a temporary file in the same directory, keeping the file mode.
func WriteAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := fs.FileMode(defaultFileMode)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".jelly-dev-*")
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
