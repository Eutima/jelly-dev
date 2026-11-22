package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteWorktree_ReplacesFile_WhenFileIsAsExpected(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\ntwo\n")
	repo.commit("first")
	repo.write("a.txt", "one\nTWO\n")
	changes := repo.open(GitSpec{})
	file := find(t, list(t, changes), "a.txt")
	if !changes.Editable(file) {
		t.Fatal("a modified file of the working tree must be editable")
	}
	if err := changes.WriteWorktree(context.Background(), file, "one\nTWO\n", "one\ntwo\n"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo.dir, "a.txt")); string(data) != "one\ntwo\n" {
		t.Fatalf("file holds %q", data)
	}
}

func TestWriteWorktree_Refuses_WhenFileChangedOnDisk(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.commit("first")
	repo.write("a.txt", "two\n")
	changes := repo.open(GitSpec{})
	file := find(t, list(t, changes), "a.txt")
	err := changes.WriteWorktree(context.Background(), file, "something else\n", "x\n")
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo.dir, "a.txt")); string(data) != "two\n" {
		t.Fatalf("file was touched: %q", data)
	}
}

func TestEditable_IsFalse_ForStagedRevisionRangeAndDeletedFiles(t *testing.T) {
	repo := newRepo(t)
	repo.write("a.txt", "one\n")
	repo.write("gone.txt", "x\n")
	repo.commit("first")
	repo.write("a.txt", "two\n")
	repo.git("add", "a.txt")
	if err := os.Remove(filepath.Join(repo.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	staged := repo.open(GitSpec{Staged: true})
	if staged.Editable(find(t, list(t, staged), "a.txt")) {
		t.Error("the index is not editable")
	}
	worktree := repo.open(GitSpec{})
	if worktree.Editable(find(t, list(t, worktree), "gone.txt")) {
		t.Error("a deleted file is not editable")
	}
	repo.commit("second")
	rng := repo.open(GitSpec{Rev: "HEAD~1..HEAD"})
	if rng.Editable(find(t, list(t, rng), "a.txt")) {
		t.Error("two revisions are not editable")
	}
}
