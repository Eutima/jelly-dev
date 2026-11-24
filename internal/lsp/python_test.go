package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// machine is a fake file system and environment for pythonFinder: files are the paths that exist.
type pythonMachine struct {
	env      map[string]string
	files    map[string]bool
	tools    map[string]bool
	uvPath   string // what `uv python find` prints, "" for an error
	uvCalls  int
	uvDir    string
	contents map[string][]byte // files that are read (pyproject.toml)
}

func (m *pythonMachine) finder(goos string) pythonFinder {
	return pythonFinder{
		getenv: func(name string) string { return m.env[name] },
		lookPath: func(name string) (string, error) {
			if m.tools[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		isFile: func(path string) bool { return m.files[path] },
		run: func(_ context.Context, dir, name string, args ...string) (string, error) {
			m.uvCalls++
			m.uvDir = dir
			if m.uvPath == "" {
				return "", errors.New("no interpreter")
			}
			return m.uvPath + "\n", nil
		},
		readFile: func(path string) ([]byte, error) { return m.contents[path], m.contentErr(path) },
		goos:     goos,
	}
}

func (m *pythonMachine) contentErr(path string) error {
	if _, ok := m.contents[path]; ok {
		return nil
	}
	return os.ErrNotExist
}

func newPythonMachine() *pythonMachine {
	return &pythonMachine{env: map[string]string{}, files: map[string]bool{}, tools: map[string]bool{}, contents: map[string][]byte{}}
}

func TestPythonFinder_FindsThePythonOfEveryKindOfSetup(t *testing.T) {
	root := "/work/app"
	venv := filepath.Join(root, ".venv", "bin", "python")

	// a normal installation: nothing is found, pyright uses the Python on the PATH
	m := newPythonMachine()
	if got := m.finder("linux").Find(root); got != "" {
		t.Fatalf("a normal installation needs no setting: %q", got)
	}
	// venv, virtualenv, uv sync, uv venv, poetry and pdm in the project: .venv, venv or env in the project
	for _, dir := range []string{".venv", "venv", "env"} {
		m := newPythonMachine()
		want := filepath.Join(root, dir, "bin", "python")
		m.files[want] = true
		if got := m.finder("linux").Find(root); got != want {
			t.Errorf("%s in the project: %q, want %q", dir, got, want)
		}
	}
	// python3 only (some environments have no python)
	m = newPythonMachine()
	m.files[filepath.Join(root, ".venv", "bin", "python3")] = true
	if got := m.finder("linux").Find(root); got != filepath.Join(root, ".venv", "bin", "python3") {
		t.Errorf("an environment with python3 only: %q", got)
	}
	// an environment that is active when jd starts (venv, virtualenv, uv, poetry shell)
	m = newPythonMachine()
	active := "/home/me/envs/shop"
	m.env["VIRTUAL_ENV"] = active
	m.files[filepath.Join(active, "bin", "python")] = true
	m.files[venv] = true // the project has one too: the active one wins
	if got := m.finder("linux").Find(root); got != filepath.Join(active, "bin", "python") {
		t.Errorf("VIRTUAL_ENV wins over the project's .venv: %q", got)
	}
	// VIRTUAL_ENV that holds no interpreter (a stale variable) does not hide the project's environment
	m = newPythonMachine()
	m.env["VIRTUAL_ENV"] = "/gone"
	m.files[venv] = true
	if got := m.finder("linux").Find(root); got != venv {
		t.Errorf("a stale VIRTUAL_ENV is ignored: %q", got)
	}
	// uv with its environment somewhere else: relative to the project, or absolute
	m = newPythonMachine()
	m.env["UV_PROJECT_ENVIRONMENT"] = ".cache/env"
	want := filepath.Join(root, ".cache", "env", "bin", "python")
	m.files[want] = true
	if got := m.finder("linux").Find(root); got != want {
		t.Errorf("UV_PROJECT_ENVIRONMENT relative to the project: %q", got)
	}
	// conda
	m = newPythonMachine()
	m.env["CONDA_PREFIX"] = "/opt/conda/envs/ml"
	m.files["/opt/conda/envs/ml/bin/python"] = true
	if got := m.finder("linux").Find(root); got != "/opt/conda/envs/ml/bin/python" {
		t.Errorf("an active conda environment: %q", got)
	}
	// the user names the interpreter
	m = newPythonMachine()
	m.env[EnvPython] = "/usr/local/bin/python3.12"
	m.files["/usr/local/bin/python3.12"] = true
	m.files[venv] = true
	if got := m.finder("linux").Find(root); got != "/usr/local/bin/python3.12" {
		t.Errorf("JD_PYTHON wins over everything: %q", got)
	}
	// Windows has its interpreter in Scripts
	m = newPythonMachine()
	winPython := filepath.Join(root, ".venv", "Scripts", "python.exe")
	m.files[winPython] = true
	if got := m.finder("windows").Find(root); got != winPython {
		t.Errorf("a Windows environment: %q", got)
	}
	if got := m.finder("linux").Find(root); got != "" {
		t.Errorf("the Windows layout is no Linux environment: %q", got)
	}
}

func TestPythonFinder_AsksUvOnlyForUvProjectsWithoutAnEnvironmentInTheProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "uv.lock"), []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newPythonMachine()
	m.tools["uv"] = true
	m.uvPath = "/home/me/.cache/uv/envs/app/bin/python"
	m.files[m.uvPath] = true
	m.files[filepath.Join(root, "uv.lock")] = true
	if got := m.finder("linux").Find(root); got != m.uvPath || m.uvDir != root {
		t.Fatalf("uv knows the environment of its project: %q (asked in %q)", got, m.uvDir)
	}
	// uv, but not a uv project
	other := newPythonMachine()
	other.tools["uv"] = true
	other.uvPath = "/p/python"
	other.files["/p/python"] = true
	if got := other.finder("linux").Find(t.TempDir()); got != "" || other.uvCalls != 0 {
		t.Fatalf("uv is not asked about a project that does not use it: %q %d", got, other.uvCalls)
	}
	// a pyproject.toml with [tool.uv]
	pyproject := t.TempDir()
	viaToml := newPythonMachine()
	viaToml.contents[filepath.Join(pyproject, "pyproject.toml")] = []byte("[project]\nname = \"x\"\n[tool.uv]\ndev-dependencies = []\n")
	viaToml.tools["uv"] = true
	viaToml.uvPath = "/p/python"
	viaToml.files["/p/python"] = true
	if got := viaToml.finder("linux").Find(pyproject); got != "/p/python" {
		t.Fatalf("[tool.uv] marks a uv project: %q", got)
	}
	// uv fails or says a path that does not exist: no setting
	broken := newPythonMachine()
	broken.tools["uv"] = true
	broken.files[filepath.Join(root, "uv.lock")] = true
	if got := broken.finder("linux").Find(root); got != "" {
		t.Fatalf("a failing uv gives no setting: %q", got)
	}
	// the project's own .venv is found without running uv
	withVenv := newPythonMachine()
	withVenv.tools["uv"] = true
	withVenv.files[filepath.Join(root, ".venv", "bin", "python")] = true
	withVenv.files[filepath.Join(root, "uv.lock")] = true
	if got := withVenv.finder("linux").Find(root); got != filepath.Join(root, ".venv", "bin", "python") || withVenv.uvCalls != 0 {
		t.Fatalf("an environment in the project needs no question to uv: %q %d", got, withVenv.uvCalls)
	}
}

