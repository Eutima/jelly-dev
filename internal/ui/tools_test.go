package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
)

// fakeTools is a tool installer with scripted results.
type fakeTools struct {
	missing   []string
	results   map[string]error
	installed []string
	forced    []bool
	added     []string            // the languages AddLanguage was asked for
	updated   []string            // the languages UpdateLanguage was asked for
	addTools  map[string][]string // the tools AddLanguage installs, by language
}

func (f *fakeTools) Pending(force bool) []string {
	f.forced = append(f.forced, force)
	return append([]string(nil), f.missing...)
}

func (f *fakeTools) Install(_ context.Context, name string) error {
	f.installed = append(f.installed, name)
	return f.results[name]
}

func (f *fakeTools) LanguageStates() []tools.LanguageState {
	return []tools.LanguageState{
		{ID: "python", Name: "Python", Tools: []string{"pyright", "black"}, Missing: []string{"pyright", "black"}},
		{ID: "go", Name: "Go", Tools: []string{"gopls"}},
		{ID: "html", Name: "HTML", Tools: []string{"vscode-langservers-extracted", "prettier"}, Missing: []string{"prettier"}},
	}
}

func (f *fakeTools) AddLanguage(_ context.Context, id string) ([]tools.InstallResult, error) {
	f.added = append(f.added, id)
	if err := f.results["add:"+id]; err != nil {
		return nil, err
	}
	var results []tools.InstallResult
	for _, name := range f.addTools[id] {
		results = append(results, tools.InstallResult{Tool: name, Err: f.results[name]})
	}
	return results, nil
}

func (f *fakeTools) UpdateLanguage(_ context.Context, id string) ([]tools.InstallResult, error) {
	f.updated = append(f.updated, id)
	if err := f.results["update:"+id]; err != nil {
		return nil, err
	}
	var results []tools.InstallResult
	for _, name := range f.addTools[id] {
		results = append(results, tools.InstallResult{Tool: name, Updated: f.results[name] == nil, Err: f.results[name]})
	}
	return results, nil
}

func (f *fakeTools) Status() []string {
	lines := []string{"ok       gopls (language server for Go)"}
	for _, name := range f.missing {
		lines = append(lines, "missing  "+name+": offline")
	}
	return lines
}

// toolsProject opens a project window whose Init the test runs itself, like the runtime.
func toolsProject(t *testing.T, installer *fakeTools, auto bool) ProjectModel {
	t.Helper()
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Tools, model.hooks.AutoInstall = installer, auto
	return model
}

func TestTools_InstallsMissingToolsWhenWindowOpens(t *testing.T) {
	installer := &fakeTools{missing: []string{"black", "prettier"}, results: map[string]error{}}
	model := toolsProject(t, installer, true)
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("the window must check for missing tools when it starts")
	}
	model = settleProject(model, cmd)
	if strings.Join(installer.installed, ",") != "black,prettier" {
		t.Fatalf("installed %v", installer.installed)
	}
	if !strings.Contains(model.status, "installed black, prettier") {
		t.Fatalf("the result must be shown: %q", model.status)
	}
	if model.toolRun != nil || model.toolStatus != "" {
		t.Fatal("the run must be over")
	}
	if len(installer.forced) != 1 || installer.forced[0] {
		t.Fatalf("the automatic check must not force: %v", installer.forced)
	}
}

func TestTools_ShowsProgressWhileInstalling(t *testing.T) {
	installer := &fakeTools{missing: []string{"black", "prettier"}, results: map[string]error{}}
	model := toolsProject(t, installer, true)
	updated, cmd := model.Update(toolsPendingMsg{names: installer.missing})
	model = updated.(ProjectModel)
	if cmd == nil || !strings.Contains(stripANSI(model.View()), "Installing black (1/2)...") {
		t.Fatalf("progress expected:\n%s", model.View())
	}
	updated, _ = model.Update(toolDoneMsg{name: "black"})
	model = updated.(ProjectModel)
	if !strings.Contains(stripANSI(model.View()), "Installing prettier (2/2)...") {
		t.Fatalf("the next tool must follow:\n%s", model.View())
	}
}

func TestTools_NothingHappensWithoutAutoInstallOrWhenNothingMissing(t *testing.T) {
	installer := &fakeTools{results: map[string]error{}}
	if cmd := toolsProject(t, installer, false).Init(); cmd != nil {
		t.Fatal("without AutoInstall the window must not check")
	}
	model := toolsProject(t, installer, true)
	model = settleProject(model, model.Init())
	if len(installer.installed) != 0 || strings.Contains(stripANSI(model.View()), "Tools:") {
		t.Fatal("nothing missing: nothing to say")
	}
	if cmd := (ProjectModel{}).Init(); cmd != nil {
		t.Fatal("no installer: no check")
	}
}

