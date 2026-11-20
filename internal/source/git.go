package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	gitTimeout     = 30 * time.Second
	maxStderrBytes = 4096
	emptyTreeHash  = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	headRef        = "HEAD"
	indexLabel     = "index"
	worktreeLabel  = "working tree"
	emptyLabel     = "empty"
	gitlinkMode    = "160000"
)

// Change statuses as reported by `git diff --name-status` ('?' marks an untracked file).
const (
	StatusModified    = 'M'
	StatusAdded       = 'A'
	StatusDeleted     = 'D'
	StatusRenamed     = 'R'
	StatusCopied      = 'C'
	StatusTypeChanged = 'T'
	StatusUnmerged    = 'U'
	StatusUntracked   = '?'
)

// ErrOutsideRepository is returned when a path resolves to a location outside the repository.
var ErrOutsideRepository = errors.New("path is outside the repository")

// ErrInvalidRevision is returned for revisions that are unknown or unsafe to pass to git.
var ErrInvalidRevision = errors.New("invalid revision")

// ChangedFile is one entry of the change list.
type ChangedFile struct {
	Status  byte
	Path    string
	OldPath string // only for renames and copies, otherwise empty
}

// GitSpec selects what is compared.
//
//	{}                     working tree against HEAD (plus untracked files)
//	{Staged: true}         index against HEAD
//	{Rev: "main"}          working tree against a revision
//	{Rev: "a..b"}          two revisions
//
// Paths limits the change list to pathspecs (relative to the directory git is started in).
type GitSpec struct {
	Staged bool
	Rev    string
	Paths  []string
}

type sideKind int

const (
	sideWorktree sideKind = iota
	sideIndex
	sideRev
)

type side struct {
	kind sideKind
	ref  string
}

// GitChanges lists and loads the changes of one repository for one GitSpec.
type GitChanges struct {
	dir       string
	root      string
	spec      GitSpec
	oldSide   side
	newSide   side
	oldLabel  string
	newLabel  string
	diffArgs  []string // revision arguments for `git diff`
	untracked bool
	twoRevs   bool // compares two revisions, so the index and conflicts are irrelevant
}

// OpenGit resolves the repository around dir and validates spec.
func OpenGit(dir string, spec GitSpec) (*GitChanges, error) {
	if strings.HasPrefix(spec.Rev, "-") {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRevision, spec.Rev)
	}
	out, err := runGit(dir, MaxTextBytes, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("open git repository: %w", err)
	}
	changes := &GitChanges{dir: dir, root: strings.TrimSpace(string(out)), spec: spec}
	if err := changes.resolve(); err != nil {
		return nil, err
	}
	return changes, nil
}

// resolve fills in the sides, labels and git arguments for the spec.
func (g *GitChanges) resolve() error {
	spec := g.spec
	switch {
	case strings.Contains(spec.Rev, "..."):
		return fmt.Errorf("%w: %q (three-dot ranges are not supported)", ErrInvalidRevision, spec.Rev)
	case strings.Contains(spec.Rev, ".."):
		if spec.Staged {
			return fmt.Errorf("%w: --staged cannot be combined with a range", ErrInvalidRevision)
		}
		from, to, _ := strings.Cut(spec.Rev, "..")
		from, to = g.orHead(from), g.orHead(to)
		if err := g.verify(from, to); err != nil {
			return err
		}
		g.oldSide, g.newSide = side{sideRev, from}, side{sideRev, to}
		g.oldLabel, g.newLabel = from, to
		g.diffArgs = []string{from, to}
		g.twoRevs = true
	default:
		base, label := g.baseRev(spec.Rev)
		if spec.Rev != "" {
			if err := g.verify(spec.Rev); err != nil {
				return err
			}
		}
		g.oldSide, g.oldLabel = side{sideRev, base}, label
		g.diffArgs = []string{base}
		if spec.Staged {
			g.newSide, g.newLabel = side{kind: sideIndex}, indexLabel
			g.diffArgs = []string{"--cached", base}
		} else {
			g.newSide, g.newLabel = side{kind: sideWorktree}, worktreeLabel
			g.untracked = true
		}
	}
	return nil
}

// baseRev returns the revision to compare against and its label; without a HEAD commit it is the empty tree.
func (g *GitChanges) baseRev(rev string) (ref, label string) {
	if rev != "" {
		return rev, rev
	}
	if _, err := runGit(g.dir, MaxTextBytes, "rev-parse", "--verify", "--quiet", headRef); err != nil {
		return emptyTreeHash, emptyLabel
	}
	return headRef, headRef
}

func (g *GitChanges) orHead(rev string) string {
	if rev == "" {
		return headRef
	}
	return rev
}

// verify checks that every revision exists and is safe to use as a command argument.
func (g *GitChanges) verify(revs ...string) error {
	for _, rev := range revs {
		if strings.HasPrefix(rev, "-") {
			return fmt.Errorf("%w: %q", ErrInvalidRevision, rev)
		}
		if _, err := runGit(g.dir, MaxTextBytes, "rev-parse", "--verify", "--quiet", rev+"^{object}"); err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidRevision, rev)
		}
	}
	return nil
}

// Labels returns the display names of the old and the new side.
func (g *GitChanges) Labels() (oldLabel, newLabel string) { return g.oldLabel, g.newLabel }

