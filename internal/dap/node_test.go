package dap

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNodeLaunch_JavaScriptRunsWithNodeAndTypeScriptWithWhatTheProjectHas(t *testing.T) {
	root := writeFiles(t, map[string]string{"app.js": "1", "src/a.ts": "1", "b.mts": "1"})
	js := nodeLaunch(filepath.Join(root, "app.js"), root)
	if js["type"] != "pwa-node" || js["request"] != "launch" || js["program"] != filepath.Join(root, "app.js") || js["cwd"] != root || js["console"] != "internalConsole" {
		t.Fatalf("js: %v", js)
	}
	if js["runtimeArgs"] != nil || js["runtimeExecutable"] != nil {
		t.Fatalf("JavaScript needs nothing but node: %v", js)
	}
	ts := nodeLaunch(filepath.Join(root, "src", "a.ts"), root)
	if !reflect.DeepEqual(ts["runtimeArgs"], []string{"--experimental-strip-types"}) {
		t.Fatalf("without a runner Node.js strips the types: %v", ts)
	}
	withTsNode := writeFiles(t, map[string]string{"node_modules/.bin/ts-node": "", "a.ts": "1"})
	if got := nodeLaunch(filepath.Join(withTsNode, "a.ts"), withTsNode); !reflect.DeepEqual(got["runtimeArgs"], []string{"-r", "ts-node/register"}) {
		t.Fatalf("ts-node of the project: %v", got)
	}
	withTsx := writeFiles(t, map[string]string{"node_modules/.bin/tsx": "", "node_modules/.bin/ts-node": "", "b.mts": "1"})
	if got := nodeLaunch(filepath.Join(withTsx, "b.mts"), withTsx); got["runtimeExecutable"] != filepath.Join(withTsx, "node_modules", ".bin", "tsx") || got["runtimeArgs"] != nil {
		t.Fatalf("tsx wins: %v", got)
	}
}

func TestJsDebug_BelongsToScriptFilesAndListens(t *testing.T) {
	for _, file := range []string{"/p/a.js", "/p/a.MJS", "/p/a.cjs", "/p/a.ts", "/p/a.mts", "/p/a.cts"} {
		adapter, ok := AdapterFor(DefaultAdapters(), file)
		if !ok || adapter.Name != "js-debug" || !adapter.Listens || adapter.Command[0] != "js-debug-adapter" {
			t.Fatalf("%s: %+v %v", file, adapter, ok)
		}
	}
	if _, ok := AdapterFor(DefaultAdapters(), "/p/a.tsx"); ok {
		t.Fatal("JSX needs a build: not run directly")
	}
}

// TestJsDebug_DebugsARealScript runs the real js-debug against a small Node.js script; skipped without js-debug-adapter and node.
func TestJsDebug_DebugsARealScript(t *testing.T) {
	for _, command := range []string{"js-debug-adapter", "node"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skip(command + " is not installed")
		}
	}
	root := writeFiles(t, map[string]string{"main.js": "const x = 21;\nconst y = x * 2;\nconsole.log('answer', y);\n"})
	file := filepath.Join(root, "main.js")
	ctx := ctxT(t)
	session, verified, err := Debug(ctx, DefaultAdapters(), exec.LookPath, file, root, map[string][]int{file: {2}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(ctx)
	if got := verified[file]; len(got) != 1 {
		t.Fatalf("breakpoint: %+v", got)
	}
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctx)
	if err != nil || len(frames) == 0 || frames[0].Line != 2 || !strings.HasSuffix(frames[0].Path, "main.js") {
		t.Fatalf("stack: %+v %v", frames, err)
	}
	scopes, err := session.Scopes(ctx, frames[0].ID)
	if err != nil || len(scopes) == 0 {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
	seen := false
	for _, scope := range scopes {
		vars, _ := session.Variables(ctx, scope.Reference)
		for _, v := range vars {
			seen = seen || (v.Name == "x" && v.Value == "21")
		}
	}
	if !seen {
		t.Fatal("x = 21 is not in the scopes")
	}
	if v, err := session.Evaluate(ctx, frames[0].ID, "x + 1"); err != nil || v.Value != "22" {
		t.Fatalf("evaluate: %+v %v", v, err)
	}
	if err := session.Next(ctx); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, session, "stopped")
	if err := session.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, session, "terminated")
}

// TestJsDebug_DebugsARealTypeScriptFile runs a .ts file with the type stripping of Node.js (22.6 and newer); skipped where that is not there.
func TestJsDebug_DebugsARealTypeScriptFile(t *testing.T) {
	for _, command := range []string{"js-debug-adapter", "node"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skip(command + " is not installed")
		}
	}
	out, err := exec.Command("node", "--version").Output()
	var major, minor int
	if _, scanErr := fmt.Sscanf(strings.TrimSpace(string(out)), "v%d.%d", &major, &minor); err != nil || scanErr != nil || major < 22 || (major == 22 && minor < 6) {
		t.Skip("Node.js 22.6 or newer is needed to run TypeScript")
	}
	root := writeFiles(t, map[string]string{"main.ts": "const x: number = 21;\nconst y: number = x * 2;\nconsole.log('answer', y);\n"})
	file := filepath.Join(root, "main.ts")
	ctx := ctxT(t)
	session, _, err := Debug(ctx, DefaultAdapters(), exec.LookPath, file, root, map[string][]int{file: {2}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(ctx)
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctx)
	if err != nil || len(frames) == 0 || frames[0].Line != 2 || !strings.HasSuffix(frames[0].Path, "main.ts") {
		t.Fatalf("the stop is in the TypeScript file: %+v %v", frames, err)
	}
}
