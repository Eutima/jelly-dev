package source

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a throwaway git repository.
type testRepo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *testRepo {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := &testRepo{t: t, dir: dir}
	repo.git("init", "-q", "-b", "main")
	return repo
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *testRepo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) commit(message string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
}

func (r *testRepo) open(spec GitSpec) *GitChanges {
	r.t.Helper()
	changes, err := OpenGit(r.dir, spec)
	if err != nil {
		r.t.Fatalf("OpenGit: %v", err)
	}
	return changes
}

func list(t *testing.T, changes *GitChanges) []ChangedFile {
	t.Helper()
	files, err := changes.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return files
}

func find(t *testing.T, files []ChangedFile, path string) ChangedFile {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("%s not in %+v", path, files)
	return ChangedFile{}
}

func TestList_ReportsModifiedDeletedAndUntracked_WhenWorkingTreeChanged(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.write("b.txt", "two\n")
	repo.commit("init")
	repo.write("a.txt", "ONE\n")
	if err := os.Remove(filepath.Join(repo.dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	repo.write("new.txt", "new\n")

	files := list(t, repo.open(GitSpec{}))

	if find(t, files, "a.txt").Status != StatusModified || find(t, files, "b.txt").Status != StatusDeleted ||
		find(t, files, "new.txt").Status != StatusUntracked {
		t.Fatalf("unexpected files: %+v", files)
	}
}

func TestLoad_ReturnsOldAndNewText_WhenFileModified(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.commit("init")
	repo.write("a.txt", "ONE\n")
	changes := repo.open(GitSpec{})

	oldText, newText, err := changes.Load(find(t, list(t, changes), "a.txt"))

	if err != nil || oldText != "one\n" || newText != "ONE\n" {
		t.Fatalf("old %q new %q err %v", oldText, newText, err)
	}
}

func TestLoad_ReturnsEmptySide_WhenFileAddedDeletedOrUntracked(t *testing.T) {
	repo := newRepo(t)
	repo.write("gone.txt", "bye\n")
	repo.commit("init")
	if err := os.Remove(filepath.Join(repo.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	repo.write("fresh.txt", "hi\n")
	changes := repo.open(GitSpec{})
	files := list(t, changes)

	oldText, newText, err := changes.Load(find(t, files, "gone.txt"))
	if err != nil || oldText != "bye\n" || newText != "" {
		t.Fatalf("deleted: %q %q %v", oldText, newText, err)
	}
	oldText, newText, err = changes.Load(find(t, files, "fresh.txt"))
	if err != nil || oldText != "" || newText != "hi\n" {
		t.Fatalf("untracked: %q %q %v", oldText, newText, err)
	}
}

func TestList_ReportsRenameWithOldPath_WhenFileMoved(t *testing.T) {
	repo := newRepo(t)
	repo.write("old name.txt", "line1\nline2\nline3\nline4\n")
	repo.commit("init")
	repo.git("mv", "old name.txt", "new name.txt")
	changes := repo.open(GitSpec{})

	files := list(t, changes)
	renamed := find(t, files, "new name.txt")
	oldText, newText, err := changes.Load(renamed)

	if renamed.Status != StatusRenamed || renamed.OldPath != "old name.txt" {
		t.Fatalf("unexpected: %+v", renamed)
	}
	if err != nil || oldText != newText {
		t.Fatalf("load: %q %q %v", oldText, newText, err)
	}
}

func TestList_SeparatesStagedFromWorkingTree_WhenStagedFlagGiven(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.write("b.txt", "one\n")
	repo.commit("init")
	repo.write("a.txt", "staged\n")
	repo.git("add", "a.txt")
	repo.write("a.txt", "staged and edited\n")
	repo.write("b.txt", "unstaged\n")
	changes := repo.open(GitSpec{Staged: true})

	files := list(t, changes)
	oldText, newText, err := changes.Load(find(t, files, "a.txt"))

	if len(files) != 1 || err != nil || oldText != "one\n" || newText != "staged\n" {
		t.Fatalf("files %+v old %q new %q err %v", files, oldText, newText, err)
	}
}

func TestList_ComparesWorkingTreeToRevision_WhenRevGiven(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "v1\n")
	repo.commit("first")
	repo.write("a.txt", "v2\n")
	repo.commit("second")
	repo.write("a.txt", "v3\n")
	changes := repo.open(GitSpec{Rev: "HEAD~1"})

	oldText, newText, err := changes.Load(find(t, list(t, changes), "a.txt"))

	if err != nil || oldText != "v1\n" || newText != "v3\n" {
		t.Fatalf("old %q new %q err %v", oldText, newText, err)
	}
}

func TestList_ComparesTwoRevisions_WhenRangeGiven(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "v1\n")
	repo.commit("first")
	repo.write("a.txt", "v2\n")
	repo.commit("second")
	repo.write("a.txt", "dirty\n")
	changes := repo.open(GitSpec{Rev: "HEAD~1..HEAD"})

	oldText, newText, err := changes.Load(find(t, list(t, changes), "a.txt"))
	oldLabel, newLabel := changes.Labels()

	if err != nil || oldText != "v1\n" || newText != "v2\n" || oldLabel != "HEAD~1" || newLabel != "HEAD" {
		t.Fatalf("old %q new %q err %v labels %q %q", oldText, newText, err, oldLabel, newLabel)
	}
}

func TestList_WorksWithoutCommits_WhenRepositoryIsNew(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "hello\n")
	repo.git("add", "a.txt")
	repo.write("b.txt", "untracked\n")
	changes := repo.open(GitSpec{})

	files := list(t, changes)

	if find(t, files, "a.txt").Status != StatusAdded || find(t, files, "b.txt").Status != StatusUntracked {
		t.Fatalf("unexpected: %+v", files)
	}
	if oldLabel, _ := changes.Labels(); oldLabel != emptyLabel {
		t.Fatalf("label %q", oldLabel)
	}
}

func TestList_LimitsToPaths_WhenPathspecGiven(t *testing.T) {
	repo := newRepo(t)
	repo.write("src/a.txt", "1\n")
	repo.write("docs/b.txt", "1\n")
	repo.commit("init")
	repo.write("src/a.txt", "2\n")
	repo.write("docs/b.txt", "2\n")
	repo.write("docs/new.txt", "x\n")

	files := list(t, repo.open(GitSpec{Paths: []string{"docs"}}))

	if len(files) != 2 || files[0].Path != "docs/b.txt" || files[1].Path != "docs/new.txt" {
		t.Fatalf("unexpected: %+v", files)
	}
}

func TestLoad_ReturnsBinaryError_WhenBlobContainsNul(t *testing.T) {
	repo := newRepo(t)
	repo.write("bin.dat", "a\x00b")
	repo.commit("init")
	repo.write("bin.dat", "a\x00c")
	changes := repo.open(GitSpec{})

	_, _, err := changes.Load(find(t, list(t, changes), "bin.dat"))

	if !errors.Is(err, ErrBinary) {
		t.Fatalf("err %v", err)
	}
}

func TestOpenGit_RejectsRevision_WhenUnknownOrOptionLike(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "1\n")
	repo.commit("init")
	for _, rev := range []string{"nope", "--output=/tmp/x", "-p", "a...b", "HEAD..nope", "-x..HEAD", "HEAD..-x"} {
		if _, err := OpenGit(repo.dir, GitSpec{Rev: rev}); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("rev %q: err %v", rev, err)
		}
	}
	if _, err := OpenGit(repo.dir, GitSpec{Staged: true, Rev: "HEAD..HEAD"}); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("staged range: err %v", err)
	}
}