func TestTools_SummarisesFailuresAndMissingRuntimes(t *testing.T) {
	installer := &fakeTools{missing: []string{"black", "pyright", "gopls2", "jdtls"}, results: map[string]error{
		"black":   &tools.MissingRuntimeError{Tool: "black", Needs: "Python (pipx or pip)"},
		"pyright": &tools.MissingRuntimeError{Tool: "pyright", Needs: "npm (Node.js)"},
		"gopls2":  errors.New("go install: network is unreachable"),
	}}
	model := toolsProject(t, installer, true)
	model = settleProject(model, model.Init())
	view := stripANSI(model.View())
	if !strings.Contains(view, "installed jdtls") && !strings.Contains(model.status, "installed jdtls") {
		t.Fatalf("view:\n%s", view)
	}
	for _, want := range []string{"2 need", "run the jd installer", "failed: gopls2", ":tools retries"} {
		if !strings.Contains(model.status, want) {
			t.Fatalf("summary lacks %q: %q", want, model.status)
		}
	}
	if !model.warn {
		t.Fatal("failures are shown as a warning")
	}
}

func TestTools_ToolsCommandRetriesWithForceAndReportsStatus(t *testing.T) {
	installer := &fakeTools{missing: []string{"black"}, results: map[string]error{}}
	model := toolsProject(t, installer, false)
	model = inEditor(model)
	model, _ = typeCommand(model, "tools status")
	if !strings.Contains(model.Editor().status, "1 of 2 installed") || !strings.Contains(model.Editor().status, "missing: black") {
		t.Fatalf("status %q", model.Editor().status)
	}
	if len(installer.installed) != 0 {
		t.Fatal("status installs nothing")
	}
	model, _ = projectKeys(model, ":", "t", "o", "o", "l", "s", "enter") // the messages of the command are fed back: it installs
	if len(installer.installed) != 1 || len(installer.forced) == 0 || !installer.forced[len(installer.forced)-1] {
		t.Fatalf(":tools must force a new try: installed %v forced %v", installer.installed, installer.forced)
	}
	none := &fakeTools{results: map[string]error{}}
	model = inEditor(toolsProject(t, none, false))
	model = settleProject(model, model.checkTools(true))
	model, _ = typeCommand(model, "tools")
	model = settleProject(model, model.checkTools(true))
	if !strings.Contains(model.Editor().status, "All language servers and formatters are installed") && !strings.Contains(stripANSI(model.View()), "All language servers") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestTools_CommandWithoutInstallerExplainsAndDoesNotRunTwice(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model = inEditor(model)
	model, _ = typeCommand(model, "tools")
	if !strings.Contains(stripANSI(model.View()), "not available here") {
		t.Fatalf("view:\n%s", model.View())
	}
	installer := &fakeTools{missing: []string{"black"}, results: map[string]error{}}
	model = inEditor(toolsProject(t, installer, true))
	updated, _ := model.Update(toolsPendingMsg{names: installer.missing})
	model = updated.(ProjectModel)
	model, _ = typeCommand(model, "tools")
	if !strings.Contains(stripANSI(model.View()), "being installed already") {
		t.Fatalf("a second run must be refused:\n%s", model.View())
	}
	updated, cmd := model.Update(toolsPendingMsg{names: []string{"x"}})
	_ = updated
	if cmd != nil {
		t.Fatal("a run in progress is not restarted")
	}
	_ = tea.Quit
}

func addLangProject(t *testing.T, installer *fakeTools) ProjectModel {
	t.Helper()
	model, _ := newProjectFixture(t, 110, sampleFiles)
	model.hooks.Tools = installer
	return inEditor(model)
}

func TestAddLanguage_ListsAllLanguagesAndInstallsTheServerAndFormatterOfTheChosenOne(t *testing.T) {
	installer := &fakeTools{results: map[string]error{}, addTools: map[string][]string{"python": {"pyright", "black"}}}
	model, _ := typeCommand(addLangProject(t, installer), "addlang")
	view := stripANSI(model.View())
	for _, want := range []string{"Add a language", "Python", "pyright, black", "Go", "gopls", "(installed - enter updates it)", "(missing: prettier)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the list lacks %q:\n%s", want, view)
		}
	}
	model = typeIntoPicker(model, "pyth") // the list is searchable
	if view = stripANSI(model.View()); strings.Contains(view, "gopls") || !strings.Contains(view, "Python") {
		t.Fatalf("typing narrows the list:\n%s", view)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ProjectModel)
	if model.langs != nil || !strings.Contains(model.toolStatus, "Installing pyright and black for Python") {
		t.Fatalf("choosing closes the list and shows the progress: %q", model.toolStatus)
	}
	model = settleProject(model, cmd)
	view = stripANSI(model.View())
	if len(installer.added) != 1 || installer.added[0] != "python" || !strings.Contains(view, "Python added: installed pyright and black") || model.toolStatus != "" {
		t.Fatalf("the language is added: %v\n%s", installer.added, view)
	}
}

