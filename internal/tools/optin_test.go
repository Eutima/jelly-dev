package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLanguages_CSharpAndPowerShellAreOptIn(t *testing.T) {
	for _, id := range []string{"csharp", "powershell"} {
		if got, err := ParseLanguages(id); err != nil || !slices.Equal(got, []string{id}) {
			t.Errorf("%s must be known when named: %v %v", id, got, err)
		}
		if all, _ := ParseLanguages("all"); slices.Contains(all, id) {
			t.Errorf("all must not include %s", id)
		}
		if slices.Contains(AllLanguages(), id) || slices.Contains(DefaultLanguages(), id) {
			t.Errorf("%s must not be installed unless asked for", id)
		}
	}
	if got := ToolsFor([]string{"csharp", "powershell"}); !slices.Equal(got, []string{"csharp-ls", "powershell-editor-services"}) {
		t.Errorf("tools of the opt-in languages: %v", got)
	}
}

func TestInstall_CSharpServerNeedsTheDotnetSdkAndUsesDotnetTool(t *testing.T) {
	_, env := newMachine(t)
	err := env.Install(context.Background(), tool("csharp-ls"))
	var missing *MissingRuntimeError
	if !errors.As(err, &missing) || !strings.Contains(missing.Needs, ".NET SDK") {
		t.Fatalf("without dotnet the error must name the .NET SDK: %v", err)
	}
	m, env := newMachine(t, "dotnet")
	m.installs["csharp-ls"] = "csharp-ls"
	if err := env.Install(context.Background(), tool("csharp-ls")); err != nil {
		t.Fatal(err)
	}
	want := "dotnet tool update --tool-path " + env.BinDir() + " csharp-ls"
	if len(m.ran) != 1 || m.ran[0] != want {
		t.Errorf("ran %v, want %q", m.ran, want)
	}
}

// psesArchive builds a zip and pins the installer's checksum to it (restored when the test ends).
func psesArchive(t *testing.T, files map[string]string) func(string) error {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	previous := psesSHA256
	psesSHA256 = hex.EncodeToString(sum[:])
	t.Cleanup(func() { psesSHA256 = previous })
	return func(dest string) error { return os.WriteFile(dest, buf.Bytes(), 0o644) }
}

func TestInstall_PowerShellServerUnpacksTheArchiveAndWritesALauncherForPwsh(t *testing.T) {
	_, env := newMachine(t)
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err == nil || !strings.Contains(err.Error(), "PowerShell 7") {
		t.Fatalf("without pwsh the error must name PowerShell 7: %v", err)
	}
	m, env := newMachine(t, "pwsh")
	m.archive = psesArchive(t, map[string]string{"PowerShellEditorServices/Start-EditorServices.ps1": "# start", "PowerShellEditorServices/x.psd1": "x"})
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.BinDir(), "powershell-editor-services"))
	if err != nil {
		t.Fatal(err)
	}
	launcher := string(data)
	for _, want := range []string{"exec 'pwsh'", "-NoProfile", "Start-EditorServices.ps1", "-Stdio", "-BundledModulesPath", "-SessionDetailsPath"} {
		if !strings.Contains(launcher, want) {
			t.Errorf("launcher lacks %q:\n%s", want, launcher)
		}
	}
	if !strings.HasPrefix(launcher, "#!/bin/sh\n") {
		t.Errorf("launcher is no shell script:\n%s", launcher)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir(), "powershell-editor-services", "logs")); err != nil {
		t.Errorf("the log folder must exist: %v", err)
	}
}

func TestInstall_PowerShellServerRefusesBadArchives(t *testing.T) {
	m, env := newMachine(t, "pwsh")
	m.archive = psesArchive(t, map[string]string{"../../evil": "x"})
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("an archive that escapes the folder must be refused: %v", err)
	}
	m, env = newMachine(t, "pwsh")
	m.archive = psesArchive(t, map[string]string{"readme.txt": "no script"})
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err == nil {
		t.Fatal("an archive without the start script must fail")
	}
	m, env = newMachine(t, "pwsh")
	m.archive = psesArchive(t, map[string]string{"PowerShellEditorServices/Start-EditorServices.ps1": "# start"})
	psesSHA256 = strings.Repeat("0", 64)
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("a download that differs from the pin must be refused: %v", err)
	}
	if !strings.Contains(psesURL, "/download/v"+psesVersion+"/") {
		t.Fatalf("the download must be a fixed release: %s", psesURL)
	}
}

func TestQuoting_KeepsPathsWithQuotesInOneWord(t *testing.T) {
	if got := psQuote("a'b"); got != "'a''b'" {
		t.Errorf("psQuote = %s", got)
	}
	if got := shQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shQuote = %s", got)
	}
	if got := cmdQuote(`a"b`); got != `"a""b"` {
		t.Errorf("cmdQuote = %s", got)
	}
}

func TestInstall_PowerShellFailedUpdateKeepsTheWorkingServer(t *testing.T) {
	m, env := newMachine(t, "pwsh")
	m.archive = psesArchive(t, map[string]string{"PowerShellEditorServices/Start-EditorServices.ps1": "# old"})
	if err := env.Install(context.Background(), tool("powershell-editor-services")); err != nil {
		t.Fatal(err)
	}
	m.archive = psesArchive(t, map[string]string{"readme.txt": "an update without the start script"})
	if err := env.Update(context.Background(), tool("powershell-editor-services")); err == nil {
		t.Fatal("a broken download must fail")
	}
	script := filepath.Join(env.DataDir(), "powershell-editor-services", "PowerShellEditorServices", "Start-EditorServices.ps1")
	if data, err := os.ReadFile(script); err != nil || string(data) != "# old" {
		t.Fatalf("the working server must stay: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir(), "powershell-editor-services.new")); err == nil {
		t.Error("the staging folder must be removed")
	}
	m.archive = psesArchive(t, map[string]string{"PowerShellEditorServices/Start-EditorServices.ps1": "# new"})
	if err := env.Update(context.Background(), tool("powershell-editor-services")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(script); string(data) != "# new" {
		t.Fatalf("a good update replaces the files: %q", data)
	}
}

func TestQuoting_PercentSignsInACmdLauncherAreWrittenTwice(t *testing.T) {
	if got := cmdQuote(`C:\100%\x`); got != `"C:\100%%\x"` {
		t.Errorf("cmdQuote = %s", got)
	}
}
