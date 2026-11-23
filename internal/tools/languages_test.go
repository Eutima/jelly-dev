package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseLanguages_KeepsTheCatalogOrderAndKnowsAll(t *testing.T) {
	got, err := ParseLanguages("css, Python;html  python")
	if err != nil || !reflect.DeepEqual(got, []string{"python", "html", "css"}) {
		t.Fatalf("order of the catalog, no doubles: %v %v", got, err)
	}
	if all, _ := ParseLanguages("all"); len(all) != len(Languages())-3 || slices.Contains(all, "csharp") || slices.Contains(all, "powershell") || slices.Contains(all, "debug") {
		t.Fatalf("all names every language but the opt-in ones (debug, C#, PowerShell): %v", all)
	}
	if none, err := ParseLanguages("NONE"); err != nil || len(none) != 0 || none == nil {
		t.Fatalf("none names no language, in any case: %#v %v", none, err)
	}
	if none, err := ParseLanguages(""); err != nil || len(none) != 0 {
		t.Fatalf("an empty list is no language: %v %v", none, err)
	}
	if _, err := ParseLanguages("python,cobol"); err == nil || !strings.Contains(err.Error(), `unknown language "cobol"`) || !strings.Contains(err.Error(), "python") {
		t.Fatalf("an unknown language names the known ones: %v", err)
	}
}

func TestToolsFor_LanguageServersAndFormattersOnceEach(t *testing.T) {
	got := ToolsFor(DefaultLanguages())
	want := []string{"pyright", "vscode-langservers-extracted", "yaml-language-server", "dockerfile-language-server", "compose-language-service", "prettier", "black"} // the order of Defaults() is the order of the tools
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the default languages need %v, got %v", want, got)
	}
	if ToolsFor(nil) != nil {
		t.Fatal("no language needs no tool")
	}
	for _, l := range Languages() { // every tool named by a language exists
		for _, name := range l.Tools {
			if tool(name).Name != name {
				t.Errorf("%s names the unknown tool %s", l.ID, name)
			}
		}
	}
	if !reflect.DeepEqual(DefaultLanguages(), []string{"python", "html", "css", "json", "yaml", "docker", "compose"}) {
		t.Fatalf("the defaults are python, html, css, json, yaml, docker and compose: %v", DefaultLanguages())
	}
}

func TestLanguages_AreSavedAndLoadedAndAnEmptyChoiceCounts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if _, ok := LoadLanguages(dir); ok {
		t.Fatal("nothing is chosen before it is saved")
	}
	if err := SaveLanguages(dir, []string{"go", "python"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadLanguages(dir); !ok || !reflect.DeepEqual(got, []string{"python", "go"}) {
		t.Fatalf("saved languages come back in catalog order: %v %v", got, ok)
	}
	if err := SaveLanguages(dir, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadLanguages(dir); !ok || len(got) != 0 {
		t.Fatalf("choosing no language is a choice: %v %v", got, ok)
	}
	if err := SaveLanguages(dir, []string{"cobol"}); err == nil {
		t.Fatal("an unknown language is refused")
	}
	if err := os.WriteFile(filepath.Join(dir, languagesFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadLanguages(dir); ok {
		t.Fatal("a broken file counts as no choice")
	}
}

func TestInstaller_OnlyInstallsToolsOfTheChosenLanguages(t *testing.T) {
	_, env := newMachine(t) // nothing installed
	dir := t.TempDir()
	if err := SaveLanguages(dir, []string{"python"}); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(dir)
	installer.env = env
	if got := installer.Pending(true); !reflect.DeepEqual(got, []string{"black", "pyright"}) {
		t.Fatalf("python needs its server and its formatter, nothing else: %v", got)
	}
	status := strings.Join(installer.Status(), "\n")
	if strings.Contains(status, "gopls") || strings.Contains(status, "jdtls") {
		t.Fatalf("tools of other languages are not listed as missing:\n%s", status)
	}
	// without any choice the defaults decide
	fresh := NewInstaller(t.TempDir())
	fresh.env = env
	if got := fresh.Pending(true); !reflect.DeepEqual(got, []string{"black", "compose-language-service", "dockerfile-language-server", "prettier", "pyright", "vscode-langservers-extracted", "yaml-language-server"}) {
		t.Fatalf("the defaults need these tools: %v", got)
	}
}

func TestInstaller_InstallLanguagesInstallsTheMissingToolsAndReportsEachOne(t *testing.T) {
	m, env := newMachine(t, "npm", "pipx", "prettier") // prettier is there already
	m.installs["pyright"] = "pyright-langserver"
	m.installs["black"] = "black"
	m.fail["npm"] = nil
	installer := &Installer{env: env, tools: Defaults(), path: filepath.Join(t.TempDir(), "tools.json"), now: time.Now}
	var told []string
	results, err := installer.InstallLanguages(context.Background(), []string{"python", "html"}, func(tool string, done bool, err error) {
		told = append(told, tool+map[bool]string{false: " ...", true: " done"}[done])
	})
	if err != nil {
		t.Fatal(err)
	}
	byTool := map[string]InstallResult{}
	for _, r := range results {
		byTool[r.Tool] = r
	}
	if !byTool["prettier"].Already || byTool["pyright"].Already || byTool["pyright"].Err != nil {
		t.Fatalf("installed ones are skipped, missing ones installed: %+v", results)
	}
	if len(told) == 0 || strings.Contains(strings.Join(told, ","), "prettier") {
		t.Fatalf("progress tells about the tools that are installed now only: %v", told)
	}
	if _, err := installer.InstallLanguages(context.Background(), nil, nil); !errors.Is(err, ErrNoLanguages) {
		t.Fatalf("no language is an error: %v", err)
	}
}

// Choosing no language (n, esc) must install nothing: an empty choice is a choice, not "no choice".
func TestInstaller_NoLanguageChosenInstallsNothing(t *testing.T) {
	_, env := newMachine(t)
	dir := t.TempDir()
	if err := SaveLanguages(dir, nil); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(dir)
	installer.env = env
	if got := installer.Pending(true); len(got) != 0 {
		t.Fatalf("with no language chosen nothing is pending, not everything: %v", got)
	}
	if status := installer.Status(); len(status) != 0 {
		t.Fatalf("no tool is listed as missing: %v", status)
	}
	if got, ok := LoadLanguages(dir); !ok || got == nil || len(got) != 0 {
		t.Fatalf("an empty choice loads as an empty list: %#v %v", got, ok)
	}
	if got, _ := ParseLanguages(""); got == nil {
		t.Fatal("an empty list of languages is empty, not nil")
	}
	if !reflect.DeepEqual(AllLanguages(), func() []string { all, _ := ParseLanguages("all"); return all }()) {
		t.Fatal("AllLanguages names every language in order")
	}
}

func TestLoadLanguages_IgnoresARetiredLanguage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, languagesFile), []byte(`{"languages":["python","groovy"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadLanguages(dir)
	if !ok || len(got) != 1 || got[0] != "python" {
		t.Fatalf("a retired language must not discard the choice: %v %v", got, ok)
	}
}

func TestLoadLanguages_AsksAgainWhenOnlyARetiredLanguageWasChosen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, languagesFile), []byte(`{"languages":["groovy"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadLanguages(dir); ok {
		t.Fatalf("nothing usable was chosen, the question must come again: %v", got)
	}
}
