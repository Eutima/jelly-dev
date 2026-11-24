package dap

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eutima/jelly-dev/internal/lsp"
)

// mainGuard finds the "if __name__ == '__main__':" of a script.
var mainGuard = regexp.MustCompile(`(?m)^if\s+__name__\s*==\s*['"]__main__['"]`)

// maxScriptRead is how much of a Python file is looked at for the main guard.
const maxScriptRead = 1 << 20

// pythonInterpreter is the Python that runs the program: the one of the project (its virtual environment), else the one on the PATH.
func pythonInterpreter(root string) string {
	if python := lsp.FindPython(root); python != "" {
		return python
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// djangoRoot returns the folder with manage.py that a file belongs to: the file's folder or the nearest one above it, up to the project.
func djangoRoot(file, root string) string {
	if !within(file, root) {
		return ""
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "manage.py")); err == nil && info.Mode().IsRegular() {
			return dir
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir {
			return ""
		}
		dir = parent
	}
}

// within tells whether a path lies inside a folder (not beside it: /a/proj2 is not in /a/proj).
func within(path, folder string) bool {
	relative, err := filepath.Rel(folder, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// isScript tells whether a file starts something when it is run (it has a main guard) and so is meant to be run on its own.
func isScript(file string) bool {
	handle, err := os.Open(file)
	if err != nil {
		return false
	}
	defer handle.Close()
	text, err := io.ReadAll(io.LimitReader(handle, maxScriptRead))
	return err == nil && mainGuard.Match(text)
}

// isTestFile tells whether a Python file holds tests by its name or by a "tests" folder between the project folder and the file (a project
// that happens to lie below a folder called tests is no test project).
func isTestFile(file, project string) bool {
	base := filepath.Base(file)
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || base == "tests.py" {
		return true
	}
	relative, err := filepath.Rel(project, filepath.Dir(file))
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "tests" {
			return true
		}
	}
	return false
}

// moduleName is a name Python can import.
var moduleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// testLabel is what manage.py test is given for a test file (its path relative to manage.py): the dotted module name, or when a part is no
// module name (a dash or a space in a folder name) the folder of the file, which manage.py test also takes.
func testLabel(relative string) string {
	parts := strings.Split(strings.TrimSuffix(filepath.ToSlash(relative), ".py"), "/")
	for _, part := range parts {
		if !moduleName.MatchString(part) {
			if dir := filepath.ToSlash(filepath.Dir(relative)); dir != "." {
				return dir
			}
			return "."
		}
	}
	return strings.Join(parts, ".")
}

// pythonLaunch builds the launch request for debugpy. In a Django project (a manage.py above the file) a file that is not a script of its
// own is debugged through manage.py: a test file runs its tests (manage.py test <module>), any other file runs the development server
// (runserver without the reloader, which would start the program a second time where the debugger is not attached), so a breakpoint in a
// view is hit by the next request. Anything else runs as a script.
func pythonLaunch(file, root string) map[string]any { return pythonConfig(file, root, false) }

// pythonConfig is pythonLaunch for the debugger (run false) or for a plain run (run true): a run keeps the reloader of the development
// server, since no debugger has to stay attached to the process.
func pythonConfig(file, root string, run bool) map[string]any {
	config := map[string]any{
		"type": "python", "request": "launch", "console": "internalConsole", "justMyCode": true, "cwd": root,
		"env": map[string]string{"PYTHONUNBUFFERED": "1"}, // the output comes at once, not when the program ends
	}
	if python := pythonInterpreter(root); python != "" {
		config["python"] = python
	}
	config["program"] = file
	if !within(file, root) { // a scratch file: Python puts the folder of the file on the path, so give it the project's, as `python -m` would
		config["env"] = map[string]string{"PYTHONUNBUFFERED": "1", "PYTHONPATH": pythonPath(root, os.Getenv("PYTHONPATH"))}
	}
	manage := djangoRoot(file, root)
	if manage == "" || (isScript(file) && filepath.Base(file) != "manage.py") {
		return config
	}
	config["cwd"], config["program"] = manage, filepath.Join(manage, "manage.py")
	switch {
	case filepath.Base(file) == "manage.py":
		config["args"], config["django"] = serverArgs(run), true
	case isTestFile(file, manage):
		relative, err := filepath.Rel(manage, file)
		if err != nil {
			relative = filepath.Base(file)
		}
		config["args"] = []string{"test", testLabel(relative)}
	default:
		config["args"], config["django"] = serverArgs(run), true
	}
	return config
}

// pythonPath is the PYTHONPATH with the project folder in front of what the environment has already.
func pythonPath(root, existing string) string {
	if existing == "" {
		return root
	}
	return root + string(os.PathListSeparator) + existing
}

// serverArgs are the arguments of the development server: with the reloader, which starts the program a second time, only for a plain run.
func serverArgs(run bool) []string {
	if run {
		return []string{"runserver"}
	}
	return []string{"runserver", "--noreload"}
}