func TestAddLanguage_AlreadyInstalledFailuresMissingRuntimeAndCancel(t *testing.T) {
	// everything there already
	installer := &fakeTools{results: map[string]error{}, addTools: map[string][]string{"go": {"gopls"}}}
	model, _ := typeCommand(addLangProject(t, installer), "language")
	model = typeIntoPicker(model, "go")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(updated.(ProjectModel).toolStatus, "Updating gopls for Go") {
		t.Fatalf("an installed language is updated, the status says so: %q", updated.(ProjectModel).toolStatus)
	}
	model = settleProject(updated.(ProjectModel), cmd)
	if view := stripANSI(model.View()); !strings.Contains(view, "Go updated: gopls") || len(installer.updated) != 1 || installer.updated[0] != "go" || len(installer.added) != 0 {
		t.Fatalf("an installed language is updated, not added again: updated %v added %v\n%s", installer.updated, installer.added, view)
	}
	// a failure, and a missing runtime that points to jd setup
	failing := &fakeTools{
		results:  map[string]error{"pyright": &tools.MissingRuntimeError{Tool: "pyright", Needs: "npm (Node.js)"}, "black": errors.New("offline")},
		addTools: map[string][]string{"python": {"pyright", "black"}},
	}
	model, _ = typeCommand(addLangProject(t, failing), "addlang")
	model = typeIntoPicker(model, "python")
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = settleProject(updated.(ProjectModel), cmd)
	view := stripANSI(model.View())
	if !strings.Contains(view, "could not install") || !strings.Contains(view, "pyright") || !model.Editor().warn {
		t.Fatalf("failures are shown as a warning:\n%s", view)
	}
	// esc closes the list and installs nothing
	cancelled := &fakeTools{results: map[string]error{}}
	model, _ = typeCommand(addLangProject(t, cancelled), "addlang")
	if model, _ = projectKeys(model, "esc"); model.langs != nil || len(cancelled.added) != 0 {
		t.Fatal("esc closes the list and installs nothing")
	}
	// no installer (a window without tools) and an install that is running
	plain, _ := newProjectFixture(t, 100, sampleFiles)
	plain, _ = typeCommand(inEditor(plain), "addlang")
	if !strings.Contains(stripANSI(plain.View()), "Tool installation is not available") {
		t.Fatalf("without an installer the command says so:\n%s", plain.View())
	}
	busy := addLangProject(t, &fakeTools{results: map[string]error{}})
	busy.toolStatus = "Installing black (1/2)..."
	busy, _ = typeCommand(busy, "addlang")
	if busy.langs != nil || !strings.Contains(stripANSI(busy.View()), "already") {
		t.Fatalf("while tools are installed the list does not open:\n%s", busy.View())
	}
}

func TestAddLanguage_TheListSaysThatAnInstalledLanguageIsUpdatedAndAFailedUpdateIsReported(t *testing.T) {
	model, _ := typeCommand(addLangProject(t, &fakeTools{results: map[string]error{}}), "addlang")
	if view := stripANSI(model.View()); !strings.Contains(view, "Go  gopls  (installed - enter updates it)") {
		t.Fatalf("the installed language says that enter updates it:\n%s", view)
	}
	failing := &fakeTools{
		results:  map[string]error{"gopls": &tools.ManualInstallError{Tool: "gopls", Hint: "brew upgrade gopls", Update: true}},
		addTools: map[string][]string{"go": {"gopls"}},
	}
	model, _ = typeCommand(addLangProject(t, failing), "addlang")
	model = typeIntoPicker(model, "go")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = settleProject(updated.(ProjectModel), cmd)
	view := stripANSI(model.View())
	if !strings.Contains(view, "could not update") || !strings.Contains(view, "gopls") || !model.Editor().warn {
		t.Fatalf("a failed update is a warning that says update:\n%s", view)
	}
}
