package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpdate_EachKindOfToolIsUpdatedWithItsOwnTool(t *testing.T) {
	tests := []struct {
		name  string
		have  []string
		tool  string
		wants string // the command that ran
	}{
		{"npm", []string{"npm"}, "prettier", "npm install -g --prefix"},
		{"go install", []string{"go"}, "gopls", "go install golang.org/x/tools/gopls@latest"},
		{"pipx upgrade", []string{"pipx"}, "black", "pipx upgrade black"},
		{"pip", []string{"python3"}, "black", "python3 -m pip install --user --upgrade black"},
		{"rustup", []string{"rustup"}, "rustfmt", "rustup update"},
	}
	for _, tt := range tests {
		m, env := newMachine(t, tt.have...)
		if err := env.Update(context.Background(), tool(tt.tool)); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if len(m.ran) != 1 || !strings.Contains(m.ran[0], tt.wants) {
			t.Errorf("%s: ran %v, want %q", tt.name, m.ran, tt.wants)
		}
	}
}

func TestUpdate_PythonToolInstalledWithPipIsReinstalledByPipx_AndMissingRuntimesAndSystemToolsAreNamed(t *testing.T) {
	m, env := newMachine(t, "pipx")
	calls := 0
	run := env.Run
	env.Run = func(ctx context.Context, extra []string, name string, args ...string) error {
		calls++
		if calls == 1 { // pipx upgrade: black was not installed with pipx
			return errors.New("Package 'black' is not installed")
		}
		return run(ctx, extra, name, args...)
	}
	if err := env.Update(context.Background(), tool("black")); err != nil {
		t.Fatal(err)
	}
	if len(m.ran) != 1 || m.ran[0] != "pipx install --force black" {
		t.Fatalf("pipx installs it again: %v", m.ran)
	}
	_, none := newMachine(t)
	var missing *MissingRuntimeError
	for _, name := range []string{"prettier", "gopls", "black", "rustfmt"} {
		if err := none.Update(context.Background(), tool(name)); !errors.As(err, &missing) {
			t.Errorf("%s without its runtime: %v", name, err)
		}
	}
	var manual *ManualInstallError
	err := none.Update(context.Background(), tool("clangd"))
	if !errors.As(err, &manual) || !manual.Update || !strings.Contains(err.Error(), "has to be updated with your package manager") {
		t.Fatalf("clangd is updated with the package manager: %v", err)
	}
	if err := none.Install(context.Background(), tool("clangd")); !errors.As(err, &manual) || manual.Update || !strings.Contains(err.Error(), "has to be installed") {
		t.Fatalf("and installed with it: %v", err)
	}
}

func TestUpdateLanguage_UpdatesWhatIsInstalledInstallsWhatIsMissingAndChoosesTheLanguage(t *testing.T) {
	m, env := newMachine(t, "npm", "pipx", "pyright-langserver") // pyright is installed, black is not
	m.components["pyright-langserver"] = true
	m.installs["black"] = "black"
	dir := t.TempDir()
	installer := NewInstaller(dir)
	installer.env = env
	results, err := installer.UpdateLanguage(context.Background(), "python")
	if err != nil {
		t.Fatal(err)
	}
	byTool := map[string]InstallResult{}
	for _, r := range results {
		byTool[r.Tool] = r
	}
	if !byTool["pyright"].Updated || byTool["pyright"].Err != nil {
		t.Fatalf("the installed server is updated: %+v", results)
	}
	if byTool["black"].Updated || byTool["black"].Already || byTool["black"].Err != nil {
		t.Fatalf("the missing formatter is installed, not updated: %+v", results)
	}
	ran := strings.Join(m.ran, "|")
	if !strings.Contains(ran, "npm install -g") || !strings.Contains(ran, "pipx install black") || strings.Contains(ran, "pipx upgrade") {
		t.Fatalf("update with npm, install with pipx: %v", m.ran)
	}
	if saved, ok := LoadLanguages(dir); !ok || !strings.Contains(strings.Join(saved, ","), "python") {
		t.Fatalf("the language is chosen: %v %v", saved, ok)
	}
	if _, err := installer.UpdateLanguage(context.Background(), "go,python"); err == nil {
		t.Fatal("one language at a time")
	}
	if _, err := installer.UpdateLanguage(context.Background(), "cobol"); err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Fatalf("an unknown language is an error: %v", err)
	}
}