// List returns the changed files, sorted by path.
func (g *GitChanges) List() ([]ChangedFile, error) {
	args := append([]string{"diff", "--no-ext-diff", "--no-color", "--name-status", "-z", "-M"}, g.diffArgs...)
	args = append(args, "--")
	args = append(args, g.spec.Paths...)
	out, err := runGit(g.dir, MaxTextBytes, args...)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	files, err := parseNameStatus(out)
	if err != nil {
		return nil, err
	}
	if !g.twoRevs {
		if err := g.markUnmerged(files); err != nil {
			return nil, err
		}
	}
	if g.untracked {
		untracked, err := g.listUntracked()
		if err != nil {
			return nil, err
		}
		files = append(files, untracked...)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (g *GitChanges) listUntracked() ([]ChangedFile, error) {
	args := append([]string{"ls-files", "-z", "--others", "--exclude-standard", "--full-name", "--"}, g.spec.Paths...)
	out, err := runGit(g.dir, MaxTextBytes, args...)
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	var files []ChangedFile
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files = append(files, ChangedFile{Status: StatusUntracked, Path: path})
		}
	}
	return files, nil
}

// parseNameStatus parses the NUL separated output of `git diff --name-status -z`.
func parseNameStatus(out []byte) ([]ChangedFile, error) {
	fields := strings.Split(string(out), "\x00")
	var files []ChangedFile
	for i := 0; i < len(fields) && fields[i] != ""; {
		status := fields[i]
		i++
		pathCount := 1
		if status[0] == StatusRenamed || status[0] == StatusCopied {
			pathCount = 2
		}
		if i+pathCount > len(fields) || fields[i+pathCount-1] == "" {
			return nil, fmt.Errorf("parse git output: truncated entry %q", status)
		}
		file := ChangedFile{Status: status[0], Path: fields[i]}
		if pathCount == 2 {
			file.OldPath, file.Path = fields[i], fields[i+1]
		}
		files = append(files, file)
		i += pathCount
	}
	return files, nil
}

// Load returns the old and the new text of a changed file. A side that does not exist
// (added, deleted or untracked files) is returned as an empty string.
func (g *GitChanges) Load(file ChangedFile) (oldText, newText string, err error) {
	if file.Status == StatusUnmerged {
		return "", "", fmt.Errorf("%s: %w", file.Path, ErrUnmerged)
	}
	if file.Status != StatusAdded && file.Status != StatusUntracked {
		oldPath := file.Path
		if file.OldPath != "" {
			oldPath = file.OldPath
		}
		if oldText, err = g.read(g.oldSide, oldPath); err != nil {
			return "", "", err
		}
	}
	if file.Status != StatusDeleted {
		if newText, err = g.read(g.newSide, file.Path); err != nil {
			return "", "", err
		}
	}
	return oldText, newText, nil
}

func (g *GitChanges) read(from side, path string) (string, error) {
	if from.kind == sideWorktree {
		return g.readWorktree(path)
	}
	object := ":0:" + path
	if from.kind == sideRev {
		object = from.ref + ":" + path
	}
	data, err := runGit(g.root, MaxTextBytes+1, "cat-file", "blob", object)
	if err != nil {
		if g.isGitlink(from, path) {
			return "", fmt.Errorf("%s: %w", path, ErrSubmodule)
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return checkText(path, data)
}

// readWorktree reads a file of the working tree. A symbolic link is shown as its target path (as git
// stores it) and is never followed; paths that leave the repository through a symlinked directory are rejected.
func (g *GitChanges) readWorktree(path string) (string, error) {
	full := filepath.Join(g.root, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		return target, nil
	}
	root, err := filepath.EvalSymlinks(g.root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: %w", path, ErrOutsideRepository)
	}
	return ReadTextFile(full)
}

// isGitlink reports whether path is a submodule entry (mode 160000) on the given side.
func (g *GitChanges) isGitlink(from side, path string) bool {
	var out []byte
	var err error
	switch from.kind {
	case sideRev:
		out, err = runGit(g.root, MaxTextBytes, "ls-tree", "-z", from.ref, "--", path)
	case sideIndex:
		out, err = runGit(g.root, MaxTextBytes, "ls-files", "-s", "-z", "--", ":(literal)"+path)
	default:
		return false
	}
	return err == nil && strings.HasPrefix(string(out), gitlinkMode+" ")
}

// markUnmerged sets StatusUnmerged on files that have unresolved merge conflicts.
func (g *GitChanges) markUnmerged(files []ChangedFile) error {
	args := append([]string{"ls-files", "-u", "-z", "--full-name", "--"}, g.spec.Paths...)
	out, err := runGit(g.dir, MaxTextBytes, args...)
	if err != nil {
		return fmt.Errorf("list unmerged files: %w", err)
	}
	unmerged := map[string]bool{}
	for _, entry := range strings.Split(string(out), "\x00") {
		// format: "<mode> <object> <stage>\t<path>"
		if _, path, found := strings.Cut(entry, "\t"); found {
			unmerged[path] = true
		}
	}
	for i := range files {
		if unmerged[files[i].Path] {
			files[i].Status = StatusUnmerged
		}
	}
	return nil
}

// limitedBuffer collects output and fails once more than max bytes were written.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		l.overflow = true
		return 0, ErrTooLarge
	}
	return l.buf.Write(p)
}

// headWriter keeps the first max bytes and silently discards the rest.
type headWriter struct {
	buf bytes.Buffer
	max int
}

func (h *headWriter) Write(p []byte) (int, error) {
	if room := h.max - h.buf.Len(); room > 0 {
		h.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// runGit runs git in dir and returns its stdout, failing if it exceeds maxBytes.
func runGit(dir string, maxBytes int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"--no-pager", "-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) // #nosec G204 -- fixed binary, arguments validated by callers
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = time.Second
	stdout := &limitedBuffer{max: maxBytes}
	stderr := &headWriter{max: maxStderrBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("git timed out after %s", gitTimeout)
		}
		// git is killed by SIGPIPE once we stop reading, so its exit error hides the real cause.
		if stdout.overflow {
			return nil, ErrTooLarge
		}
		if message := strings.TrimSpace(stderr.buf.String()); message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return stdout.buf.Bytes(), nil
}
