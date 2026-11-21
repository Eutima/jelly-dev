package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestHelperServer is not a test: it is the fake language server that the other tests start as a subprocess.
func TestHelperServer(t *testing.T) {
	if os.Getenv("GO_FAKE_LSP") != "1" {
		t.Skip("helper process")
	}
	tr := newTransport(os.Stdin, os.Stdout)
	texts := map[string]string{}
	send := func(v any) { _ = tr.write(v) }
	for {
		body, err := tr.read()
		if err != nil {
			os.Exit(0)
		}
		var msg struct {
			ID     *json.RawMessage `json:"id"`
			Method string           `json:"method"`
			Params json.RawMessage  `json:"params"`
		}
		_ = json.Unmarshal(body, &msg)
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result}) }
		logLine := func(line string) { // what the client told the server, for the tests that look at it
			if file := os.Getenv("GO_FAKE_LOG"); file != "" {
				if f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
					_, _ = f.WriteString(line + "\n")
					f.Close()
				}
			}
		}
		switch msg.Method {
		case "":
			if msg.ID != nil {
				var response struct{ Result json.RawMessage }
				_ = json.Unmarshal(body, &response)
				logLine("response " + string(*msg.ID) + " " + string(response.Result))
			}
		case "workspace/didChangeConfiguration":
			logLine("didChangeConfiguration " + string(msg.Params))
			// like pyright: after a change the server asks for the sections it uses
			send(map[string]any{"jsonrpc": "2.0", "id": 903, "method": "workspace/configuration", "params": map[string]any{
				"items": []any{map[string]any{"section": "python"}, map[string]any{"section": "python.analysis"}, map[string]any{"section": "python.pythonPath"}, 7},
			}})
		case "initialize":
			// servers ask the client things while starting up; the client must answer them
			send(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "workspace/configuration", "params": map[string]any{"items": []any{1, 2}}})
			send(map[string]any{"jsonrpc": "2.0", "id": 901, "method": "window/workDoneProgress/create", "params": map[string]any{}})
			reply(map[string]any{"capabilities": map[string]any{}})
		case "textDocument/didOpen":
			var p struct{ TextDocument struct{ URI, Text string } }
			_ = json.Unmarshal(msg.Params, &p)
			texts[p.TextDocument.URI] = p.TextDocument.Text
		case "textDocument/didChange":
			var p struct {
				TextDocument   struct{ URI string }
				ContentChanges []struct{ Text string }
			}
			_ = json.Unmarshal(msg.Params, &p)
			texts[p.TextDocument.URI] = p.ContentChanges[0].Text
		case "textDocument/definition", "textDocument/references":
			var p struct {
				TextDocument struct{ URI string }
				Position     Position
			}
			_ = json.Unmarshal(msg.Params, &p)
			target := os.Getenv("GO_FAKE_TARGET")
			uri := PathToURI(target)
			loc := func(line, char int) map[string]any {
				return map[string]any{"uri": uri, "range": Range{Start: Position{line, char}, End: Position{line, char + 1}}}
			}
			if msg.Method == "textDocument/references" {
				reply([]any{loc(0, 0), loc(1, 2), loc(2, 4)})
				continue
			}
			switch p.Position.Line {
			case 0:
				reply(loc(2, 3)) // a single Location
			case 1:
				reply([]any{loc(0, 0), loc(1, 1)}) // several
			case 2:
				reply([]any{map[string]any{"targetUri": uri, "targetRange": Range{}, "targetSelectionRange": Range{Start: Position{1, 2}}}}) // LocationLink
			case 3:
				reply(nil)
			case 4:
				send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32603, "message": "boom"}})
			default:
				reply(loc(len(strings.Split(texts[p.TextDocument.URI], "\n")), 0)) // proves which text the server has
			}
		case "textDocument/completion":
			shape := os.Getenv("GO_FAKE_COMPLETION")
			items := []any{
				map[string]any{"label": "println", "kind": 3, "detail": "func(a ...any)"},
				map[string]any{"label": "print", "kind": 3, "insertText": "print()", "sortText": "b"},
				map[string]any{"label": "snip", "insertText": "snip(${1:arg}, ${2|a,b|})$0", "insertTextFormat": 2},
				map[string]any{"label": "edited", "textEdit": map[string]any{"range": Range{Start: Position{0, 1}, End: Position{0, 3}}, "newText": "EDITED"}},
				map[string]any{"label": "replace", "textEdit": map[string]any{"insert": Range{Start: Position{0, 0}, End: Position{0, 1}}, "replace": Range{Start: Position{0, 0}, End: Position{0, 4}}, "newText": "replace"}},
				map[string]any{"label": ""},
			}
			switch shape {
			case "list":
				reply(map[string]any{"isIncomplete": false, "items": items})
			case "null":
				reply(nil)
			default:
				reply(items)
			}
		case "textDocument/formatting":
			if os.Getenv("GO_FAKE_NOFORMAT") == "1" {
				send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
				continue
			}
			var p struct{ TextDocument struct{ URI string } }
			_ = json.Unmarshal(msg.Params, &p)
			lines := strings.Split(texts[p.TextDocument.URI], "\n")
			// the fake formatter turns the first line to upper case
			reply([]any{map[string]any{"range": Range{Start: Position{0, 0}, End: Position{0, len([]rune(lines[0]))}}, "newText": strings.ToUpper(lines[0])}})
		case "shutdown":
			reply(nil)
		case "exit":
			os.Exit(0)
		}
	}
}