func TestSettingAt_FindsSectionsOfTheSettings(t *testing.T) {
	settings := map[string]any{"python": map[string]any{"pythonPath": "/p/python", "analysis": map[string]any{"mode": "basic"}}}
	if got := settingAt(settings, "python.pythonPath"); got != "/p/python" {
		t.Errorf("a leaf: %v", got)
	}
	if got, ok := settingAt(settings, "python.analysis").(map[string]any); !ok || got["mode"] != "basic" {
		t.Errorf("a section: %v", got)
	}
	for _, none := range []string{"python.nothing", "go", "python.pythonPath.deeper"} {
		if got := settingAt(settings, none); got != nil {
			t.Errorf("%s has no setting: %v", none, got)
		}
	}
	if settingAt(nil, "python") != nil || settingAt(settings, "") == nil {
		t.Error("no settings give nothing; the empty section is all of them")
	}
}

func TestClient_ConfigureSendsTheSettingsAndAnswersTheServersQuestionsWithThem(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv("GO_FAKE_LOG", log)
	target := newFile(t, "app.py", "x = 1\n")
	client, err := Start(context.Background(), "pyright", fakeCommand(t, target), filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Configure(map[string]any{"python": map[string]any{"pythonPath": "/p/.venv/bin/python"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(log)
		text = string(data)
		if strings.Contains(text, "response 903") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(text, `didChangeConfiguration {"settings":{"python":{"pythonPath":"/p/.venv/bin/python"}}}`) {
		t.Fatalf("the settings are sent to the server:\n%s", text)
	}
	if !strings.Contains(text, `response 903 [{"pythonPath":"/p/.venv/bin/python"},null,"/p/.venv/bin/python",null]`) {
		t.Fatalf("the sections the server asks for are answered from the settings, the rest with null:\n%s", text)
	}
}

func TestManager_OnlyPyrightGetsAPythonAndTheEnvironmentAtTheTopOfAProjectIsUsed(t *testing.T) {
	var told []string
	m := NewManager("/work/mono", DefaultServers())
	m.python = func(root string) string {
		told = append(told, root)
		if root == "/work/mono" {
			return "/work/mono/.venv/bin/python"
		}
		return "" // the sub project has no environment of its own
	}
	specs := map[string]ServerSpec{}
	for _, spec := range DefaultServers() {
		specs[spec.Name] = spec
	}
	if got := m.pythonFor(specs["gopls"], "/work/mono"); got != "" || len(told) != 0 {
		t.Fatalf("only pyright needs a Python: %q %v", got, told)
	}
	if got := m.pythonFor(specs["pyright"], "/work/mono/services/api"); got != "/work/mono/.venv/bin/python" {
		t.Fatalf("the environment at the top of the project is used: %q (asked %v)", got, told)
	}
}

// The whole way: a request for a Python file starts pyright and tells it the interpreter of the project.
func TestManager_StartingPyrightTellsItTheInterpreter(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	t.Setenv("GO_FAKE_LOG", log)
	target := newFile(t, "app.py", "x = 1\n")
	m := NewManager(filepath.Dir(target), DefaultServers())
	m.lookPath = func(string) (string, error) { return "/usr/bin/fake", nil }
	m.start = func(ctx context.Context, name string, command []string, root string) (*Client, error) {
		return Start(ctx, name, fakeCommand(t, target), root)
	}
	m.python = func(string) string { return "/p/uv-env/bin/python" }
	defer m.Close()
	if _, _, err := m.client(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(log); strings.Contains(string(data), `"pythonPath":"/p/uv-env/bin/python"`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pyright was not told the interpreter")
}

func TestPythonFinder_UvicornIsNoUvAConfiguredPythonThatDoesNotExistIsIgnoredAndCondaOnWindows(t *testing.T) {
	root := "/work/app"
	// [tool.uvicorn] is not [tool.uv]
	m := newPythonMachine()
	m.tools["uv"] = true
	m.uvPath = "/p/python"
	m.files["/p/python"] = true
	m.contents[filepath.Join(root, "pyproject.toml")] = []byte("[tool.uvicorn]\nport = 8000\n")
	if got := m.finder("linux").Find(root); got != "" || m.uvCalls != 0 {
		t.Errorf("a project with [tool.uvicorn] is no uv project: %q %d", got, m.uvCalls)
	}
	m.contents[filepath.Join(root, "pyproject.toml")] = []byte("[tool.uv.sources]\nlib = { path = \"../lib\" }\n")
	if got := m.finder("linux").Find(root); got != "/p/python" {
		t.Errorf("[tool.uv.sources] marks a uv project: %q", got)
	}
	// JD_PYTHON that names nothing is ignored, a command on the PATH counts
	n := newPythonMachine()
	n.env[EnvPython] = "/nowhere/python"
	n.files[filepath.Join(root, ".venv", "bin", "python")] = true
	if got := n.finder("linux").Find(root); got != filepath.Join(root, ".venv", "bin", "python") {
		t.Errorf("a wrong JD_PYTHON is ignored: %q", got)
	}
	n.env[EnvPython] = "python3.12"
	n.tools["python3.12"] = true
	if got := n.finder("linux").Find(root); got != "python3.12" {
		t.Errorf("a command on the PATH is fine: %q", got)
	}
	// conda on Windows keeps python.exe at the top of the environment
	w := newPythonMachine()
	w.env["CONDA_PREFIX"] = `C:\conda\envs\ml`
	w.files[filepath.Join(`C:\conda\envs\ml`, "python.exe")] = true
	if got := w.finder("windows").Find(root); got != filepath.Join(`C:\conda\envs\ml`, "python.exe") {
		t.Errorf("a conda environment on Windows: %q", got)
	}
}

// Looking for the Python (asking uv can take seconds) must not hold the lock that every language server request needs.
func TestManager_SlowPythonLookupDoesNotBlockOtherServers(t *testing.T) {
	target := newFile(t, "app.py", "x = 1\n")
	goFile := newFile(t, "main.go", "package main\n")
	m := NewManager(filepath.Dir(target), DefaultServers())
	m.lookPath = func(string) (string, error) { return "/usr/bin/fake", nil }
	m.start = func(ctx context.Context, name string, command []string, root string) (*Client, error) {
		return Start(ctx, name, fakeCommand(t, target), root)
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	m.python = func(string) string {
		close(entered)
		<-release // the slow `uv python find`
		return ""
	}
	pyDone := make(chan error, 1)
	go func() {
		_, _, err := m.client(context.Background(), target)
		pyDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the Python lookup never started")
	}
	goDone := make(chan error, 1)
	go func() {
		_, _, err := m.client(context.Background(), goFile)
		goDone <- err
	}()
	select {
	case err := <-goDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a request for another server waits for the slow Python lookup")
	}
	close(release)
	if err := <-pyDone; err != nil {
		t.Fatal(err)
	}
	m.Close()
}

// A scratch file lives outside every project: pyright still runs for the project jd was opened in, with the environment found there.
func TestPythonFor_AFileOutsideTheProjectUsesTheEnvironmentOfTheProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the layout of the environment differs on Windows")
	}
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CONDA_PREFIX", "")
	t.Setenv("UV_PROJECT_ENVIRONMENT", "")
	t.Setenv(EnvPython, "")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	python := filepath.Join(root, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(t.TempDir(), "scratches", "try.py")
	var pyright ServerSpec
	for _, spec := range DefaultServers() {
		if spec.Name == "pyright" {
			pyright = spec
		}
	}
	workspace := rootFor(pyright, root, scratch)
	if workspace != root {
		t.Fatalf("workspace of a scratch file = %q, want the project %q", workspace, root)
	}
	m := NewManager(root, DefaultServers())
	if got := m.pythonFor(pyright, workspace); got != python {
		t.Fatalf("interpreter = %q, want %q", got, python)
	}
}
