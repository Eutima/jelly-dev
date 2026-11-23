package tools

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLanguageStates_ShowMissingToolsAndTheChosenLanguages(t *testing.T) {
	_, env := newMachine(t, "pyright-langserver") // pyright is there, black is not
	dir := t.TempDir()
	if err := SaveLanguages(dir, []string{"python"}); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(dir)
	installer.env = env
	states := map[string]LanguageState{}
	for _, s := range installer.LanguageStates() {
		states[s.ID] = s
	}
	if len(states) != len(Languages()) {
		t.Fatalf("every language is listed: %d", len(states))
	}
	python := states["python"]
	if !python.Chosen || !reflect.DeepEqual(python.Tools, []string{"pyright", "black", "debugpy"}) || !reflect.DeepEqual(python.Missing, []string{"black", "debugpy"}) {
		t.Fatalf("python is chosen and misses its formatter and its debugger: %+v", python)
	}
	if go1 := states["go"]; go1.Chosen || !reflect.DeepEqual(go1.Missing, []string{"gopls", "delve"}) {
		t.Fatalf("go is not chosen and misses gopls and delve: %+v", go1)
	}
}

func TestAddLanguage_ChoosesTheLanguageAndInstallsItsServerAndFormatter(t *testing.T) {
	m, env := newMachine(t, "pipx", "npm", "python3")
	m.installs["pyright"] = "pyright-langserver"
	m.installs["black"] = "black"
	m.installs["debugpy"] = "debugpy-adapter"
	dir := t.TempDir()
	installer := NewInstaller(dir)
	installer.env = env
	results, err := installer.AddLanguage(context.Background(), "go")
	var missingRuntime *MissingRuntimeError
	if err != nil || len(results) != 2 || !errors.As(results[0].Err, &missingRuntime) || !errors.As(results[1].Err, &missingRuntime) { // gopls and delve need go, which this machine lacks
		t.Fatalf("gopls cannot be installed without Go: %v %+v", err, results)
	}
	if saved, ok := LoadLanguages(dir); !ok || !strings.Contains(strings.Join(saved, ","), "go") || !strings.Contains(strings.Join(saved, ","), "python") {
		t.Fatalf("the language is added to the chosen ones, the defaults stay: %v %v", saved, ok)
	}
	if !installer.wanted("gopls") {
		t.Fatal("the IDE keeps the tools of the added language installed")
	}
	// a language whose tools are installed already
	results, err = installer.AddLanguage(context.Background(), "python")
	if err != nil {
		t.Fatal(err)
	}
	var installed []string
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Tool, r.Err)
		}
		if !r.Already {
			installed = append(installed, r.Tool)
		}
	}
	if !reflect.DeepEqual(installed, []string{"pyright", "debugpy", "black"}) {
		t.Fatalf("the server and the formatter are installed: %v (%+v)", installed, results)
	}
	again, _ := installer.AddLanguage(context.Background(), "python")
	for _, r := range again {
		if !r.Already {
			t.Fatalf("a second time nothing is installed: %+v", again)
		}
	}
}

func TestAddLanguage_RefusesUnknownOrSeveralLanguages(t *testing.T) {
	installer := &Installer{env: Env{}, tools: Defaults(), path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	if _, err := installer.AddLanguage(context.Background(), "cobol"); err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Fatalf("an unknown language is an error: %v", err)
	}
	if _, err := installer.AddLanguage(context.Background(), "go,python"); err == nil {
		t.Fatal("one language at a time")
	}
	if _, err := installer.AddLanguage(context.Background(), ""); err == nil {
		t.Fatal("no language is an error")
	}
}

func TestLanguages_CCppAndRustHaveTheirServersAndFormatters(t *testing.T) {
	for id, want := range map[string][]string{"c": {"clangd", "clang-format"}, "cpp": {"clangd", "clang-format"}, "rust": {"rust-analyzer", "rustfmt"}} {
		got := ToolsFor([]string{id})
		if len(got) != 2 {
			t.Errorf("%s: %v", id, got)
		}
		for _, name := range want {
			found := false
			for _, g := range got {
				found = found || g == name
			}
			if !found {
				t.Errorf("%s lacks %s: %v", id, name, got)
			}
		}
	}
	if got := ToolsFor([]string{"c", "cpp"}); len(got) != 2 {
		t.Errorf("C and C++ share clangd and clang-format: %v", got)
	}
}

