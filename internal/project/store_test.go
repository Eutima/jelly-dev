package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func dirs(t *testing.T, names ...string) []string {
	t.Helper()
	base := t.TempDir()
	var out []string
	for _, name := range names {
		path := filepath.Join(base, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		resolved, _ := filepath.EvalSymlinks(path)
		out = append(out, resolved)
	}
	return out
}

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	return store
}

func TestStore_KeepsNewestFirstAndMovesReopenedProjectToFront(t *testing.T) {
	paths := dirs(t, "a", "b", "c")
	store := newStore(t)
	for _, path := range paths {
		store.Touch(path)
	}
	store.Touch(paths[0])
	got := store.Recent()
	if len(got) != 3 || got[0].Path != paths[0] || got[1].Path != paths[2] || got[2].Path != paths[1] {
		t.Fatalf("order %+v", got)
	}
	if latest, ok := store.Latest(); !ok || latest.Path != paths[0] {
		t.Fatalf("latest %+v", latest)
	}
}

func TestStore_SkipsProjectsWhoseDirectoryIsGone(t *testing.T) {
	paths := dirs(t, "a", "b")
	store := newStore(t)
	store.Touch(paths[0])
	store.Touch(paths[1])
	if err := os.RemoveAll(paths[1]); err != nil {
		t.Fatal(err)
	}
	if latest, ok := store.Latest(); !ok || latest.Path != paths[0] {
		t.Fatalf("latest %+v", latest)
	}
	empty := newStore(t)
	if _, ok := empty.Latest(); ok {
		t.Fatal("no projects, no latest")
	}
}

func TestStore_SavesAndLoadsSessions(t *testing.T) {
	path := dirs(t, "p")[0]
	dir := t.TempDir()
	store, _ := Open(dir)
	store.Touch(path)
	store.SetSession(path, Session{Files: []string{"a.go"}, Active: "a.go", Cursors: map[string]Cursor{"a.go": {Row: 3, Col: 4}}})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	session := loaded.Session(path)
	if session.Active != "a.go" || session.Cursors["a.go"] != (Cursor{3, 4}) || len(loaded.Entries) != 1 {
		t.Fatalf("loaded %+v", loaded.Entries)
	}
}

func TestStore_CapsListLength(t *testing.T) {
	store := newStore(t)
	base := t.TempDir()
	for i := 0; i < maxProjects+5; i++ {
		dir := filepath.Join(base, string(rune('a'+i)))
		_ = os.MkdirAll(dir, 0o755)
		store.Touch(dir)
	}
	if len(store.Entries) != maxProjects {
		t.Fatalf("%d entries", len(store.Entries))
	}
}

func TestOpen_FailsForBrokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("a broken list must be an error, not silently dropped")
	}
}

func TestRoot_FindsGitRootForFilesAndKeepsDirectories(t *testing.T) {
	base := dirs(t, "repo/sub/deep", "plain")[0]
	repo := filepath.Dir(filepath.Dir(base))
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "x.go")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if root, err := Root(file); err != nil || root != repo {
		t.Fatalf("root %q, %v, want %q", root, err, repo)
	}
	if root, err := Root(base); err != nil || root != base {
		t.Fatalf("a directory is its own root: %q, %v", root, err)
	}
	plain := filepath.Join(filepath.Dir(repo), "plain", "f.txt")
	_ = os.WriteFile(plain, []byte("x"), 0o644)
	if root, err := Root(plain); err != nil || root != filepath.Dir(plain) {
		t.Fatalf("without git the file's directory: %q, %v", root, err)
	}
}
