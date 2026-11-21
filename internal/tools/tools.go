// Package tools installs the language servers and formatters the editor uses. jd checks for them when it starts and installs
// what is missing, with the language's own tool: npm, go, pip or pipx, or a download. It never installs a runtime
// (Go, Node.js, Python, Java): that needs root rights and is the installer's job; a missing runtime is reported.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Kind is how a tool is installed.
type Kind int

const (
	// NPM tools are installed with npm install -g (below ~/.local, so no root rights are needed).
	NPM Kind = iota
	// Go tools are installed with go install into ~/.local/bin.
	Go
	// Python tools are installed with pipx, or pip for the user.
	Python
	// Download tools are fetched from their project's site.
	Download
)

// Tool is a language server or formatter.
type Tool struct {
	Name     string // shown to the user
	Binary   string // the command that shows that it is installed
	Kind     Kind
	Packages []string // npm packages, the Go package path, the pip package
	Purpose  string   // what it is for, e.g. "language server for Go"
}

// Defaults are the tools jd installs: the servers of lsp.DefaultServers and their formatters.
func Defaults() []Tool {
	return []Tool{
		{Name: "gopls", Binary: "gopls", Kind: Go, Packages: []string{"golang.org/x/tools/gopls"}, Purpose: "language server for Go"},
		{Name: "typescript-language-server", Binary: "typescript-language-server", Kind: NPM,
			Packages: []string{"typescript", "typescript-language-server"}, Purpose: "language server for JavaScript and TypeScript"},
		{Name: "pyright", Binary: "pyright-langserver", Kind: NPM, Packages: []string{"pyright"}, Purpose: "language server for Python"},
		{Name: "vscode-langservers-extracted", Binary: "vscode-html-language-server", Kind: NPM,
			Packages: []string{"vscode-langservers-extracted"}, Purpose: "language servers for HTML, CSS and JSON"},
		{Name: "angular-language-server", Binary: "ngserver", Kind: NPM,
			Packages: []string{"@angular/language-server", "@angular/language-service", "typescript"}, Purpose: "language server for Angular templates"},
		{Name: "jdtls", Binary: "jdtls", Kind: Download, Purpose: "language server for Java"},
		{Name: "groovy-language-server", Binary: "groovy-language-server", Kind: Download, Purpose: "language server for Groovy"},
		{Name: "prettier", Binary: "prettier", Kind: NPM, Packages: []string{"prettier"}, Purpose: "formatter for HTML, CSS, JavaScript, TypeScript and JSON"},
		{Name: "black", Binary: "black", Kind: Python, Packages: []string{"black"}, Purpose: "formatter for Python"},
	}
}

// MissingRuntimeError is returned when the tool needed to install something (npm, go, pip, java) is not there.
type MissingRuntimeError struct{ Tool, Needs string }

func (e *MissingRuntimeError) Error() string {
	return fmt.Sprintf("%s needs %s, which is not installed (the jd installer installs it)", e.Tool, e.Needs)
}

// Env is what installing depends on; tests replace it.
type Env struct {
	Home     string
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Run runs a command with extra environment variables and returns the tail of its output on failure.
	Run func(ctx context.Context, env []string, name string, args ...string) error
	// Fetch downloads a URL into a file.
	Fetch func(ctx context.Context, url, dest string) error
}

// DefaultEnv is the real machine.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, GOOS: runtime.GOOS, Getenv: os.Getenv, LookPath: exec.LookPath, Run: runCommand, Fetch: fetch}
}

// BinDir is the folder jd installs command line tools into.
func (e Env) BinDir() string { return filepath.Join(e.Home, ".local", "bin") }

// DataDir is the folder jd installs downloaded tools into.
func (e Env) DataDir() string {
	if dir := e.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(e.Home, ".local", "share")
}

// ExtraPaths are the folders tools end up in, which may not be on the PATH of the shell that started jd.
func (e Env) ExtraPaths() []string {
	paths := []string{e.BinDir()}
	if gobin := e.Getenv("GOBIN"); gobin != "" {
		paths = append(paths, gobin)
	}
	if gopath := e.Getenv("GOPATH"); gopath != "" {
		paths = append(paths, filepath.Join(gopath, "bin"))
	} else {
		paths = append(paths, filepath.Join(e.Home, "go", "bin"))
	}
	if e.GOOS == "windows" {
		if appdata := e.Getenv("APPDATA"); appdata != "" {
			paths = append(paths, filepath.Join(appdata, "npm"))
		}
	}
	return paths
}

