// Package tools installs the language servers and formatters the editor uses. jd checks for them when it starts and installs
// what is missing, with the language's own tool: npm, go, pip or pipx, or a download. It does not install runtimes (Go, Node.js,
// Python, Java) itself: that needs root rights or a pinned download and is the installer script's job. The program that uses this
// package may give Env an InstallRuntime hook that runs the script for Node.js (needed by the npm tools); without it, or for the
// other runtimes, a missing runtime is reported.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	// Rustup tools are installed with rustup component add.
	Rustup
	// System tools come from the package manager of the machine and need root rights: jd only says how (the installer does it).
	System
)

// Tool is a language server or formatter.
type Tool struct {
	Name     string // shown to the user
	Binary   string // the command that shows that it is installed
	Kind     Kind
	Packages []string // npm packages, the Go package path, the pip package
	Purpose  string   // what it is for, e.g. "language server for Go"
	Hint     string   // how to install a System tool by hand
}

// ManualInstallError is returned for a tool that jd cannot install (or update) without root rights.
type ManualInstallError struct {
	Tool, Hint string
	Update     bool // the tool is installed, it is its update that has to be done by hand
}

func (e *ManualInstallError) Error() string {
	if e.Update {
		return fmt.Sprintf("%s has to be updated with your package manager: %s", e.Tool, e.Hint)
	}
	return fmt.Sprintf("%s has to be installed with your package manager: %s", e.Tool, e.Hint)
}

// Defaults are the tools jd installs: the servers of lsp.DefaultServers and their formatters.
func Defaults() []Tool {
	return []Tool{
		{Name: "gopls", Binary: "gopls", Kind: Go, Packages: []string{"golang.org/x/tools/gopls"}, Purpose: "language server for Go"},
		{Name: "typescript-language-server", Binary: "typescript-language-server", Kind: NPM,
			Packages: []string{"typescript@5", "typescript-language-server"}, Purpose: "language server for JavaScript and TypeScript"},
		{Name: "pyright", Binary: "pyright-langserver", Kind: NPM, Packages: []string{"pyright"}, Purpose: "language server for Python"},
		{Name: "vscode-langservers-extracted", Binary: "vscode-html-language-server", Kind: NPM,
			Packages: []string{"vscode-langservers-extracted"}, Purpose: "language servers for HTML, CSS and JSON"},
		{Name: "angular-language-server", Binary: "ngserver", Kind: NPM,
			Packages: []string{"@angular/language-server", "@angular/language-service", "typescript@5"}, Purpose: "language server for Angular templates"},
		{Name: "yaml-language-server", Binary: "yaml-language-server", Kind: NPM, Packages: []string{"yaml-language-server"}, Purpose: "language server for YAML"},
		{Name: "dockerfile-language-server", Binary: "docker-langserver", Kind: NPM,
			Packages: []string{"dockerfile-language-server-nodejs"}, Purpose: "language server for Dockerfiles"},
		{Name: "compose-language-service", Binary: "docker-compose-langserver", Kind: NPM,
			Packages: []string{"@microsoft/compose-language-service"}, Purpose: "language server for Docker Compose files"},
		{Name: "clangd", Binary: "clangd", Kind: System, Purpose: "language server for C and C++",
			Hint: "apt install clangd, dnf install clang-tools-extra, apk add clang-extra-tools, pacman -S clang, brew install llvm; or run jd setup"},
		{Name: "clang-format", Binary: "clang-format", Kind: Python, Packages: []string{"clang-format"}, Purpose: "formatter for C and C++"},
		{Name: "rust-analyzer", Binary: "rust-analyzer", Kind: Rustup, Packages: []string{"rust-analyzer"}, Purpose: "language server for Rust"},
		{Name: "rustfmt", Binary: "rustfmt", Kind: Rustup, Packages: []string{"rustfmt"}, Purpose: "formatter for Rust"},
		{Name: "jdtls", Binary: "jdtls", Kind: Download, Purpose: "language server for Java"},
		{Name: "prettier", Binary: "prettier", Kind: NPM, Packages: []string{"prettier"}, Purpose: "formatter for HTML, CSS, JavaScript, TypeScript, JSON and YAML"},
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
	// InstallRuntime installs a runtime that a tool needs ("node") when it is missing; nil: a missing runtime is only reported. It
	// returns when the runtime can be found on the PATH of this process.
	InstallRuntime func(ctx context.Context, runtime string) error
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
	paths := []string{e.BinDir(), filepath.Join(e.Home, ".cargo", "bin")}
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
	if t.Kind == Rustup {
		// rustup puts a proxy for rust-analyzer and rustfmt into ~/.cargo/bin whether the component is installed or not, so the
		// file on the PATH proves nothing: rustup says whether the toolchain has it
		if _, err := e.LookPath("rustup"); err != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return e.Run(ctx, nil, "rustup", "which", t.Binary) == nil
	}
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
	case Rustup:
		err = e.installRustup(ctx, t)
	case System:
		err = &ManualInstallError{Tool: t.Name, Hint: t.Hint}
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
		var missing *MissingRuntimeError
		if !errors.As(err, &missing) || e.InstallRuntime == nil {
			return err
		}
		if installErr := e.InstallRuntime(ctx, "node"); installErr != nil {
			return fmt.Errorf("%s needs Node.js, which is missing and could not be installed: %w", t.Name, installErr)
		}
		if _, err := e.need(t, "npm (Node.js)", "npm"); err != nil {
			return err
		}
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
	}
	return fmt.Errorf("%s: %w", t.Name, errNoTool)
}

