package project

import (
	"path/filepath"
	"testing"
)

func TestNotePath_BuildsFileInNotesFolder_WhenNameIsPlain(t *testing.T) {
	for name, want := range map[string]string{"": "notes.md", "ideas": "ideas.md", "todo.md": "todo.md", " plan ": "plan.md"} {
		got, err := NotePath("/p", name)
		if err != nil || got != filepath.Join("/p", ".jd", "notes", want) {
			t.Errorf("NotePath(%q) = %q, %v", name, got, err)
		}
	}
}

func TestNotePath_Refuses_WhenNameLeavesTheFolder(t *testing.T) {
	for _, name := range []string{"..", ".", "../x", "a/b", `a\b`} {
		if got, err := NotePath("/p", name); err == nil {
			t.Errorf("NotePath(%q) = %q, want an error", name, got)
		}
	}
}

func TestNoteRoot_ReturnsWhatWasStored_AndEmptyWhenNothingIs(t *testing.T) {
	dir := t.TempDir()
	if root, err := NoteRoot(dir); err != nil || root != "" {
		t.Fatalf("empty config: %q, %v", root, err)
	}
	project := t.TempDir()
	if err := SetNoteRoot(dir, project); err != nil {
		t.Fatal(err)
	}
	root, err := NoteRoot(dir)
	if err != nil || root != clean(project) {
		t.Fatalf("NoteRoot = %q, %v", root, err)
	}
}

func TestNoteOwner_ReturnsTheProject_ForNoteFilesOnly(t *testing.T) {
	note, _ := NotePath("/work/shop", "ideas")
	if root, ok := NoteOwner(note); !ok || root != filepath.FromSlash("/work/shop") {
		t.Fatalf("NoteOwner(%q) = %q, %v", note, root, ok)
	}
	for _, path := range []string{"/work/shop/main.go", "/work/shop/notes/a.md", "/work/shop/.jd/other/a.md", ""} {
		if root, ok := NoteOwner(filepath.FromSlash(path)); ok {
			t.Errorf("NoteOwner(%q) = %q, want none", path, root)
		}
	}
}