// EnsurePath puts the folders of installed tools at the end of the PATH of this process (and so of the language servers
// and formatters jd starts), unless they are on it already.
func EnsurePath() {
	env := DefaultEnv()
	current := os.Getenv("PATH")
	have := map[string]bool{}
	for _, dir := range filepath.SplitList(current) {
		have[dir] = true
	}
	for _, dir := range env.ExtraPaths() {
		if dir != "" && !have[dir] {
			current += string(os.PathListSeparator) + dir
		}
	}
	_ = os.Setenv("PATH", current)
}

// Installed reports whether the tool's command can be found.
func (e Env) Installed(t Tool) bool {
	if _, err := e.LookPath(t.Binary); err == nil {
		return true
	}
	return false
}

// Missing returns the tools that are not installed.
func (e Env) Missing(list []Tool) []Tool {
	var missing []Tool
	for _, t := range list {
		if !e.Installed(t) {
			missing = append(missing, t)
		}
	}
	return missing
}

// need returns the first of the commands that exists, or a MissingRuntimeError naming them.
func (e Env) need(t Tool, label string, commands ...string) (string, error) {
	for _, c := range commands {
		if _, err := e.LookPath(c); err == nil {
			return c, nil
		}
	}
	return "", &MissingRuntimeError{Tool: t.Name, Needs: label}
}

// Install installs one tool.
func (e Env) Install(ctx context.Context, t Tool) error {
	var err error
	switch t.Kind {
	case NPM:
		err = e.installNPM(ctx, t)
	case Go:
		err = e.installGo(ctx, t)
	case Python:
		err = e.installPython(ctx, t)
	case Download:
		err = e.installDownload(ctx, t)
	default:
		err = fmt.Errorf("unknown kind of tool %q", t.Name)
	}
	if err != nil {
		return err
	}
	if !e.Installed(t) {
		// installed into a folder that is not on the PATH yet: look there too
		for _, dir := range e.ExtraPaths() {
			for _, name := range []string{t.Binary, t.Binary + ".cmd", t.Binary + ".exe"} {
				if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("%s was installed but %q is not on the PATH (add %s to it)", t.Name, t.Binary, e.BinDir())
	}
	return nil
}

func (e Env) installNPM(ctx context.Context, t Tool) error {
	if _, err := e.need(t, "npm (Node.js)", "npm"); err != nil {
		return err
	}
	args := []string{"install", "-g"}
	if e.GOOS != "windows" {
		args = append(args, "--prefix", filepath.Join(e.Home, ".local"))
	}
	return e.Run(ctx, nil, "npm", append(args, t.Packages...)...)
}

func (e Env) installGo(ctx context.Context, t Tool) error {
	if _, err := e.need(t, "Go", "go"); err != nil {
		return err
	}
	return e.Run(ctx, []string{"GOBIN=" + e.BinDir()}, "go", "install", t.Packages[0]+"@latest")
}

func (e Env) installPython(ctx context.Context, t Tool) error {
	if _, err := e.LookPath("pipx"); err == nil {
		return e.Run(ctx, nil, "pipx", "install", t.Packages[0])
	}
	python, err := e.need(t, "Python (pipx or pip)", "python3", "python", "py")
	if err != nil {
		return err
	}
	if err := e.Run(ctx, nil, python, "-m", "pip", "install", "--user", t.Packages[0]); err != nil {
		// a system Python that is managed by the distribution refuses pip; --user keeps the change in the home folder
		if retryErr := e.Run(ctx, nil, python, "-m", "pip", "install", "--user", "--break-system-packages", t.Packages[0]); retryErr != nil {
			return err
		}
	}
	return nil
}

// errNoTool is returned for a Download tool this package does not know.
var errNoTool = errors.New("no download recipe")

func (e Env) installDownload(ctx context.Context, t Tool) error {
	switch t.Name {
	case "jdtls":
		return e.installJdtls(ctx, t)
	case "groovy-language-server":
		return e.installGroovy(ctx, t)
	}
	return fmt.Errorf("%s: %w", t.Name, errNoTool)
}