func fakeCommand(t *testing.T, target string) []string {
	t.Helper()
	t.Setenv("GO_FAKE_LSP", "1")
	t.Setenv("GO_FAKE_TARGET", target)
	return []string{os.Args[0], "-test.run=^TestHelperServer$"}
}

func newFile(t *testing.T, name, text string) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClient_AnswersDefinitionInAllShapes(t *testing.T) {
	target := newFile(t, "target.go", "package x\nfunc a() {}\nvar b = 1\n")
	client, err := Start(context.Background(), "fake", fakeCommand(t, target), filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	uri := PathToURI(target)
	tests := []struct {
		line int
		want []Position
	}{
		{0, []Position{{2, 3}}},
		{1, []Position{{0, 0}, {1, 1}}},
		{2, []Position{{1, 2}}},
		{3, nil},
	}
	for _, tt := range tests {
		locs, err := client.Definition(context.Background(), target, Position{Line: tt.line})
		if err != nil {
			t.Fatalf("line %d: %v", tt.line, err)
		}
		var got []Position
		for _, loc := range locs {
			if loc.URI != uri {
				t.Errorf("line %d: uri %q", tt.line, loc.URI)
			}
			got = append(got, loc.Range.Start)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("line %d: got %v, want %v", tt.line, got, tt.want)
		}
	}
	if _, err := client.Definition(context.Background(), target, Position{Line: 4}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("server errors must come back: %v", err)
	}
	refs, err := client.References(context.Background(), target, Position{})
	if err != nil || len(refs) != 3 {
		t.Fatalf("references %v, %v", refs, err)
	}
}

func TestClient_SendsOpenAndChangedText(t *testing.T) {
	target := newFile(t, "a.go", "x\n")
	client, err := Start(context.Background(), "fake", fakeCommand(t, target), filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Sync(target, "go", "one\ntwo\nthree\nfour\nfive\nsix"); err != nil {
		t.Fatal(err)
	}
	locs, err := client.Definition(context.Background(), target, Position{Line: 5})
	if err != nil || len(locs) != 1 || locs[0].Range.Start.Line != 6 {
		t.Fatalf("the server must have the opened text: %v, %v", locs, err)
	}
	if err := client.Sync(target, "go", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight"); err != nil {
		t.Fatal(err)
	}
	locs, _ = client.Definition(context.Background(), target, Position{Line: 5})
	if len(locs) != 1 || locs[0].Range.Start.Line != 8 {
		t.Fatalf("the server must have the changed text: %v", locs)
	}
	if err := client.CloseDocument(target); err != nil {
		t.Fatal(err)
	}
}

func TestClient_FailsAfterServerStopped(t *testing.T) {
	target := newFile(t, "a.go", "x\n")
	client, err := Start(context.Background(), "fake", fakeCommand(t, target), filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if _, err := client.Definition(context.Background(), target, Position{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestStart_FailsForMissingCommand(t *testing.T) {
	if _, err := Start(context.Background(), "x", []string{"/definitely/not/here"}, t.TempDir()); err == nil {
		t.Fatal("a missing server must be an error")
	}
	if _, err := Start(context.Background(), "x", nil, t.TempDir()); err == nil {
		t.Fatal("an empty command must be an error")
	}
}

func TestClient_TimesOutWhenServerDoesNotAnswer(t *testing.T) {
	client := newClient("silent", nil, strings.NewReader(""), io.Discard)
	<-client.done // the reader ended at once: the client counts as stopped
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Definition(ctx, "/x.go", Position{}); err == nil {
		t.Fatal("an error is expected")
	}
}

func TestManager_FindsServerPerFileAndPlaces(t *testing.T) {
	target := newFile(t, "main.go", "package main\nfunc é() {}\nvar b = 1\n")
	dir := filepath.Dir(target)
	spec := ServerSpec{Name: "fake", Command: fakeCommand(t, target), Extensions: []string{".go"}, LanguageID: "go"}
	manager := NewManager(dir, []ServerSpec{spec})
	manager.lookPath = func(string) (string, error) { return "/bin/fake", nil }
	defer manager.Close()
	got, err := manager.Definition(context.Background(), target, "package main\nfunc é() {}\nvar b = 1\n", 0, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v, %v", got, err)
	}
	want := Place{Path: target, Row: 2, Col: 3, Text: "var b = 1"}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
	refs, err := manager.References(context.Background(), target, "package main\nfunc é() {}\nvar b = 1\n", 1, 5)
	if err != nil || len(refs) != 3 || refs[1].Text != "func é() {}" {
		t.Fatalf("%v, %v", refs, err)
	}
	if len(manager.clients) != 1 {
		t.Fatalf("one server must serve both requests, have %d", len(manager.clients))
	}
}

func TestManager_ReportsMissingServerAndUnknownFileType(t *testing.T) {
	manager := NewManager(t.TempDir(), DefaultServers())
	manager.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	_, err := manager.Definition(context.Background(), "/p/main.go", "", 0, 0)
	var missing *NotInstalledError
	if !errors.As(err, &missing) || missing.Server != "gopls" || !strings.Contains(err.Error(), "jd installer") {
		t.Fatalf("got %v", err)
	}
	if _, err := manager.Definition(context.Background(), "/p/notes.txt", "", 0, 0); !errors.Is(err, ErrNoServer) {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultServers_CoverHTMLAndJavaScript(t *testing.T) {
	for file, want := range map[string][2]string{
		"index.html": {"html", "html"}, "app.js": {"typescript-language-server", "javascript"}, "x.mjs": {"typescript-language-server", "javascript"},
		"view.jsx": {"typescript-language-server", "javascriptreact"}, "main.ts": {"typescript-language-server", "typescript"},
		"a.py": {"pyright", "python"}, "A.java": {"jdtls", "java"}, "s.css": {"css", "css"},
	} {
		spec, id, ok := specFor(DefaultServers(), file)
		if !ok || spec.Name != want[0] || id != want[1] {
			t.Errorf("%s: %s %s %v", file, spec.Name, id, ok)
		}
	}
}

func TestLoadServers_MergesConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lsp.json")
	config := `{"servers":[{"name":"gopls","command":["gopls","-remote=auto"],"extensions":[".go"]},{"name":"zig","command":["zls"],"extensions":[".zig"],"languageId":"zig"}]}`
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	servers, err := LoadServers(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _ := specFor(servers, "main.go")
	if len(spec.Command) != 2 || spec.Command[1] != "-remote=auto" {
		t.Fatalf("a configured server replaces the default: %+v", spec)
	}
	if spec, id, ok := specFor(servers, "main.zig"); !ok || spec.Name != "zig" || id != "zig" {
		t.Fatalf("a new server is added: %+v", spec)
	}
	if servers, err := LoadServers(filepath.Join(t.TempDir(), "missing.json")); err != nil || len(servers) != len(DefaultServers()) {
		t.Fatalf("a missing file gives the defaults: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"servers":[{"name":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServers(path); err == nil {
		t.Fatal("an incomplete entry must be an error")
	}
}

func TestRootFor_UsesNearestMarkerBelowProjectRoot(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	module := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(filepath.Join(module, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := ServerSpec{RootMarkers: []string{"go.mod"}}
	if got := rootFor(spec, root, filepath.Join(module, "pkg", "a.go")); got != module {
		t.Fatalf("got %q, want %q", got, module)
	}
	if got := rootFor(spec, root, filepath.Join(root, "other.go")); got != root {
		t.Fatalf("without a marker the project root: %q", got)
	}
}

func TestPositionConversionsUseUTF16(t *testing.T) {
	line := []rune("a😀b")
	if got := UTF16Column(line, 2); got != 3 { // the emoji takes two units
		t.Fatalf("UTF16Column = %d", got)
	}
	if got := RuneColumn("a😀b", 3); got != 2 {
		t.Fatalf("RuneColumn = %d", got)
	}
	if got := RuneColumn("ab", 99); got != 2 {
		t.Fatalf("RuneColumn must clamp: %d", got)
	}
}

func TestURIConversionRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "with space", "ü.go")
	back, ok := URIToPath(PathToURI(path))
	if !ok || back != path {
		t.Fatalf("got %q, %v", back, ok)
	}
	if _, ok := URIToPath("jar:file:///x.jar!/A.class"); ok {
		t.Fatal("other schemes are not files")
	}
}

func TestTransport_RejectsBrokenFrames(t *testing.T) {
	for name, input := range map[string]string{
		"no length":  "X-Other: 1\r\n\r\n{}",
		"bad length": "Content-Length: abc\r\n\r\n",
		"too large":  "Content-Length: 99999999999\r\n\r\n",
		"cut body":   "Content-Length: 10\r\n\r\n{}",
	} {
		if _, err := newTransport(bufio.NewReader(strings.NewReader(input)), io.Discard).read(); err == nil {
			t.Errorf("%s: an error is expected", name)
		}
	}
}

func TestApplyEdits(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		edits []TextEdit
		want  string
	}{
		{"none", "a\nb", nil, "a\nb"},
		{"replace in a line", "hello world", []TextEdit{{Range{Position{0, 6}, Position{0, 11}}, "there"}}, "hello there"},
		{"insert", "ab", []TextEdit{{Range{Position{0, 1}, Position{0, 1}}, "X"}}, "aXb"},
		{"delete a line", "a\nb\nc", []TextEdit{{Range{Position{1, 0}, Position{2, 0}}, ""}}, "a\nc"},
		{"several edits in any order", "one\ntwo\nthree", []TextEdit{
			{Range{Position{2, 0}, Position{2, 5}}, "3"}, {Range{Position{0, 0}, Position{0, 3}}, "1"}}, "1\ntwo\n3"},
		{"utf16 columns", "a😀b", []TextEdit{{Range{Position{0, 3}, Position{0, 4}}, "B"}}, "a😀B"},
		{"append behind the last line", "a\n", []TextEdit{{Range{Position{1, 0}, Position{1, 0}}, "b\n"}}, "a\nb\n"},
		{"whole document", "x  \ny", []TextEdit{{Range{Position{0, 0}, Position{1, 1}}, "x\ny\n"}}, "x\ny\n"},
	}
	for _, tt := range tests {
		got, err := ApplyEdits(tt.text, tt.edits)
		if err != nil || got != tt.want {
			t.Errorf("%s: got %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
	if _, err := ApplyEdits("ab", []TextEdit{{Range{Position{0, 0}, Position{0, 2}}, "x"}, {Range{Position{0, 1}, Position{0, 2}}, "y"}}); err == nil {
		t.Error("overlapping edits must be an error")
	}
	if _, err := ApplyEdits("ab", []TextEdit{{Range{Position{5, 0}, Position{5, 1}}, "x"}}); err == nil {
		t.Error("an edit outside the text must be an error")
	}
	if _, err := ApplyEdits("ab", []TextEdit{{Range{Position{0, 2}, Position{0, 1}}, "x"}}); err == nil {
		t.Error("an edit that ends before it starts must be an error")
	}
}

func TestManager_FormatsWithLanguageServer(t *testing.T) {
	target := newFile(t, "main.go", "x\n")
	spec := ServerSpec{Name: "fake", Command: fakeCommand(t, target), Extensions: []string{".go"}, LanguageID: "go"}
	manager := NewManager(filepath.Dir(target), []ServerSpec{spec})
	manager.lookPath = func(string) (string, error) { return "/bin/fake", nil }
	defer manager.Close()
	got, err := manager.Format(context.Background(), target, "package x\nvar a = 1\n")
	if err != nil || got != "PACKAGE X\nvar a = 1\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestManager_FormatReportsServerWithoutFormatting(t *testing.T) {
	target := newFile(t, "main.go", "x\n")
	t.Setenv("GO_FAKE_NOFORMAT", "1")
	spec := ServerSpec{Name: "fake", Command: fakeCommand(t, target), Extensions: []string{".go"}}
	manager := NewManager(filepath.Dir(target), []ServerSpec{spec})
	manager.lookPath = func(string) (string, error) { return "/bin/fake", nil }
	defer manager.Close()
	if _, err := manager.Format(context.Background(), target, "x"); !errors.Is(err, ErrCannotFormat) {
		t.Fatalf("got %v", err)
	}
}

func TestManager_FormatsWithExternalCommandWhenInstalled(t *testing.T) {
	if _, err := exec.LookPath("tr"); err != nil {
		t.Skip("tr is not available")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	spec := ServerSpec{Name: "x", Command: []string{"never-started"}, Extensions: []string{".txt"}, Formatter: []string{"tr", "a-z", "A-Z"}}
	manager := NewManager(dir, []ServerSpec{spec})
	got, err := manager.Format(context.Background(), filepath.Join(dir, "a.txt"), "hello\n")
	if err != nil || got != "HELLO\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	spec.Formatter = []string{"tr", "--definitely-wrong-flag"}
	manager = NewManager(dir, []ServerSpec{spec})
	if _, err := manager.Format(context.Background(), filepath.Join(dir, "a.txt"), "x"); err == nil || !strings.HasPrefix(err.Error(), "tr: ") {
		t.Fatalf("a failing formatter must report its message: %v", err)
	}
	if _, err := manager.Format(context.Background(), filepath.Join(dir, "a.unknown"), "x"); !errors.Is(err, ErrNoServer) {
		t.Fatalf("got %v", err)
	}
}

func TestManager_UsesServerWhenFormatterNotInstalled(t *testing.T) {
	target := newFile(t, "main.go", "x\n")
	spec := ServerSpec{Name: "fake", Command: fakeCommand(t, target), Extensions: []string{".go"}, LanguageID: "go", Formatter: []string{"formatter-that-is-not-there"}}
	manager := NewManager(filepath.Dir(target), []ServerSpec{spec})
	manager.lookPath = func(name string) (string, error) {
		if name == "formatter-that-is-not-there" {
			return "", errors.New("missing")
		}
		return "/bin/" + name, nil
	}
	defer manager.Close()
	got, err := manager.Format(context.Background(), target, "go\n")
	if err != nil || got != "GO\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestIndentationOf(t *testing.T) {
	for name, tt := range map[string]struct {
		text   string
		size   int
		spaces bool
	}{
		"tabs":       {"a\n\tb\n\tc\n", 4, false},
		"two spaces": {"a\n  b\n    c\n", 2, true},
		"four":       {"a\n    b\n        c\n", 4, true},
		"none":       {"a\nb\n", 4, true},
	} {
		if size, spaces := indentationOf(tt.text); size != tt.size || spaces != tt.spaces {
			t.Errorf("%s: %d %v", name, size, spaces)
		}
	}
}

func TestDefaultServers_HaveFormatters(t *testing.T) {
	want := map[string]string{
		"main.go": "gofmt", "app.py": "black", "page.html": "prettier", "style.css": "prettier", "theme.scss": "prettier",
		"app.js": "prettier", "view.jsx": "prettier", "main.ts": "prettier", "view.tsx": "prettier", "esm.mts": "prettier", "config.json": "prettier",
	}
	for file, formatter := range want {
		spec, _, ok := specFor(DefaultServers(), file)
		if !ok || len(spec.Formatter) == 0 || spec.Formatter[0] != formatter {
			t.Errorf("%s: formatter %v, want %s", file, spec.Formatter, formatter)
		}
	}
	if spec, _, _ := specFor(DefaultServers(), "Main.java"); len(spec.Formatter) != 0 {
		t.Errorf("Java is formatted by jdtls, not by a command: %v", spec.Formatter)
	}
	if spec, _, _ := specFor(DefaultServers(), "page.html"); spec.Formatter[len(spec.Formatter)-1] != "{file}" {
		t.Errorf("prettier needs the file name to pick its parser: %v", spec.Formatter)
	}
}

func TestClient_CompletionAnswersInAllShapes(t *testing.T) {
	target := newFile(t, "a.go", "x\n")
	for _, shape := range []string{"array", "list"} {
		t.Setenv("GO_FAKE_COMPLETION", shape)
		client, err := Start(context.Background(), "fake", fakeCommand(t, target), filepath.Dir(target))
		if err != nil {
			t.Fatal(err)
		}
		items, err := client.Complete(context.Background(), target, Position{})
		client.Close()
		if err != nil || len(items) != 5 {
			t.Fatalf("%s: %v, %d items", shape, err, len(items))
		}
		if items[0].Label != "println" || items[0].Kind != 3 || items[0].Detail != "func(a ...any)" {
			t.Errorf("%s: first item %+v", shape, items[0])
		}
		if items[2].InsertText != "snip(arg, a)" {
			t.Errorf("%s: snippets lose their placeholders: %q", shape, items[2].InsertText)
		}
		if edit := items[3].Edit; edit == nil || edit.NewText != "EDITED" || edit.Range.Start.Character != 1 {
			t.Errorf("%s: textEdit %+v", shape, items[3].Edit)
		}
		if edit := items[4].Edit; edit == nil || edit.Range.End.Character != 4 {
			t.Errorf("%s: an insert/replace edit uses the replace range: %+v", shape, items[4].Edit)
		}
	}
	t.Setenv("GO_FAKE_COMPLETION", "null")
	client, err := Start(context.Background(), "fake", fakeCommand(t, target), filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if items, err := client.Complete(context.Background(), target, Position{}); err != nil || len(items) != 0 {
		t.Fatalf("null answer: %v %v", items, err)
	}
}

func TestManager_CompleteUsesEditorTextAndPosition(t *testing.T) {
	target := newFile(t, "a.go", "x\n")
	spec := ServerSpec{Name: "fake", Command: fakeCommand(t, target), Extensions: []string{".go"}, LanguageID: "go"}
	manager := NewManager(filepath.Dir(target), []ServerSpec{spec})
	manager.lookPath = func(string) (string, error) { return "/bin/fake", nil }
	defer manager.Close()
	items, err := manager.Complete(context.Background(), target, "pri", 0, 3)
	if err != nil || len(items) == 0 {
		t.Fatalf("%v %v", items, err)
	}
}

func TestSnippetText(t *testing.T) {
	for in, want := range map[string]string{
		"plain":               "plain",
		"f(${1:a}, ${2:b})$0": "f(a, b)",
		"${1|x,y|} z":         "x z",
		`cost \$5 $1`:         "cost $5 ",
		"${3}end":             "end",
	} {
		if got := SnippetText(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if KindName(3) != "func" || KindName(999) != "" {
		t.Error("kind names")
	}
}

func TestSpecFor_AngularTemplatesGoToAngularServerAndOtherHTMLToHTMLServer(t *testing.T) {
	servers := DefaultServers()
	for file, want := range map[string]string{
		"src/app/app.component.html": "angular", "index.html": "html", "page.htm": "html", "component.html": "html", "a.component.html.bak": "",
	} {
		spec, id, ok := specFor(servers, file)
		if want == "" {
			if ok {
				t.Errorf("%s: unexpected server %s", file, spec.Name)
			}
			continue
		}
		if !ok || spec.Name != want || id != "html" {
			t.Errorf("%s: %s %s %v, want %s", file, spec.Name, id, ok, want)
		}
	}
	if spec, _, _ := specFor(servers, "app.component.html"); len(spec.Formatter) == 0 || spec.Formatter[0] != "prettier" {
		t.Errorf("Angular templates are formatted with prettier: %v", spec.Formatter)
	}
}

func TestExpandCommand(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("USERPROFILE", "/home/me")
	got := expandCommand([]string{"ngserver", "--probe", "{root}/node_modules,{home}/.local/lib/node_modules", "plain"}, "/work/app")
	want := []string{"ngserver", "--probe", "/work/app/node_modules,/home/me/.local/lib/node_modules", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