// installRustup installs a component of the Rust toolchain; without a toolchain the stable one is installed first.
func (e Env) installRustup(ctx context.Context, t Tool) error {
	if _, err := e.need(t, "rustup (the Rust toolchain)", "rustup"); err != nil {
		return err
	}
	add := append([]string{"component", "add"}, t.Packages...)
	if err := e.Run(ctx, nil, "rustup", add...); err != nil {
		if toolchainErr := e.Run(ctx, nil, "rustup", "default", "stable"); toolchainErr != nil {
			return err
		}
		return e.Run(ctx, nil, "rustup", add...)
	}
	return nil
}

// Update brings an installed tool to its newest version, with the tool that installed it: npm and go install fetch the latest
// one again, pipx and pip upgrade, rustup updates the toolchain, the downloads are fetched again. A tool of the package manager
// of the machine (clangd) cannot be updated without root rights: the error says how.
func (e Env) Update(ctx context.Context, t Tool) error {
	switch t.Kind {
	case NPM:
		return e.installNPM(ctx, t) // npm install -g takes the newest version of a package that is installed already
	case Go:
		return e.installGo(ctx, t) // @latest
	case Python:
		return e.updatePython(ctx, t)
	case Download:
		return e.installDownload(ctx, t)
	case Rustup:
		return e.updateRustup(ctx, t)
	case System:
		return &ManualInstallError{Tool: t.Name, Hint: t.Hint, Update: true}
	}
	return fmt.Errorf("unknown kind of tool %q", t.Name)
}

func (e Env) updatePython(ctx context.Context, t Tool) error {
	if _, err := e.LookPath("pipx"); err == nil {
		err := e.Run(ctx, nil, "pipx", "upgrade", t.Packages[0])
		if err != nil && strings.Contains(err.Error(), "not installed") {
			// installed another way (pip): pipx installs it, the newest version (any other failure, the network, is reported as it is)
			return e.Run(ctx, nil, "pipx", "install", "--force", t.Packages[0])
		}
		return err
	}
	python, err := e.need(t, "Python (pipx or pip)", "python3", "python", "py")
	if err != nil {
		return err
	}
	args := []string{"-m", "pip", "install", "--user", "--upgrade", t.Packages[0]}
	if err := e.Run(ctx, nil, python, args...); err != nil {
		// a system Python that is managed by the distribution refuses pip; --user keeps the change in the home folder
		if retryErr := e.Run(ctx, nil, python, append(args, "--break-system-packages")...); retryErr != nil {
			return err
		}
	}
	return nil
}

// updateRustup updates the toolchain, with its components (rust-analyzer, rustfmt).
func (e Env) updateRustup(ctx context.Context, t Tool) error {
	if _, err := e.need(t, "rustup (the Rust toolchain)", "rustup"); err != nil {
		return err
	}
	return e.Run(ctx, nil, "rustup", "update")
}
