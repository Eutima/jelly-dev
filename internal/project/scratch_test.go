package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidScratchName_AddsExtensionAndRefusesFolders(t *testing.T) {
	for name, want := range map[string]string{"a.py": "a.py", " notes ": "notes.txt", "x.": "x.txt", ".env": ".env"} {
		if got, err := ValidScratchName(name); err != nil || got != want {
			t.Errorf("ValidScratchName(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"", " ", ".", "..", "../x", "a/b", `a\b`} {
		if got, err := ValidScratchName(name); err == nil {
			t.Errorf("ValidScratchName(%q) = %q, want an error", name, got)
		}
	}
}

func TestListScratches_ReturnsNewestFirst_AndNothingWithoutFolder(t *testing.T) {
	dir := t.TempDir()
	if list, err := ListScratches(dir); err != nil || len(list) != 0 {
		t.Fatalf("no folder: %v, %v", list, err)
	}
	scratches := ScratchDir(dir)
	if err := os.MkdirAll(filepath.Join(scratches, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for name, age := range map[string]time.Duration{"old.py": 2 * time.Hour, "new.md": 0} {
		path := filepath.Join(scratches, name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ListScratches(dir)
	if err != nil || len(list) != 2 || list[0].Name != "new.md" || list[1].Name != "old.py" {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestNewScratchName_UsesTimestamp(t *testing.T) {
	got := NewScratchName(time.Date(2026, 11, 24, 9, 5, 7, 0, time.UTC))
	if got != "scratch-20261124-090507.txt" {
		t.Fatalf("name = %q", got)
	}
}
