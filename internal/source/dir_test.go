package source

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func listDirs(t *testing.T, left, right map[string]string) (*DirChanges, []ChangedFile) {
	t.Helper()
	leftDir, rightDir := t.TempDir(), t.TempDir()
	writeTree(t, leftDir, left)
	writeTree(t, rightDir, right)
	changes, err := OpenDirs(leftDir, rightDir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := changes.List()
	if err != nil {
		t.Fatal(err)
	}
	return changes, files
}

func statuses(files []ChangedFile) string {
	var parts []string
	for _, file := range files {
		parts = append(parts, string(rune(file.Status))+" "+file.Path)
	}
	return strings.Join(parts, ", ")
}

func TestDirList_ReportsModifiedAddedAndDeleted_WhenTreesDiffer(t *testing.T) {
	_, files := listDirs(t,
		map[string]string{"same.txt": "x", "changed.txt": "old", "gone.txt": "bye", "sub/deep.txt": "1"},
		map[string]string{"same.txt": "x", "changed.txt": "new", "fresh.txt": "hi", "sub/deep.txt": "2"})
	want := "M changed.txt, A fresh.txt, D gone.txt, M sub/deep.txt"
	if got := statuses(files); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDirList_DetectsChange_WhenSizeEqualButContentDiffers(t *testing.T) {
	_, files := listDirs(t, map[string]string{"a.txt": "abcd"}, map[string]string{"a.txt": "abce"})
	if statuses(files) != "M a.txt" {
		t.Fatalf("got %q", statuses(files))
	}
}

func TestDirList_DetectsChange_WhenFilesLargerThanCompareBuffer(t *testing.T) {
	big := strings.Repeat("x", compareBufSize*2+10)
	changedAtEnd := big[:len(big)-1] + "y"
	_, files := listDirs(t,
		map[string]string{"same.txt": big, "diff.txt": big},
		map[string]string{"same.txt": big, "diff.txt": changedAtEnd})
	if statuses(files) != "M diff.txt" {
		t.Fatalf("got %q", statuses(files))
	}
}

func TestDirList_ReturnsNothing_WhenTreesIdentical(t *testing.T) {
	_, files := listDirs(t, map[string]string{"a.txt": "1", "d/b.txt": "2"}, map[string]string{"a.txt": "1", "d/b.txt": "2"})
	if len(files) != 0 {
		t.Fatalf("got %q", statuses(files))
	}
}

func TestDirList_SkipsGitDirectories_WhenPresent(t *testing.T) {
	_, files := listDirs(t,
		map[string]string{".git/config": "left", "a.txt": "1"},
		map[string]string{".git/config": "right", "a.txt": "1"})
	if len(files) != 0 {
		t.Fatalf("got %q", statuses(files))
	}
}

func TestDirList_FollowsLinksToFiles_WhenRightSideUsesSymlinks(t *testing.T) {
	// git difftool --dir-diff fills its temporary directory with links to the working tree files.
	leftDir, rightDir, realDir := t.TempDir(), t.TempDir(), t.TempDir()
	writeTree(t, leftDir, map[string]string{"a.txt": "old\n", "same.txt": "x\n"})
	writeTree(t, realDir, map[string]string{"a.txt": "new\n", "same.txt": "x\n"})
	for _, name := range []string{"a.txt", "same.txt"} {
		if err := os.Symlink(filepath.Join(realDir, name), filepath.Join(rightDir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	changes, err := OpenDirs(leftDir, rightDir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := changes.List()
	if err != nil || statuses(files) != "M a.txt" {
		t.Fatalf("files %q err %v", statuses(files), err)
	}
	if oldText, newText, err := changes.Load(files[0]); err != nil || oldText != "old\n" || newText != "new\n" {
		t.Fatalf("load %q %q %v", oldText, newText, err)
	}
}

func TestDirList_SkipsDanglingAndDirectoryLinks_WhenPresent(t *testing.T) {
	leftDir, rightDir, other := t.TempDir(), t.TempDir(), t.TempDir()
	writeTree(t, other, map[string]string{"inner.txt": "x"})
	if err := os.Symlink(filepath.Join(other, "missing"), filepath.Join(rightDir, "dangling")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(other, filepath.Join(rightDir, "dirlink")); err != nil {
		t.Fatal(err)
	}
	changes, err := OpenDirs(leftDir, rightDir)
	if err != nil {
		t.Fatal(err)
	}
	if files, err := changes.List(); err != nil || len(files) != 0 {
		t.Fatalf("files %q err %v", statuses(files), err)
	}
}

func TestDirList_ScansTargets_WhenRootsAreSymlinks(t *testing.T) {
	realLeft, realRight, links := t.TempDir(), t.TempDir(), t.TempDir()
	writeTree(t, realLeft, map[string]string{"a.txt": "1"})
	linkLeft, linkRight := filepath.Join(links, "left"), filepath.Join(links, "right")
	if err := os.Symlink(realLeft, linkLeft); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(realRight, linkRight); err != nil {
		t.Fatal(err)
	}
	changes, err := OpenDirs(linkLeft, linkRight)
	if err != nil {
		t.Fatal(err)
	}
	files, err := changes.List()
	if err != nil || statuses(files) != "D a.txt" {
		t.Fatalf("files %q err %v", statuses(files), err)
	}
	if leftLabel, _ := changes.Labels(); leftLabel != linkLeft {
		t.Fatalf("label %q", leftLabel)
	}
}

func TestDirList_TreatsFileAndDirectoryAtSamePathAsDeletedAndAdded_WhenTypesConflict(t *testing.T) {
	_, files := listDirs(t, map[string]string{"x": "file"}, map[string]string{"x/a.txt": "inside"})
	if got := statuses(files); got != "D x, A x/a.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestDirLoad_ReturnsTextsAndEmptyMissingSide_WhenFilesChanged(t *testing.T) {
	changes, files := listDirs(t,
		map[string]string{"c.txt": "old\n", "gone.txt": "bye\n"},
		map[string]string{"c.txt": "new\n", "fresh.txt": "hi\n"})
	byPath := map[string]ChangedFile{}
	for _, file := range files {
		byPath[file.Path] = file
	}
	cases := map[string][2]string{"c.txt": {"old\n", "new\n"}, "gone.txt": {"bye\n", ""}, "fresh.txt": {"", "hi\n"}}
	for path, want := range cases {
		oldText, newText, err := changes.Load(byPath[path])
		if err != nil || oldText != want[0] || newText != want[1] {
			t.Fatalf("%s: %q %q %v", path, oldText, newText, err)
		}
	}
}

func TestDirLoad_ReturnsBinaryError_WhenFileHasNulBytes(t *testing.T) {
	changes, files := listDirs(t, map[string]string{"b.dat": "a\x00b"}, map[string]string{"b.dat": "a\x00c"})
	if _, _, err := changes.Load(files[0]); !errors.Is(err, ErrBinary) {
		t.Fatalf("err %v", err)
	}
}

func TestOpenDirs_Fails_WhenPathMissingOrNotDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDirs(dir, file); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("file: %v", err)
	}
	if _, err := OpenDirs(dir, filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

func TestSameContent_ReturnsFalse_WhenOneFileIsPrefixOfOther(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a": strings.Repeat("x", compareBufSize), "b": strings.Repeat("x", compareBufSize+1)})
	same, err := sameContent(filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if err != nil || same {
		t.Fatalf("same %v err %v", same, err)
	}
}
