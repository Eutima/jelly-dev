// Package search finds files of a project by name and by content. Folders that hold dependencies and caches
// (node_modules, .venv, ...) and everything the project's .gitignore or .jdignore hides are skipped.
package search

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eutima/jelly-dev/internal/filetree"
)

// MaxFiles is the most files a search looks at; a repository with more is cut off (the result says so).
const MaxFiles = 200000

// Files lists the files of a project, as paths relative to root with slashes, sorted. Folders that are ignored are not
// entered at all. truncated reports that the limit was hit.
func Files(ctx context.Context, root string) (files []string, truncated bool, err error) {
	ig := filetree.NewIgnorer(root, true)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir // an unreadable folder does not stop the search
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if ig.Ignored(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil // symbolic links, sockets and the like are not searched
		}
		if len(files) >= MaxFiles {
			truncated = true
			return filepath.SkipAll
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(files)
	return files, truncated, nil
}

// FileMatch is a file that matches a name query.
type FileMatch struct {
	Path  string
	Score int
}

// MatchFiles returns the files whose path matches the query, best first, at most limit. The query is split at
// spaces; every word must appear in the path (case-insensitive): as a subsequence when fuzzy is set, else in one piece
// ("contains"). Matches in the file name, at the start of words and in one piece score higher. An empty query matches
// everything in the order of files.
func MatchFiles(files []string, query string, limit int, fuzzy bool) []FileMatch {
	terms := strings.Fields(strings.ToLower(query))
	var out []FileMatch
	for _, file := range files {
		score, ok := scorePath(file, terms, fuzzy)
		if !ok {
			continue
		}
		out = append(out, FileMatch{Path: file, Score: score})
		if len(terms) == 0 && len(out) >= limit {
			break
		}
	}
	if len(terms) > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Score != out[j].Score {
				return out[i].Score > out[j].Score
			}
			return len(out[i].Path) < len(out[j].Path)
		})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func scorePath(path string, terms []string, fuzzy bool) (int, bool) {
	lower := strings.ToLower(path)
	base := strings.LastIndexByte(lower, '/') + 1
	total := 0
	for _, term := range terms {
		score, ok := scoreTerm(lower, base, term, fuzzy)
		if !ok {
			return 0, false
		}
		total += score
	}
	return total - len(path)/8, true
}

// scoreTerm matches one word as a subsequence of the path and rates the best placement it finds: it looks for the
// word in one piece first (in the file name, then anywhere), then, when fuzzy is set, letter by letter.
func scoreTerm(lower string, base int, term string, fuzzy bool) (int, bool) {
	if i := strings.LastIndex(lower[base:], term); i >= 0 {
		score := 100 + 20*len(term)
		if i == 0 {
			score += 40 // the file name starts with it
		}
		if lower[base:] == term {
			score += 60
		}
		return score, true
	}
	if i := strings.LastIndex(lower, term); i >= 0 {
		score := 50 + 10*len(term)
		if i == 0 || lower[i-1] == '/' {
			score += 20
		}
		return score, true
	}
	if !fuzzy {
		return 0, false
	}
	score, pos, previous := 0, 0, -2
	for _, r := range term {
		found := strings.IndexRune(lower[pos:], r)
		if found < 0 {
			return 0, false
		}
		at := pos + found
		switch {
		case at == previous+1:
			score += 15
		case at == 0 || strings.ContainsRune("/._-", rune(lower[at-1])):
			score += 12 // the start of a word
		default:
			score += 5
		}
		if at >= base {
			score += 3
		}
		previous, pos = at, at+len(string(r))
	}
	return score, true
}
