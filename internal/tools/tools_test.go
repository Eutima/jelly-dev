package tools

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// machine is a fake computer: which commands exist, which commands were run, which downloads were asked for.
type machine struct {
	t          *testing.T
	home       string
	have       map[string]bool
	ran        []string
	runEnv     [][]string
	fail       map[string]error // by command name
	fetched    []string
	archive    func(dest string) error
	components map[string]bool   // the components rustup has installed (a proxy file exists for every one of them)
	installs   map[string]string // a command that appears after running the key (like npm creating prettier)
}

func newMachine(t *testing.T, have ...string) (*machine, Env) {
	m := &machine{t: t, home: t.TempDir(), have: map[string]bool{}, components: map[string]bool{}, fail: map[string]error{}, installs: map[string]string{}}
	for _, name := range have {
		m.have[name] = true
	}
	env := Env{
		Home: m.home, GOOS: "linux", Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if m.have[name] {
				return "/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, extra []string, name string, args ...string) error {
			if name == "rustup" && len(args) == 2 && args[0] == "which" { // asks, installs nothing: is the component there?
				if m.components[args[1]] {
					return nil
				}
				return errors.New("unknown binary " + args[1] + " in the toolchain")
			}
			m.ran = append(m.ran, name+" "+strings.Join(args, " "))
			m.runEnv = append(m.runEnv, extra)
			if err := m.fail[name]; err != nil {
				return err
			}
			for key, binary := range m.installs {
				if strings.Contains(strings.Join(args, " "), key) {
					m.have[binary] = true
					m.components[binary] = true
				}
			}
			return nil
		},
		Fetch: func(_ context.Context, url, dest string) error {
			m.fetched = append(m.fetched, url)
			if m.archive != nil {
				return m.archive(dest)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, []byte("jar"), 0o644)
		},
	}
	return m, env
}

func tool(name string) Tool {
	for _, t := range Defaults() {
		if t.Name == name {
			return t
		}
	}
	panic("no tool " + name)
}

func TestDefaults_CoverServersAndFormatters(t *testing.T) {
	binaries := map[string]bool{}
	for _, tl := range Defaults() {
		binaries[tl.Binary] = true
	}
	for _, want := range []string{"gopls", "typescript-language-server", "pyright-langserver", "vscode-html-language-server", "ngserver",
		"jdtls", "groovy-language-server", "prettier", "black"} {
		if !binaries[want] {
			t.Errorf("%s is missing from the default tools", want)
		}
	}
}

func TestMissing_ListsToolsThatAreNotOnThePath(t *testing.T) {
	_, env := newMachine(t, "gopls", "prettier")
	var names []string
	for _, tl := range env.Missing(Defaults()) {
		names = append(names, tl.Name)
	}
	for _, name := range names {
		if name == "gopls" || name == "prettier" {
			t.Errorf("%s is installed", name)
		}
	}
	if len(names) != len(Defaults())-2 {
		t.Fatalf("missing %v", names)
	}
}

func TestInstall_NPMUsesPrefixBelowHome(t *testing.T) {
	m, env := newMachine(t, "npm")
	m.installs["prettier"] = "prettier"
	if err := env.Install(context.Background(), tool("prettier")); err != nil {
		t.Fatal(err)
	}
	want := "npm install -g --prefix " + filepath.Join(m.home, ".local") + " prettier"
	if len(m.ran) != 1 || m.ran[0] != want {
		t.Fatalf("ran %v, want %q", m.ran, want)
	}
	m, env = newMachine(t, "npm")
	m.installs["angular"] = "ngserver"
	if err := env.Install(context.Background(), tool("angular-language-server")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.ran[0], "@angular/language-server @angular/language-service typescript") {
		t.Fatalf("the Angular server needs its language service and TypeScript: %v", m.ran)
	}
}

func TestInstall_NPMOnWindowsHasNoPrefix(t *testing.T) {
	m, env := newMachine(t, "npm")
	env.GOOS = "windows"
	m.installs["prettier"] = "prettier"
	_ = env.Install(context.Background(), tool("prettier"))
	if m.ran[0] != "npm install -g prettier" {
		t.Fatalf("ran %v", m.ran)
	}
}

func TestInstall_GoToolsGoToBinDirOfHome(t *testing.T) {
	m, env := newMachine(t, "go")
	m.installs["gopls"] = "gopls"
	if err := env.Install(context.Background(), tool("gopls")); err != nil {
		t.Fatal(err)
	}
	if m.ran[0] != "go install golang.org/x/tools/gopls@latest" || !reflect.DeepEqual(m.runEnv[0], []string{"GOBIN=" + filepath.Join(m.home, ".local", "bin")}) {
		t.Fatalf("ran %v env %v", m.ran, m.runEnv)
	}
}

