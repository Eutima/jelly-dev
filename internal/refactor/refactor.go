// Package refactor applies the edits of a language server's rename to the files of a project and keeps a journal, so the last
// refactorings can be undone: the journal holds the text every changed file had before.
package refactor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/source"
)

const (
	maxJournal = 20
	dirMode    = 0o755

	lockAttempts = 20
	lockWait     = 100 * time.Millisecond
	staleLock    = time.Minute
)

// ErrNothingToUndo is returned by Undo when the journal is empty.
var ErrNothingToUndo = errors.New("there is no refactoring to undo")

// FileState is a changed file: its text before the refactoring and a hash of what the refactoring wrote.
type FileState struct {
	Path     string `json:"path"` // where the text was edited (before the renames)
	Original string `json:"original"`
	After    string `json:"after"` // sha256 of the new text, to notice later changes
}

// Move is a renamed file.
type Move struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// Entry is one refactoring in the journal.
type Entry struct {
	Label string      `json:"label"`
	Time  time.Time   `json:"time"`
	Files []FileState `json:"files,omitempty"`
	Moves []Move      `json:"moves,omitempty"`
}

// Result tells what Apply or Undo changed.
type Result struct {
	Label string
	Files int // files whose text changed
	Edits int // text edits (Apply only)
	Moves int // files renamed
	// Moved lists the renamed files as they were done (Apply) or are undone (Undo: Old is where the file was again put).
	Moved []Move
}

// Journal is the list of refactorings of a project, newest last, kept in a file.
type Journal struct {
	path string
	root string
}

// JournalPath is where the journal of the project in root lives.
func JournalPath(root string) string { return filepath.Join(root, ".jd", "refactor-journal.json") }

// Open returns the journal of the project in root.
func Open(root string) *Journal { return &Journal{path: JournalPath(root), root: root} }

func (j *Journal) load() ([]Entry, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the refactoring journal: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("read the refactoring journal %s: %w", j.path, err)
	}
	return entries, nil
}

func (j *Journal) save(entries []Entry) error {
	if len(entries) > maxJournal {
		entries = entries[len(entries)-maxJournal:]
	}
	if err := os.MkdirAll(filepath.Dir(j.path), dirMode); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(j.path, append(data, '\n'))
}

// Last returns the refactoring Undo would revert.
func (j *Journal) Last() (Entry, bool) {
	entries, err := j.load()
	if err != nil || len(entries) == 0 {
		return Entry{}, false
	}
	return entries[len(entries)-1], true
}

func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// resolved follows the symbolic links of a path; for a path that does not exist yet the nearest folder that does is resolved.
func resolved(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Join(path, rest)
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// inside reports whether path is the project folder or below it, also through symbolic links: a link in the project that leads out
// of it does not count.
func (j *Journal) inside(path string) bool {
	rel, err := filepath.Rel(resolved(j.root), resolved(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// lock makes one window at a time change the journal and the files (another jd in the same project waits for it, then fails).
func (j *Journal) lock() (unlock func(), err error) {
	lockPath := j.path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), dirMode); err != nil {
		return nil, err
	}
	for range lockAttempts {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLock {
			_ = os.Remove(lockPath) // left behind by a window that died
			continue
		}
		time.Sleep(lockWait)
	}
	return nil, errors.New("another jd window is refactoring this project: try again in a moment")
}

// Apply writes the edit to disk: first the text edits, then the file renames. Nothing is written when any part cannot be done
// (a file outside the project, an edit that does not fit its file, a target that exists). The old texts go into the journal first.
func (j *Journal) Apply(label string, edit lsp.WorkspaceEdit) (Result, error) {
	unlock, err := j.lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	entry := Entry{Label: label, Time: time.Now()}
	newTexts := make([]string, 0, len(edit.Files))
	for _, file := range edit.Files {
		if !j.inside(file.Path) {
			return Result{}, fmt.Errorf("the refactoring changes %s, outside the project: not applied", file.Path)
		}
		data, err := os.ReadFile(file.Path) // #nosec G304 -- inside the project, checked above
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", file.Path, err)
		}
		updated, err := lsp.ApplyEdits(string(data), file.Edits)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", file.Path, err)
		}
		if updated == string(data) {
			continue
		}
		entry.Files = append(entry.Files, FileState{Path: file.Path, Original: string(data), After: hash(updated)})
		newTexts = append(newTexts, updated)
	}
	for _, move := range edit.Renames {
		if !j.inside(move.Old) || !j.inside(move.New) {
			return Result{}, fmt.Errorf("the refactoring moves %s outside the project: not applied", move.Old)
		}
		if _, err := os.Lstat(move.New); err == nil {
			return Result{}, fmt.Errorf("%s exists already: not applied", move.New)
		}
		entry.Moves = append(entry.Moves, Move{Old: move.Old, New: move.New})
	}
	if len(entry.Files) == 0 && len(entry.Moves) == 0 {
		return Result{}, errors.New("the refactoring changes nothing")
	}
	entries, err := j.load()
	if err != nil {
		return Result{}, err
	}
	if err := j.save(append(entries, entry)); err != nil { // the journal comes first: a crash half way can still be undone
		return Result{}, fmt.Errorf("write the refactoring journal: %w", err)
	}
	if err := writeAll(entry, newTexts); err != nil {
		_ = revert(entry, false)
		_ = j.save(entries)
		return Result{}, err
	}
	return Result{Label: label, Files: len(entry.Files), Edits: edit.EditCount(), Moves: len(entry.Moves), Moved: entry.Moves}, nil
}

