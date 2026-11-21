package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxFolderMatches = 200

// folderMatches returns the folders that complete a typed path: the entries of the folder before the last
// separator whose names start with what follows it, each written the way the user typed the path (a leading "~/" stays),
// with a trailing separator. Hidden folders are only offered when the typed name starts with a dot. Without a match
// of the exact case, names that match ignoring case are offered.
func folderMatches(input string) []string {
	home, _ := os.UserHomeDir()
	typedDir, prefix := splitTyped(input)
	dir := typedDir
	if strings.HasPrefix(dir, "~") && home != "" {
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !isFolderEntry(dir, entry) || (strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	matches := pick(names, func(name string) bool { return strings.HasPrefix(name, prefix) })
	if len(matches) == 0 {
		lower := strings.ToLower(prefix)
		matches = pick(names, func(name string) bool { return strings.HasPrefix(strings.ToLower(name), lower) })
	}
	if len(matches) > maxFolderMatches {
		matches = matches[:maxFolderMatches]
	}
	for i, name := range matches {
		matches[i] = typedDir + name + string(filepath.Separator)
	}
	return matches
}

// splitTyped splits a typed path at its last separator: the folder part (with the separator) and the name typed so far.
func splitTyped(input string) (dir, prefix string) {
	if input == "~" {
		return "~" + string(filepath.Separator), ""
	}
	index := strings.LastIndexAny(input, "/"+string(filepath.Separator))
	return input[:index+1], input[index+1:]
}

func pick(names []string, keep func(string) bool) []string {
	var out []string
	for _, name := range names {
		if keep(name) {
			out = append(out, name)
		}
	}
	return out
}

// isFolderEntry reports whether a directory entry is a folder; links to folders count.
func isFolderEntry(dir string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink != 0 {
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		return err == nil && info.IsDir()
	}
	return false
}

// commonPrefix returns the longest prefix all strings share.
func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
