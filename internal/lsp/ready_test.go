package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eutima/jelly-dev/internal/jsonframe"
)

func idleClient() *Client {
	return &Client{progress: map[string]bool{}, activity: time.Now().Add(-time.Hour), done: make(chan struct{})}
}

func progressMsg(method, params string) message {
	return message{Method: method, Params: json.RawMessage(params)}
}

func TestWaitReady_WaitsForAnnouncedWork_ThenReturns(t *testing.T) {
	c := idleClient()
	c.ready = true // not the first question
	c.noteActivity(progressMsg("window/workDoneProgress/create", `{"token":"t1"}`))
	c.noteActivity(progressMsg("$/progress", `{"token":"t1","value":{"kind":"begin","title":"Indexing"}}`))
	c.mu.Lock()
	c.activity = time.Now().Add(-time.Hour) // announced work counts even when the server is silent
	c.mu.Unlock()
	go func() {
		time.Sleep(150 * time.Millisecond)
		c.noteActivity(progressMsg("$/progress", `{"token":"t1","value":{"kind":"end"}}`))
	}()
	start := time.Now()
	c.waitReady(context.Background())
	if waited := time.Since(start); waited < 100*time.Millisecond || waited > 2*time.Second {
		t.Fatalf("waited %v: the question must wait for the end of the work, and no longer", waited)
	}
}

func TestWaitReady_FirstQuestionWaitsUntilServerIsQuiet(t *testing.T) {
	c := idleClient()
	c.mu.Lock()
	c.activity = time.Now() // the server has just spoken
	c.mu.Unlock()
	start := time.Now()
	c.waitReady(context.Background())
	if waited := time.Since(start); waited < firstQuiet-50*time.Millisecond {
		t.Fatalf("the first question did not wait for the server to settle (%v)", waited)
	}
	start = time.Now()
	c.mu.Lock()
	c.activity = time.Now()
	c.mu.Unlock()
	c.waitReady(context.Background())
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("later questions must not wait for a quiet server")
	}
}

func TestWaitReady_IgnoresProgressWithoutToken_AndStopsWhenCancelled(t *testing.T) {
	c := idleClient()
	c.noteActivity(progressMsg("window/workDoneProgress/create", `{}`))
	if len(c.progress) != 0 {
		t.Fatal("a progress message without a token cannot be waited for")
	}
	c.progress["stuck"] = true
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	c.waitReady(ctx)
	if time.Since(start) > time.Second {
		t.Fatal("a cancelled question must stop waiting")
	}
}

func TestManager_OpensProjectFilesOfTypeScriptWithoutTsconfig(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"src/a.ts": "export const a = 1\n", "src/b.js": "export const b = 2\n", "node_modules/x/i.ts": "x\n", ".hidden/h.ts": "h\n", "readme.md": "x\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewManager(root, DefaultServers())
	c := idleClient()
	c.docs = map[string]document{}
	c.tr = jsonframe.New(os.Stdin, discard{})
	spec, _, _ := specFor(manager.servers, "a.ts")
	manager.preload(c, spec, root)
	if len(c.docs) != 2 {
		t.Fatalf("opened %d files, want the two sources outside node_modules and hidden folders: %v", len(c.docs), c.docs)
	}
	if hasProjectFile(root) {
		t.Fatal("no tsconfig.json here")
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasProjectFile(root) {
		t.Fatal("tsconfig.json must count as a project file")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestRootFor_PythonUsesOutermostMarker_WhenEveryAppHasARequirementsFile(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"requirements.txt", "app/requirements.txt", "app/models/requirements.txt"} {
		path := filepath.Join(project, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(project, "app", "models", "models.py")
	python, _, _ := specFor(DefaultServers(), file)
	if got := rootFor(python, project, file); got != project {
		t.Fatalf("pyright started in %s: imports and references of other apps would be invisible to it", got)
	}
	nearest := python
	nearest.OutermostRoot = false
	if got := rootFor(nearest, project, file); got != filepath.Join(project, "app", "models") {
		t.Fatalf("without OutermostRoot the nearest marker wins, got %s", got)
	}
}