func TestInstall_RustToolsUseRustupAndClangdIsManual(t *testing.T) {
	m, env := newMachine(t, "rustup")
	m.installs["rust-analyzer"] = "rust-analyzer"
	if err := env.Install(context.Background(), tool("rust-analyzer")); err != nil {
		t.Fatalf("rust-analyzer: %v (%v)", err, m.ran)
	}
	if len(m.ran) != 1 || m.ran[0] != "rustup component add rust-analyzer" {
		t.Fatalf("rustup adds the component: %v", m.ran)
	}
	// no toolchain yet: the first try fails, the stable toolchain is installed, the second try works
	m2, env2 := newMachine(t, "rustup")
	m2.installs["rustfmt"] = "rustfmt"
	calls := 0
	run := env2.Run
	env2.Run = func(ctx context.Context, extra []string, name string, args ...string) error {
		calls++
		if calls == 1 {
			return errors.New("rustup could not choose a version of rustfmt: no default toolchain")
		}
		return run(ctx, extra, name, args...)
	}
	if err := env2.Install(context.Background(), tool("rustfmt")); err != nil {
		t.Fatalf("rustfmt without a toolchain: %v", err)
	}
	if strings.Join(m2.ran, "|") != "rustup default stable|rustup component add rustfmt" {
		t.Fatalf("the stable toolchain is installed first: %v", m2.ran)
	}
	// no rustup: the missing runtime is named
	_, env3 := newMachine(t)
	var missing *MissingRuntimeError
	if err := env3.Install(context.Background(), tool("rustfmt")); !errors.As(err, &missing) {
		t.Fatalf("without rustup the runtime is missing: %v", err)
	}
	// clangd comes from the package manager: jd says how
	var manual *ManualInstallError
	err := env3.Install(context.Background(), tool("clangd"))
	if !errors.As(err, &manual) || !strings.Contains(err.Error(), "apt install clangd") {
		t.Fatalf("clangd needs the package manager: %v", err)
	}
	// clang-format is a Python wheel: installed without root
	m4, env4 := newMachine(t, "pipx")
	m4.installs["clang-format"] = "clang-format"
	if err := env4.Install(context.Background(), tool("clang-format")); err != nil || !strings.Contains(strings.Join(m4.ran, "|"), "pipx install clang-format") {
		t.Fatalf("clang-format with pipx: %v %v", err, m4.ran)
	}
	if !slices.Contains(env.ExtraPaths(), filepath.Join(env.Home, ".cargo", "bin")) {
		t.Fatalf("rustup's folder is searched for the tools: %v", env.ExtraPaths())
	}
}

// rustup keeps a proxy for rust-analyzer and rustfmt in ~/.cargo/bin whether the component is installed or not: only rustup knows.
func TestInstalled_ARustupProxyWithoutTheComponentIsNotInstalled(t *testing.T) {
	m, env := newMachine(t, "rustup", "rust-analyzer", "rustfmt") // the proxies are on the PATH
	if env.Installed(tool("rust-analyzer")) || env.Installed(tool("rustfmt")) {
		t.Fatal("a proxy file is no proof: the components are not installed")
	}
	m.components["rust-analyzer"] = true
	if !env.Installed(tool("rust-analyzer")) || env.Installed(tool("rustfmt")) {
		t.Fatal("rustup says which component is there")
	}
	_, none := newMachine(t) // no rustup at all
	if none.Installed(tool("rustfmt")) {
		t.Fatal("without rustup nothing of Rust is installed")
	}
	installer := &Installer{env: env, tools: Defaults(), path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	if got := installer.Pending(true); !slices.Contains(got, "rustfmt") || slices.Contains(got, "rust-analyzer") {
		t.Fatalf("the missing component is pending, the installed one is not: %v", got)
	}
}
