package filetree

import (
	"os"
	"path/filepath"
	"testing"
)

func build(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(rows []*Node) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTree_ListsDirectoriesFirstSortedAndLazy(t *testing.T) {
	root := build(t, "b.txt", "A.txt", "src/main.go", "src/util/u.go", "docs/readme.md")
	tree, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(tree.Visible()); !equal(got, []string{"docs", "src", "A.txt", "b.txt"}) {
		t.Fatalf("first level %v", got)
	}
	src := tree.Visible()[1]
	if src.Loaded {
		t.Fatal("directories must be read on demand")
	}
	tree.Expand(src)
	if got := names(tree.Visible()); !equal(got, []string{"docs", "src", "util", "main.go", "A.txt", "b.txt"}) {
		t.Fatalf("after expand %v", got)
	}
	tree.Collapse(src)
	if got := names(tree.Visible()); len(got) != 4 {
		t.Fatalf("after collapse %v", got)
	}
}

func TestTree_HidesGitAndIgnoredEntries(t *testing.T) {
	root := build(t, ".git/config", "keep.go", "build/out.o", "app.log", "sub/app.log", "sub/keep.go", ".gitignore")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# comment\n*.log\n/build/\n!keep.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, _ := New(root)
	if got := names(tree.Visible()); !equal(got, []string{"sub", ".gitignore", "keep.go"}) {
		t.Fatalf("visible %v", got)
	}
	tree.Expand(tree.Visible()[0])
	if got := names(tree.Visible()); !equal(got, []string{"sub", "keep.go", ".gitignore", "keep.go"}) {
		t.Fatalf("sub %v", got)
	}
}

func TestTree_RevealExpandsParents(t *testing.T) {
	root := build(t, "a/b/c.txt", "a/x.txt")
	tree, _ := New(root)
	node := tree.Reveal(filepath.Join(root, "a", "b", "c.txt"))
	if node == nil || node.Name != "c.txt" {
		t.Fatalf("node %v", node)
	}
	if got := names(tree.Visible()); !equal(got, []string{"a", "b", "c.txt", "x.txt"}) {
		t.Fatalf("visible %v", got)
	}
	if tree.Reveal(filepath.Join(root, "missing")) != nil || tree.Reveal("/elsewhere/file") != nil {
		t.Fatal("unknown paths must not be revealed")
	}
}

func TestTree_RefreshKeepsExpandedDirectories(t *testing.T) {
	root := build(t, "a/one.txt")
	tree, _ := New(root)
	tree.Expand(tree.Visible()[0])
	if err := os.WriteFile(filepath.Join(root, "a", "two.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree.Refresh()
	if got := names(tree.Visible()); !equal(got, []string{"a", "one.txt", "two.txt"}) {
		t.Fatalf("visible %v", got)
	}
}

func TestNew_FailsForFilesAndMissingDirectories(t *testing.T) {
	root := build(t, "f.txt")
	if _, err := New(filepath.Join(root, "f.txt")); err == nil {
		t.Fatal("a file is no project directory")
	}
	if _, err := New(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing directory is an error")
	}
}
