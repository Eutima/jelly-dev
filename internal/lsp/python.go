package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// EnvPython lets the user name the Python interpreter the language server resolves imports with.
const EnvPython = "JD_PYTHON"

// pythonFinder finds the Python interpreter of a project, so that pyright resolves the imports of the project's packages and not
// only those of the Python it finds on the PATH. Four kinds of setups work, with no setting needed:
//
//   - an environment that is active when jd starts (VIRTUAL_ENV: venv, virtualenv, uv, poetry shell; CONDA_PREFIX),
//   - an environment in the project: .venv (uv sync, uv venv, python -m venv .venv, poetry and pdm in the project) or venv or env,
//   - a uv project whose environment is elsewhere (UV_PROJECT_ENVIRONMENT, or what `uv python find` says),
//   - a normal installation: nothing is found and pyright uses the Python on the PATH.
//
// JD_PYTHON (the interpreter itself) wins over all of them; a pyrightconfig.json or [tool.pyright] with its own venv wins in pyright.
type pythonFinder struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	isFile   func(string) bool
	run      func(ctx context.Context, dir, name string, args ...string) (string, error)
	readFile func(string) ([]byte, error)
	goos     string
}

func newPythonFinder() pythonFinder {
	return pythonFinder{getenv: os.Getenv, lookPath: exec.LookPath, isFile: isRegularFile, run: runOutput, readFile: os.ReadFile, goos: runtime.GOOS}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func runOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- uv with fixed arguments
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// interpreterIn returns the Python inside an environment folder, "" when the folder holds none.
func (f pythonFinder) interpreterIn(env string) string {
	if env == "" {
		return ""
	}
	names := []string{filepath.Join("bin", "python"), filepath.Join("bin", "python3")}
	if f.goos == "windows" {
		names = []string{filepath.Join("Scripts", "python.exe"), "python.exe"} // conda keeps python.exe at the top of the environment
	}
	for _, name := range names {
		if path := filepath.Join(env, name); f.isFile(path) {
			return path
		}
	}
	return ""
}

// Find returns the interpreter to resolve the imports of the project in root with; "" means the Python on the PATH.
func (f pythonFinder) Find(root string) string {
	if python := f.getenv(EnvPython); python != "" {
		if _, err := f.lookPath(python); err == nil || f.isFile(python) { // a path, or a command like python3.12; a wrong one is ignored
			return python
		}
	}
	if python := f.interpreterIn(f.getenv("VIRTUAL_ENV")); python != "" {
		return python
	}
	if env := f.getenv("UV_PROJECT_ENVIRONMENT"); env != "" {
		if !filepath.IsAbs(env) {
			env = filepath.Join(root, env)
		}
		if python := f.interpreterIn(env); python != "" {
			return python
		}
	}
	for _, name := range []string{".venv", "venv", "env"} {
		if python := f.interpreterIn(filepath.Join(root, name)); python != "" {
			return python
		}
	}
	if python := f.interpreterIn(f.getenv("CONDA_PREFIX")); python != "" {
		return python
	}
	return f.uvPython(root)
}

// uvPython asks uv for the interpreter of a uv project (uv.lock, or [tool.uv] in pyproject.toml); the project's own .venv was
// found before, this covers environments uv keeps elsewhere.
func (f pythonFinder) uvPython(root string) string {
	if _, err := f.lookPath("uv"); err != nil || !f.isUvProject(root) {
		return ""
	}
	out, err := f.run(context.Background(), root, "uv", "python", "find")
	if err != nil {
		return ""
	}
	path, _, _ := strings.Cut(out, "\n")
	path = strings.TrimSpace(path)
	if path == "" || !f.isFile(path) {
		return ""
	}
	return path
}

func (f pythonFinder) isUvProject(root string) bool {
	if f.isFile(filepath.Join(root, "uv.lock")) {
		return true
	}
	data, err := f.readFile(filepath.Join(root, "pyproject.toml"))
	text := string(data)
	// [tool.uv] and [tool.uv.sources], not [tool.uvicorn]
	return err == nil && (strings.Contains(text, "[tool.uv]") || strings.Contains(text, "[tool.uv."))
}

// FindPython returns the Python interpreter of the project in root (see pythonFinder); "" means the one on the PATH. The debugger runs
// the program with it, so the packages of the project's environment are there.
func FindPython(root string) string { return newPythonFinder().Find(root) }
