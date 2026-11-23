package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall_DebugpyNeedsPythonBuildsAnEnvironmentAndWritesALauncher(t *testing.T) {
	_, env := newMachine(t)
	err := env.Install(context.Background(), tool("debugpy"))
	var missing *MissingRuntimeError
	if !errors.As(err, &missing) || !strings.Contains(missing.Needs, "Python 3") {
		t.Fatalf("without Python the error must name it: %v", err)
	}
	m, env := newMachine(t, "python3")
	if err := env.Install(context.Background(), tool("debugpy")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.DataDir(), "debugpy")
	wantVenv := "python3 -m venv " + target
	wantPip := filepath.Join(target, "bin", "python") + " -m pip install --disable-pip-version-check debugpy==" + debugpyVersion
	if len(m.ran) != 2 || m.ran[0] != wantVenv || m.ran[1] != wantPip {
		t.Fatalf("ran %q, want %q then %q", m.ran, wantVenv, wantPip)
	}
	data, err := os.ReadFile(filepath.Join(env.BinDir(), "debugpy-adapter"))
	if err != nil || !strings.Contains(string(data), "-m debugpy.adapter \"$@\"") || !strings.Contains(string(data), filepath.Join(target, "bin", "python")) {
		t.Fatalf("launcher: %q %v", data, err)
	}
	if info, _ := os.Stat(filepath.Join(env.BinDir(), "debugpy-adapter")); info == nil || info.Mode()&0o100 == 0 {
		t.Fatal("the launcher must be executable")
	}
}

func TestInstall_DebugpyOnWindowsUsesTheScriptsFolderAndACmdLauncher(t *testing.T) {
	_, env := newMachine(t, "python")
	env.GOOS = "windows"
	if err := env.Install(context.Background(), tool("debugpy")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.BinDir(), "debugpy-adapter.cmd"))
	if err != nil || !strings.Contains(string(data), filepath.Join("debugpy", "Scripts", "python.exe")) || !strings.Contains(string(data), "-m debugpy.adapter %*") {
		t.Fatalf("launcher: %q %v", data, err)
	}
}

func TestInstall_DebugpyKeepsTheWorkingInstallWhenAnUpdateFails(t *testing.T) {
	m, env := newMachine(t, "python3")
	target := filepath.Join(env.DataDir(), "debugpy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "old-install")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.fail["python3"] = errors.New("No module named venv")
	err := env.Update(context.Background(), tool("debugpy"))
	if err == nil || !strings.Contains(err.Error(), "python3-venv") {
		t.Fatalf("the error says what is missing: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the old install must be put back")
	}
	if _, err := os.Stat(target + ".old"); err == nil {
		t.Fatal("no .old folder is left behind")
	}
	delete(m.fail, "python3")
	m.fail[filepath.Join(target, "bin", "python")] = errors.New("no network")
	if err := env.Update(context.Background(), tool("debugpy")); err == nil || !strings.Contains(err.Error(), "no network") {
		t.Fatalf("a failing pip is reported: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the old install is put back after a failed pip as well")
	}
}