func TestOpenGit_Fails_WhenNotInRepository(t *testing.T) {
	if _, err := OpenGit(t.TempDir(), GitSpec{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseNameStatus_ReturnsError_WhenOutputTruncated(t *testing.T) {
	if _, err := parseNameStatus([]byte("R100\x00only-old\x00")); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadTextFile_Rejects_WhenNotRegularOrTooLarge(t *testing.T) {
	if _, err := ReadTextFile(t.TempDir()); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("dir: %v", err)
	}
	if _, err := ReadTextFile("/dev/zero"); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("device: %v", err)
	}
	if _, err := checkText("big", make([]byte, MaxTextBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
}

// gitMayFail runs git and returns whether it succeeded (used for commands that are expected to fail).
func (r *testRepo) gitMayFail(args ...string) bool {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	return cmd.Run() == nil
}

func TestLoad_ReturnsUnmergedError_WhenMergeConflicts(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "base\n")
	repo.commit("base")
	repo.git("checkout", "-q", "-b", "other")
	repo.write("a.txt", "other\n")
	repo.commit("other")
	repo.git("checkout", "-q", "main")
	repo.write("a.txt", "main\n")
	repo.commit("main")
	if repo.gitMayFail("merge", "other") {
		t.Fatal("expected a merge conflict")
	}
	changes := repo.open(GitSpec{})

	file := find(t, list(t, changes), "a.txt")
	_, _, err := changes.Load(file)

	if file.Status != StatusUnmerged || !errors.Is(err, ErrUnmerged) {
		t.Fatalf("status %c err %v", file.Status, err)
	}
}

func TestLoad_ReturnsSubmoduleError_WhenSubmoduleCommitChanged(t *testing.T) {
	sub := newRepo(t)
	sub.write("s.txt", "1\n")
	sub.commit("sub init")
	repo := newRepo(t)
	repo.write("a.txt", "1\n")
	repo.commit("init")
	repo.git("-c", "protocol.file.allow=always", "submodule", "add", "-q", sub.dir, "vendor")
	repo.commit("add submodule")
	sub2 := &testRepo{t: t, dir: filepath.Join(repo.dir, "vendor")}
	sub2.write("s.txt", "2\n")
	sub2.commit("sub change")
	changes := repo.open(GitSpec{})

	file := find(t, list(t, changes), "vendor")
	_, _, err := changes.Load(file)

	if !errors.Is(err, ErrSubmodule) {
		t.Fatalf("err %v", err)
	}
}

func TestRunGit_ReturnsTooLargeError_WhenOutputExceedsLimit(t *testing.T) {
	repo := newRepo(t)
	if _, err := runGit(repo.dir, 3, "rev-parse", "--show-toplevel"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err %v", err)
	}
}

func TestLoad_ShowsLinkTarget_WhenFileIsSymlink(t *testing.T) {
	repo := newRepo(t)
	repo.write("real.txt", "content\n")
	if err := os.Symlink("real.txt", filepath.Join(repo.dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	repo.commit("init")
	if err := os.Remove(filepath.Join(repo.dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(repo.dir, "link")); err != nil {
		t.Fatal(err)
	}
	changes := repo.open(GitSpec{})

	oldText, newText, err := changes.Load(find(t, list(t, changes), "link"))

	if err != nil || oldText != "real.txt" || newText != "/etc/hostname" {
		t.Fatalf("old %q new %q err %v", oldText, newText, err)
	}
}

func TestLoad_RejectsPath_WhenParentDirectoryIsSymlinkOutsideRepository(t *testing.T) {
	repo := newRepo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo.dir, "dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	changes := repo.open(GitSpec{})

	_, _, err := changes.Load(ChangedFile{Status: StatusUntracked, Path: "dir/secret.txt"})

	if !errors.Is(err, ErrOutsideRepository) {
		t.Fatalf("err %v", err)
	}
}