func writeAll(entry Entry, newTexts []string) error {
	for i, file := range entry.Files {
		if err := source.WriteAtomic(file.Path, []byte(newTexts[i])); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	for _, move := range entry.Moves {
		if err := os.MkdirAll(filepath.Dir(move.New), dirMode); err != nil {
			return err
		}
		if err := os.Rename(move.Old, move.New); err != nil {
			return fmt.Errorf("rename %s: %w", move.Old, err)
		}
	}
	return nil
}

// current returns where a file is after the moves of an entry.
func current(entry Entry, path string) string {
	for _, move := range entry.Moves {
		if move.Old == path {
			return move.New
		}
	}
	return path
}

// Undo reverts the newest refactoring. It refuses when a file changed since (its text is not the one the refactoring wrote),
// so later work is never overwritten, and then keeps the journal as it was.
func (j *Journal) Undo() (Result, error) {
	unlock, err := j.lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	entries, err := j.load()
	if err != nil {
		return Result{}, err
	}
	if len(entries) == 0 {
		return Result{}, ErrNothingToUndo
	}
	entry := entries[len(entries)-1]
	for _, file := range entry.Files {
		data, err := os.ReadFile(current(entry, file.Path)) // #nosec G304 -- inside the project: Apply checked it
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", current(entry, file.Path), err)
		}
		if hash(string(data)) != file.After {
			return Result{}, fmt.Errorf("%s changed after the refactoring: not undone (the journal keeps it)", current(entry, file.Path))
		}
	}
	for _, move := range entry.Moves {
		if _, err := os.Lstat(move.New); err != nil {
			return Result{}, fmt.Errorf("%s is gone: not undone", move.New)
		}
		if _, err := os.Lstat(move.Old); err == nil {
			return Result{}, fmt.Errorf("%s exists again: not undone", move.Old)
		}
	}
	if err := revert(entry, true); err != nil {
		return Result{}, err
	}
	if err := j.save(entries[:len(entries)-1]); err != nil {
		return Result{}, fmt.Errorf("write the refactoring journal: %w", err)
	}
	return Result{Label: entry.Label, Files: len(entry.Files), Moves: len(entry.Moves), Moved: entry.Moves}, nil
}

// revert moves the files back and restores their old texts. With wrote false, files that were never written (a failed Apply)
// are left alone when they are missing.
func revert(entry Entry, wrote bool) error {
	for i := len(entry.Moves) - 1; i >= 0; i-- {
		move := entry.Moves[i]
		if _, err := os.Lstat(move.New); err != nil && !wrote {
			continue
		}
		if err := os.Rename(move.New, move.Old); err != nil {
			return fmt.Errorf("rename %s back: %w", move.New, err)
		}
	}
	for _, file := range entry.Files {
		if err := source.WriteAtomic(file.Path, []byte(file.Original)); err != nil {
			return fmt.Errorf("restore %s: %w", file.Path, err)
		}
	}
	return nil
}
