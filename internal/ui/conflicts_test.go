package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

// conflictedRepo is a repository in the middle of a merge that has a conflict in a.txt.
func conflictedRepo(t *testing.T) string {
	t.Helper()
	dir := gitRepoWithCommit(t)
	git := func(allowFail bool, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil && !allowFail {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(false, "checkout", "-q", "-b", "other")
	write("theirs\n")
	git(false, "commit", "-q", "-am", "theirs")
	git(false, "checkout", "-q", "main")
	write("ours\n")
	git(false, "commit", "-q", "-am", "ours")
	git(true, "merge", "other") // stops on the conflict
	return dir
}

func TestConflicts_AreShownInTheTopBar_WhenTheProjectIsInAMerge(t *testing.T) {
	dir := conflictedRepo(t)
	model, err := NewProjectModel(dir, "", project.Session{}, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model = updated.(ProjectModel)
	if view := stripANSI(model.View()); !strings.Contains(view, "! 1 conflict") || !strings.Contains(view, ":mergetool") {
		t.Fatalf("the top bar must show the conflict and the command:\n%s", view)
	}
}

func TestConflicts_AreNotShown_WhenThereAreNone(t *testing.T) {
	model, err := NewProjectModel(gitRepoWithCommit(t), "", project.Session{}, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	if view := stripANSI(updated.View()); strings.Contains(view, "conflict") {
		t.Fatalf("no conflict, nothing to show:\n%s", view)
	}
}

func TestMergeToolCommand_StartsTheMergeTool_WhenThereAreConflicts(t *testing.T) {
	dir := conflictedRepo(t)
	var ranIn string
	model, err := NewProjectModel(dir, "", project.Session{}, ProjectHooks{MergeTool: func(d string) *exec.Cmd {
		ranIn = d
		return exec.Command("true")
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd := model.runProjectCommand("mergetool")
	if cmd == nil || updated.(ProjectModel).conflicts == nil {
		t.Fatal(":mergetool must start the merge tool")
	}
	if ranIn != dir {
		t.Fatalf("the merge tool must run in the project, ran in %q", ranIn)
	}
}

func TestMergeToolCommand_SaysSo_WhenThereAreNoConflicts(t *testing.T) {
	model, err := NewProjectModel(gitRepoWithCommit(t), "", project.Session{}, ProjectHooks{MergeTool: func(string) *exec.Cmd {
		t.Error("nothing to merge")
		return exec.Command("true")
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.runProjectCommand("mergetool")
	if got := updated.(ProjectModel).status; !strings.Contains(got, "No merge conflicts") {
		t.Fatalf("status %q", got)
	}
}

func TestPalette_OffersTheMergeTool(t *testing.T) {
	model, err := NewProjectModel(gitRepoWithCommit(t), "", project.Session{}, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range model.paletteActions() {
		if action.key == ":mergetool" {
			return
		}
	}
	t.Fatal("the palette must list :mergetool")
}

func TestExternalProgramEnd_ReloadsChangedFiles_WhenTheyHaveNoUnsavedEdits(t *testing.T) {
	dir := conflictedRepo(t)
	path := filepath.Join(dir, "a.txt")
	model, err := NewProjectModel(dir, path, project.Session{}, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(model.editor.Lines(), "\n"), "<<<<<<<") {
		t.Fatalf("the file must start with conflict markers: %v", model.editor.Lines())
	}
	if err := os.WriteFile(path, []byte("resolved\n"), 0o644); err != nil { // what the merge tool does
		t.Fatal(err)
	}
	updated, _ := model.Update(externalDoneMsg{what: "mergetool"})
	model = updated.(ProjectModel)
	if got := strings.Join(model.editor.Lines(), "\n"); got != "resolved" {
		t.Fatalf("the editor must show the merged file, it shows %q", got)
	}
	if status := model.status + model.editor.status; !strings.Contains(status, "Reloaded a.txt") {
		t.Fatalf("status %q", status)
	}
}

func TestExternalProgramEnd_KeepsUnsavedEdits(t *testing.T) {
	dir := gitRepoWithCommit(t)
	path := filepath.Join(dir, "a.txt")
	model, err := NewProjectModel(dir, path, project.Session{}, ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	model.editor.vim.Handle("x") // an unsaved change
	if !model.editor.Modified() {
		t.Fatal("the edit must count as unsaved")
	}
	if err := os.WriteFile(path, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(externalDoneMsg{what: "diff"})
	if !updated.(ProjectModel).editor.Modified() {
		t.Fatal("a tab with unsaved edits must keep its text")
	}
}

func TestCloseDialog_CtrlCCancelsLikeEsc_AndCtrlDConfirms(t *testing.T) {
	newDialog := func() ProjectModel {
		model, err := NewProjectModel(gitRepoWithCommit(t), "", project.Session{}, ProjectHooks{})
		if err != nil {
			t.Fatal(err)
		}
		return model.confirmClose("Close?", func(m ProjectModel) (tea.Model, tea.Cmd) { return m, nil })
	}
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyEsc} {
		updated, _ := newDialog().dialogKey(tea.KeyMsg{Type: key})
		if updated.(ProjectModel).dlg != nil {
			t.Errorf("key %v must close the dialog", key)
		}
	}
	ran := false
	model := newDialog()
	model.dlg.then = func(m ProjectModel) (tea.Model, tea.Cmd) { ran = true; return m, nil }
	model.dialogKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if ran {
		t.Error("ctrl+c must not confirm")
	}
	model.dialogKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !ran {
		t.Error("ctrl+d still confirms")
	}
}