func TestInstall_PythonPrefersPipxThenPipAndRetriesForManagedSystems(t *testing.T) {
	m, env := newMachine(t, "pipx")
	m.installs["black"] = "black"
	if err := env.Install(context.Background(), tool("black")); err != nil || m.ran[0] != "pipx install black" {
		t.Fatalf("pipx: %v %v", err, m.ran)
	}
	m, env = newMachine(t, "python3")
	m.installs["black"] = "black"
	if err := env.Install(context.Background(), tool("black")); err != nil || m.ran[0] != "python3 -m pip install --user black" {
		t.Fatalf("pip: %v %v", err, m.ran)
	}
	m, env = newMachine(t, "python3")
	calls := 0
	env.Run = func(_ context.Context, _ []string, name string, args ...string) error {
		calls++
		m.ran = append(m.ran, strings.Join(args, " "))
		if !strings.Contains(strings.Join(args, " "), "--break-system-packages") {
			return errors.New("externally-managed-environment")
		}
		m.have["black"] = true
		return nil
	}
	if err := env.Install(context.Background(), tool("black")); err != nil || calls != 2 {
		t.Fatalf("a managed system Python needs the second try: %v, %d calls", err, calls)
	}
}

func TestInstall_ReportsMissingRuntimes(t *testing.T) {
	_, env := newMachine(t)
	for name, needs := range map[string]string{"prettier": "npm", "gopls": "Go", "black": "Python", "jdtls": "Java", "groovy-language-server": "Java"} {
		err := env.Install(context.Background(), tool(name))
		var missing *MissingRuntimeError
		if !errors.As(err, &missing) || !strings.Contains(missing.Needs, needs) || !strings.Contains(err.Error(), "installer installs it") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInstall_FailsWhenCommandFails(t *testing.T) {
	m, env := newMachine(t, "npm")
	m.fail["npm"] = errors.New("npm install: network down")
	if err := env.Install(context.Background(), tool("prettier")); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("got %v", err)
	}
}

func TestInstall_FailsWhenToolStillNotFoundAfterwards(t *testing.T) {
	_, env := newMachine(t, "npm") // the fake npm installs nothing
	err := env.Install(context.Background(), tool("prettier"))
	if err == nil || !strings.Contains(err.Error(), "not on the PATH") {
		t.Fatalf("got %v", err)
	}
}

func TestInstall_AcceptsToolsInFoldersThatAreNotOnThePathYet(t *testing.T) {
	m, env := newMachine(t, "npm")
	if err := os.MkdirAll(env.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	env.Run = func(context.Context, []string, string, ...string) error {
		return os.WriteFile(filepath.Join(env.BinDir(), "prettier"), []byte("#!/bin/sh"), 0o755)
	}
	if err := env.Install(context.Background(), tool("prettier")); err != nil {
		t.Fatalf("a tool in ~/.local/bin counts as installed: %v (ran %v)", err, m.ran)
	}
}

func TestInstall_GroovyDownloadsJarAndWritesLauncher(t *testing.T) {
	m, env := newMachine(t, "java")
	err := env.Install(context.Background(), tool("groovy-language-server"))
	launcher := filepath.Join(env.BinDir(), "groovy-language-server")
	data, readErr := os.ReadFile(launcher)
	if err != nil || readErr != nil || !strings.Contains(string(data), "java -jar") || !strings.Contains(string(data), "groovy-language-server-all.jar") {
		t.Fatalf("%v %v %q", err, readErr, data)
	}
	if len(m.fetched) != 1 || !strings.HasPrefix(m.fetched[0], "https://github.com/GroovyLanguageServer/") {
		t.Fatalf("fetched %v", m.fetched)
	}
	if info, _ := os.Stat(launcher); info.Mode()&0o100 == 0 {
		t.Fatal("the launcher must be executable")
	}
}

func jdtlsArchive(files map[string]string) func(string) error {
	return func(dest string) error {
		file, err := os.Create(dest)
		if err != nil {
			return err
		}
		defer file.Close()
		zw := gzip.NewWriter(file)
		defer zw.Close()
		tw := tar.NewWriter(zw)
		defer tw.Close()
		for name, content := range files {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
				return err
			}
			if _, err := tw.Write([]byte(content)); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestInstall_JdtlsUnpacksArchiveAndWritesLauncher(t *testing.T) {
	m, env := newMachine(t, "java", "python3")
	m.archive = jdtlsArchive(map[string]string{"bin/jdtls": "#!/usr/bin/env python3\n", "plugins/x.jar": "jar"})
	if err := env.Install(context.Background(), tool("jdtls")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir(), "jdtls", "plugins", "x.jar")); err != nil {
		t.Fatalf("the archive must be unpacked: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(env.BinDir(), "jdtls"))
	if !strings.Contains(string(data), "python3") || !strings.Contains(string(data), filepath.Join("jdtls", "bin", "jdtls")) {
		t.Fatalf("launcher %q", data)
	}
	m, env = newMachine(t, "java", "python3")
	m.archive = jdtlsArchive(map[string]string{"readme.txt": "no script"})
	if err := env.Install(context.Background(), tool("jdtls")); err == nil {
		t.Fatal("an archive without the start script must fail")
	}
	m, env = newMachine(t, "java", "python3")
	m.archive = jdtlsArchive(map[string]string{"../../evil": "x"})
	if err := env.Install(context.Background(), tool("jdtls")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("an archive that escapes the folder must be refused: %v", err)
	}
}

func TestInstall_WindowsLauncherIsACmdFile(t *testing.T) {
	_, env := newMachine(t, "java")
	env.GOOS = "windows"
	if err := env.Install(context.Background(), tool("groovy-language-server")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.BinDir(), "groovy-language-server.cmd")); err != nil {
		t.Fatalf("a .cmd launcher is expected: %v", err)
	}
}

func TestExtraPaths_CoverBinFolders(t *testing.T) {
	_, env := newMachine(t)
	env.Getenv = func(key string) string { return map[string]string{"GOPATH": "/gp", "GOBIN": "/gb"}[key] }
	paths := env.ExtraPaths()
	for _, want := range []string{filepath.Join(env.Home, ".local", "bin"), "/gb", filepath.Join("/gp", "bin")} {
		found := false
		for _, p := range paths {
			found = found || p == want
		}
		if !found {
			t.Errorf("%s missing from %v", want, paths)
		}
	}
}

func TestInstaller_RemembersFailuresAndRetriesLater(t *testing.T) {
	m, env := newMachine(t, "npm")
	m.fail["npm"] = errors.New("offline")
	installer := &Installer{env: env, tools: []Tool{tool("prettier"), tool("pyright")}, path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	installer.now = func() time.Time { return clock }
	if got := installer.Pending(false); !reflect.DeepEqual(got, []string{"prettier", "pyright"}) {
		t.Fatalf("pending %v", got)
	}
	if err := installer.Install(context.Background(), "prettier"); err == nil {
		t.Fatal("the failure must be reported")
	}
	if got := installer.Pending(false); !reflect.DeepEqual(got, []string{"pyright"}) {
		t.Fatalf("a tool that just failed is not tried again at once: %v", got)
	}
	if got := installer.Pending(true); len(got) != 2 {
		t.Fatalf("force tries everything: %v", got)
	}
	clock = clock.Add(RetryAfter + time.Minute)
	if got := installer.Pending(false); len(got) != 2 {
		t.Fatalf("after a day the tool is tried again: %v", got)
	}
	delete(m.fail, "npm")
	m.installs["prettier"] = "prettier"
	if err := installer.Install(context.Background(), "prettier"); err != nil {
		t.Fatal(err)
	}
	if status := strings.Join(installer.Status(), "\n"); !strings.Contains(status, "ok       prettier") || !strings.Contains(status, "missing  pyright") {
		t.Fatalf("status:\n%s", status)
	}
	if err := installer.Install(context.Background(), "nothing"); err == nil {
		t.Fatal("an unknown tool is an error")
	}
	if installer.Purpose("prettier") == "" || installer.Purpose("nothing") != "" {
		t.Fatal("purpose")
	}
}

func TestInstaller_StatusShowsWhyAToolFailed(t *testing.T) {
	m, env := newMachine(t, "npm")
	m.fail["npm"] = errors.New("npm install: E404 not found")
	installer := &Installer{env: env, tools: []Tool{tool("pyright")}, path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	_ = installer.Install(context.Background(), "pyright")
	if status := strings.Join(installer.Status(), "\n"); !strings.Contains(status, "E404") {
		t.Fatalf("status:\n%s", status)
	}
}
