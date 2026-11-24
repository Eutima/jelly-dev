package dap

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrNoRunner is returned for files that jd does not know how to run.
var ErrNoRunner = errors.New("no way to run this file type")

// Command is a program to run in a shell: what the run of a file (F5) starts.
type Command struct {
	Argv []string          // the program and its arguments
	Dir  string            // the folder to run it in
	Env  map[string]string // variables to add to the environment
}

// RunCommand returns how a file runs without a debugger. A Python file follows the rules of the debugger (a script runs as it is, in a
// Django project a test file runs its tests and any other file starts the development server) except that the server keeps its reloader.
// A Go test file runs its tests, any other Go file the program of its folder; JavaScript and TypeScript run with Node.js.
func RunCommand(file, root string) (Command, error) {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".py":
		config := pythonConfig(file, root, true)
		python, _ := config["python"].(string)
		if python == "" {
			python = "python3"
		}
		program, _ := config["program"].(string)
		args, _ := config["args"].([]string)
		dir, _ := config["cwd"].(string)
		env, _ := config["env"].(map[string]string)
		return Command{Argv: append([]string{python, program}, args...), Dir: dir, Env: env}, nil
	case ".go":
		verb := "run"
		if strings.HasSuffix(file, "_test.go") {
			verb = "test"
		}
		return Command{Argv: []string{"go", verb, "."}, Dir: filepath.Dir(file)}, nil
	case ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts":
		return Command{Argv: []string{"node", file}, Dir: root}, nil // found by the shell the command is typed into
	}
	return Command{}, ErrNoRunner
}
