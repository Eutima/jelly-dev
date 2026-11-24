package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	scratchFolder = "scratches"
	// DefaultScratchExt is added to the name of a scratch file that has none.
	DefaultScratchExt = ".txt"
)

// Scratch is one scratch file: a file of the user that belongs to no project.
type Scratch struct {
	Name     string
	Modified time.Time
}

// ScratchDir returns the folder of the scratch files: <config dir>/scratches.
func ScratchDir(configDir string) string { return filepath.Join(configDir, scratchFolder) }

// ValidScratchName returns the file name of a scratch file typed by the user: a plain file name (no folder), with .txt added when it
// has no extension.
func ValidScratchName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%q is not a scratch file name (no folders)", name)
	}
	if filepath.Ext(name) == "" || strings.HasSuffix(name, ".") {
		name = strings.TrimSuffix(name, ".") + DefaultScratchExt
	}
	return name, nil
}

// ScratchPath returns the file of the scratch file called name.
func ScratchPath(configDir, name string) (string, error) {
	name, err := ValidScratchName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(ScratchDir(configDir), name), nil
}

// NewScratchName returns a file name for a scratch file that is not named by the user.
func NewScratchName(now time.Time) string {
	return "scratch-" + now.Format("20060102-150405") + DefaultScratchExt
}

// ListScratches returns the scratch files, the most recently changed first; no folder yet means none.
func ListScratches(configDir string) ([]Scratch, error) {
	entries, err := os.ReadDir(ScratchDir(configDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the scratch files: %w", err)
	}
	var list []Scratch
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		list = append(list, Scratch{Name: entry.Name(), Modified: info.ModTime()})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Modified.After(list[j].Modified) })
	return list, nil
}
