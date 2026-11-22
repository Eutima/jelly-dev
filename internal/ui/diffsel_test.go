package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitProject is a project window on a git repository with two commits, a branch `feature` and a tag, and a recording Self hook.
func gitProject(t *testing.T) (ProjectModel, *projectFixture, *[][]string) {
	t.Helper()
	model, fx := newProjectFixture(t, 120, sampleFiles)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", fx.root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "first")
	git("tag", "v1")
	git("branch", "feature")
	if err := os.WriteFile(filepath.Join(fx.root, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-q", "-am", "second")
	var runs [][]string
	model.hooks.Self = func(_ string, args ...string) *exec.Cmd {
		runs = append(runs, args)
		return exec.Command("true")
	}
	return model, fx, &runs
}

func TestDiff_WithRevisionArgument_RunsGitViewWithIt(t *testing.T) {
	model, _, runs := gitProject(t)
	for command, want := range map[string]string{"diff feature": "git feature", "diff v1..main": "git v1..main", "diff --staged": "git --staged"} {
		*runs = nil
		_, cmd := model.runProjectCommand(command)
		if cmd == nil || len(*runs) != 1 || strings.Join((*runs)[0], " ") != want {
			t.Errorf(":%s ran %v, want %q", command, *runs, want)
		}
	}
}

func TestDiff_RefusesUnknownRevisionAndOptions_WithAMessage(t *testing.T) {
	model, _, runs := gitProject(t)
	for _, command := range []string{"diff no-such-branch", "diff --output=x", "diff -p"} {
		updated, cmd := model.runProjectCommand(command)
		if cmd != nil || len(*runs) != 0 {
			t.Errorf(":%s must not start the diff view", command)
		}
		if got := updated.(ProjectModel); !got.warn && !got.editor.warn {
			t.Errorf(":%s must say why", command)
		}
	}
}

func TestDiffAgainst_ListsBranchesTagsAndCommits_AndRunsTheChosenOne(t *testing.T) {
	model, _, runs := gitProject(t)
	updated, _ := model.runProjectCommand("diffrev")
	model = updated.(ProjectModel)
	view := stripANSI(model.View())
	for _, want := range []string{"Diff against a branch, tag or commit", "main", "feature", "v1", "first"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the list lacks %q:\n%s", want, view)
		}
	}
	model, _ = projectKeys(model, "f", "e", "a", "t", "enter")
	if len(*runs) != 1 || strings.Join((*runs)[0], " ") != "git feature" || model.revs != nil {
		t.Fatalf("ran %v, revs %v", *runs, model.revs)
	}
}

func TestDiffBetween_AsksForBothSides(t *testing.T) {
	model, _, runs := gitProject(t)
	updated, _ := model.runProjectCommand("diffrevs")
	model = updated.(ProjectModel)
	model, _ = projectKeys(model, "v", "1", "enter") // the old side
	if model.revs == nil || !strings.Contains(stripANSI(model.View()), "Diff v1 .. which revision?") {
		t.Fatalf("the second list must follow:\n%s", stripANSI(model.View()))
	}
	model, _ = projectKeys(model, "f", "e", "a", "t", "enter")
	if len(*runs) != 1 || strings.Join((*runs)[0], " ") != "git v1..feature" {
		t.Fatalf("ran %v", *runs)
	}
}

func TestDiffFile_LimitsTheViewToTheShownFile(t *testing.T) {
	model, fx, runs := gitProject(t)
	if err := model.openFile(filepath.Join(fx.root, "src", "main.go"), false); err != nil {
		t.Fatal(err)
	}
	updated, _ := model.runProjectCommand("difffile")
	model = updated.(ProjectModel)
	model, _ = projectKeys(model, "v", "1", "enter")
	if len(*runs) != 1 || strings.Join((*runs)[0], " ") != "git v1 -- src/main.go" {
		t.Fatalf("ran %v", *runs)
	}
}

func TestDiffWorktree_ComparesWithAnotherWorktree(t *testing.T) {
	model, fx, runs := gitProject(t)
	other := filepath.Join(t.TempDir(), "side")
	if out, err := exec.Command("git", "-C", fx.root, "worktree", "add", "-q", "-b", "side", other).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	updated, _ := model.runProjectCommand("diffworktree")
	model = updated.(ProjectModel)
	if view := stripANSI(model.View()); !strings.Contains(view, "side") || strings.Contains(view, "main ") {
		t.Fatalf("only the other worktree is listed:\n%s", view)
	}
	model, _ = projectKeys(model, "enter")
	if len(*runs) != 1 || len((*runs)[0]) != 2 || (*runs)[0][1] != fx.root || filepath.Base((*runs)[0][0]) != "side" {
		t.Fatalf("ran %v, want the other worktree and this project", *runs)
	}
}

func TestDiffWorktree_SaysSo_WhenThereIsNoOtherWorktree(t *testing.T) {
	model, _, runs := gitProject(t)
	updated, _ := model.runProjectCommand("diffworktree")
	got := updated.(ProjectModel)
	if got.revs != nil || len(*runs) != 0 || !strings.Contains(got.status, "no other worktree") {
		t.Fatalf("revs %v, runs %v", got.revs, *runs)
	}
}

func TestDiff_OutsideAGitRepository_SaysSo(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Self = func(string, ...string) *exec.Cmd { t.Fatal("no diff view outside a repository"); return nil }
	for _, command := range []string{"diff main", "diffrev", "diffrevs", "difffile"} {
		updated, cmd := model.runProjectCommand(command)
		if cmd != nil || updated.(ProjectModel).revs != nil {
			t.Errorf(":%s must refuse outside a git repository", command)
		}
	}
}

func TestDiff_RefusesSeveralRevisions(t *testing.T) {
	model, _, runs := gitProject(t)
	if _, cmd := model.runProjectCommand("diff main feature v1"); cmd != nil || len(*runs) != 0 {
		t.Fatal(":diff takes one revision")
	}
}
