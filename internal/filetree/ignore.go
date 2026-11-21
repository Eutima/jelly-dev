package filetree

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultIgnored are folders that hold dependencies, caches and tooling files rather than the project's own code.
// Searches skip them (the file tree still shows them).
var DefaultIgnored = []string{
	"node_modules/", ".venv/", "venv/", "__pycache__/", ".mypy_cache/", ".pytest_cache/", ".ruff_cache/", ".tox/",
	".eggs/", "*.egg-info/", ".gradle/", ".idea/", ".next/", ".nuxt/", ".cache/", ".parcel-cache/", ".terraform/",
}

// Ignorer decides which files and folders are hidden: ".git", the patterns of the root .gitignore and .jdignore
// (names and globs, a trailing slash means folders only; "!" lines and comments are skipped) and, when asked for,
// DefaultIgnored.
type Ignorer struct{ patterns []string }

// NewIgnorer reads the ignore files of a project folder.
func NewIgnorer(root string, defaults bool) *Ignorer {
	ig := &Ignorer{patterns: []string{".git/"}}
	if defaults {
		ig.patterns = append(ig.patterns, DefaultIgnored...)
	}
	for _, name := range []string{".gitignore", ".jdignore"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
				continue
			}
			ig.patterns = append(ig.patterns, strings.TrimPrefix(line, "/"))
		}
	}
	return ig
}

// Ignored reports whether an entry is hidden; rel is its path relative to the project, with slashes.
func (ig *Ignorer) Ignored(rel string, dir bool) bool {
	base := path.Base(rel)
	for _, pattern := range ig.patterns {
		onlyDir := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(pattern, "/")
		if onlyDir && !dir {
			continue
		}
		target := base
		if strings.Contains(pattern, "/") {
			target = rel
		}
		if ok, _ := path.Match(pattern, target); ok {
			return true
		}
	}
	return false
}
