package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// privateTempDir returns a folder of the current user in the temp dir for the project list, used when there is no home
// directory. The name is predictable, so the folder is created private (0700) and one that is not a real folder (a symlink
// or something another user prepared with looser rights) is refused.
func privateTempDir() (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("jd-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return "", fmt.Errorf("%s is not a private folder; set %s or $HOME instead", dir, EnvConfigDir)
	}
	return dir, nil
}