func TestUpdateLanguages_UpdatesSharedToolsOnceAndTellsTheProgress(t *testing.T) {
	m, env := newMachine(t, "npm", "vscode-html-language-server", "prettier")
	m.components["vscode-html-language-server"] = true
	installer := &Installer{env: env, tools: Defaults(), path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	var told []string
	results, err := installer.UpdateLanguages(context.Background(), []string{"html", "css", "json"}, func(tool string, done bool, err error) {
		told = append(told, tool+map[bool]string{false: " ...", true: " done"}[done])
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 { // the HTML, CSS and JSON servers are one package, and prettier is shared
		t.Fatalf("shared tools are updated once: %+v", results)
	}
	if !reflect.DeepEqual(told, []string{"vscode-langservers-extracted ...", "vscode-langservers-extracted done", "prettier ...", "prettier done"}) {
		t.Fatalf("progress: %v", told)
	}
	if _, err := installer.UpdateLanguages(context.Background(), nil, nil); !errors.Is(err, ErrNoLanguages) {
		t.Fatalf("no language is an error: %v", err)
	}
}

func TestInstallJdtls_AnUpdateRemovesTheOldFilesFirst(t *testing.T) {
	m, env := newMachine(t, "java", "python3")
	m.archive = jdtlsArchive(map[string]string{"bin/jdtls": "#!/usr/bin/env python3\n", "plugins/launcher_new.jar": "new"})
	stale := filepath.Join(env.DataDir(), "jdtls", "plugins", "launcher_old.jar")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.Update(context.Background(), tool("jdtls")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("the old launcher jar must not stay next to the new one")
	}
	if _, err := os.Stat(filepath.Join(env.DataDir(), "jdtls", "plugins", "launcher_new.jar")); err != nil {
		t.Fatalf("the new files are there: %v", err)
	}
}

func TestUpdatePython_PipxFallsBackOnlyWhenThePackageIsNotInstalledWithPipx(t *testing.T) {
	_, env := newMachine(t, "pipx")
	var ran []string
	env.Run = func(_ context.Context, _ []string, name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "upgrade" {
			return errors.New("pipx upgrade: could not reach pypi.org")
		}
		return nil
	}
	err := env.Update(context.Background(), tool("black"))
	if err == nil || !strings.Contains(err.Error(), "could not reach pypi.org") {
		t.Fatalf("a network failure is reported as it is: %v", err)
	}
	if len(ran) != 1 {
		t.Fatalf("and not retried as an installation: %v", ran)
	}
}

func TestUpdateLanguages_RustupUpdatesTheToolchainOnceAndTheResultIsRemembered(t *testing.T) {
	m, env := newMachine(t, "rustup")
	m.components["rust-analyzer"], m.components["rustfmt"] = true, true
	m.fail["rustup"] = nil
	dir := t.TempDir()
	installer := &Installer{env: env, tools: Defaults(), path: filepath.Join(dir, "tools.json"), now: time.Now}
	results, err := installer.UpdateLanguages(context.Background(), []string{"rust"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updates := 0
	for _, r := range m.ran {
		if r == "rustup update" {
			updates++
		}
	}
	if updates != 1 || len(results) != 2 || !results[0].Updated || !results[1].Updated {
		t.Fatalf("one rustup update for rust-analyzer and rustfmt: %d %+v", updates, results)
	}
	// a failure is remembered in the failures file, a later success clears it
	m.fail["rustup"] = errors.New("offline")
	if _, err := installer.UpdateLanguages(context.Background(), []string{"rust"}, nil); err != nil {
		t.Fatal(err)
	}
	if status := strings.Join(installer.Status(), "\n"); !strings.Contains(status, "update: offline") {
		t.Fatalf("a failed update shows in the status:\n%s", status)
	}
	delete(m.fail, "rustup")
	if _, err := installer.UpdateLanguages(context.Background(), []string{"rust"}, nil); err != nil {
		t.Fatal(err)
	}
	if status := strings.Join(installer.Status(), "\n"); strings.Contains(status, "update: offline") {
		t.Fatalf("a successful update clears the failure:\n%s", status)
	}
}
