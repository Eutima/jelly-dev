package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/ui"
)

// ideEnv runs with a private project list and records the models that were shown and the windows that were opened.
type ideEnv struct {
	env     environment
	config  string
	models  []tea.Model
	windows []string
}

func newIDEEnv(t *testing.T) *ideEnv {
	t.Helper()
	config := t.TempDir()
	e := &ideEnv{config: config}
	e.env = testEnv(func(model tea.Model) error { e.models = append(e.models, model); return nil })
	e.env.getenv = func(key string) string {
		switch key {
		case project.EnvConfigDir:
			return config
		case "JD_EDITOR":
			return "vim" // the tests of the window type vim keys
		}
		return ""
	}
	e.env.newWindow = func(dir string) error { e.windows = append(e.windows, dir); return nil }
	return e
}

func (e *ideEnv) run(args ...string) (int, string) {
	var stderr bytes.Buffer
	code := run(args, &bytes.Buffer{}, &stderr, e.env)
	return code, stderr.String()
}

func (e *ideEnv) recent(t *testing.T) []project.Entry {
	t.Helper()
	store, err := project.Open(e.config)
	if err != nil {
		t.Fatal(err)
	}
	return store.Entries
}

func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestIDE_ShowsWelcomeScreen_WhenNoProjectYet(t *testing.T) {
	e := newIDEEnv(t)
	if code, msg := e.run(); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	if _, ok := e.models[0].(ui.WelcomeModel); !ok || len(e.models) != 1 {
		t.Fatalf("models %T", e.models)
	}
}

func TestIDE_OpensDirectoryAsProjectAndRemembersIt(t *testing.T) {
	e := newIDEEnv(t)
	dir := realDir(t)
	if code, msg := e.run(dir); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	model, ok := e.models[0].(ui.ProjectModel)
	if !ok || model.Root() != dir {
		t.Fatalf("model %T", e.models[0])
	}
	if entries := e.recent(t); len(entries) != 1 || entries[0].Path != dir {
		t.Fatalf("recent %+v", entries)
	}
}

func TestIDE_ReopensMostRecentProject_WhenNoArgument(t *testing.T) {
	e := newIDEEnv(t)
	first, second := realDir(t), realDir(t)
	e.run(first)
	e.run(second)
	e.models = nil
	if code, msg := e.run(); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	if model, ok := e.models[0].(ui.ProjectModel); !ok || model.Root() != second {
		t.Fatalf("the most recent project must open: %T", e.models[0])
	}
	e.run(first)
	e.models = nil
	e.run()
	if model := e.models[0].(ui.ProjectModel); model.Root() != first {
		t.Fatalf("opening a project makes it the most recent one: %s", model.Root())
	}
}

func TestIDE_SkipsRecentProjectThatWasDeleted(t *testing.T) {
	e := newIDEEnv(t)
	gone, kept := realDir(t), realDir(t)
	e.run(kept)
	e.run(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	e.models = nil
	e.run()
	if model, ok := e.models[0].(ui.ProjectModel); !ok || model.Root() != kept {
		t.Fatalf("model %T", e.models[0])
	}
}

func TestIDE_OpensFileInItsGitRoot(t *testing.T) {
	e := newIDEEnv(t)
	repo := realDir(t)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "src", "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, msg := e.run(file); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	model := e.models[0].(ui.ProjectModel)
	if model.Root() != repo || model.Editor().Path() != file {
		t.Fatalf("root %q file %q", model.Root(), model.Editor().Path())
	}
}

func TestIDE_StartsNewFile_WhenPathDoesNotExistYet(t *testing.T) {
	e := newIDEEnv(t)
	dir := realDir(t)
	file := filepath.Join(dir, "new.txt")
	if code, msg := e.run(file); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	if model := e.models[0].(ui.ProjectModel); model.Editor().Path() != file {
		t.Fatalf("file %q", model.Editor().Path())
	}
	if code, _ := e.run(filepath.Join(dir, "missing", "new.txt")); code != exitFailure {
		t.Fatalf("a missing folder must fail, code %d", code)
	}
}

func TestIDE_FailsForBrokenProjectList(t *testing.T) {
	e := newIDEEnv(t)
	if err := os.WriteFile(filepath.Join(e.config, "projects.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, msg := e.run(realDir(t))
	if code != exitFailure || !strings.Contains(msg, "projects.json") {
		t.Fatalf("code %d: %s", code, msg)
	}
}

func TestIDE_StoresSessionWhenWindowIsClosed(t *testing.T) {
	e := newIDEEnv(t)
	dir := realDir(t)
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.env.runUI = func(model tea.Model) error {
		// drive the window like a user: size, move to line 3, quit
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
		for _, key := range []string{"j", "j", ":", "q", "enter"} {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
			if key == "enter" {
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			}
			model, _ = model.Update(msg)
		}
		return nil
	}
	if code, msg := e.run(file); code != exitOK {
		t.Fatalf("code %d: %s", code, msg)
	}
	store, _ := project.Open(e.config)
	session := store.Session(dir)
	if session.Active != "a.txt" || session.Cursors["a.txt"].Row != 2 {
		t.Fatalf("session %+v", session)
	}
	e.models = nil
	e.env.runUI = func(model tea.Model) error { e.models = append(e.models, model); return nil }
	e.run()
	if row, _ := e.models[0].(ui.ProjectModel).Editor().Cursor(); row != 2 {
		t.Fatalf("the cursor must be restored, row %d", row)
	}
}

func TestRun_PrintsUsage_WhenHelpRequested(t *testing.T) {
	for _, flag := range []string{"-h", "--help", "help"} {
		var stdout bytes.Buffer
		if code := run([]string{flag}, &stdout, &bytes.Buffer{}, testEnv(noUI)); code != exitOK || !strings.Contains(stdout.String(), "usage") {
			t.Fatalf("%s: code %d %q", flag, code, stdout.String())
		}
	}
}
