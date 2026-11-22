package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/source"
)

// revPicker is the open list of revisions (or worktrees) to compare with; choosing one runs then with its value.
type revPicker struct {
	picker PickerModel
	values []string
	then   func(ProjectModel, string) (tea.Model, tea.Cmd)
}

// selfCommand runs this program in dir with the given arguments (the diff views are jd itself).
func (m ProjectModel) selfCommand(args ...string) *exec.Cmd {
	if m.hooks.Self != nil {
		return m.hooks.Self(m.root, args...)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	cmd := exec.Command(exe, args...) // #nosec G204 -- this program with arguments that were built here, not through a shell
	cmd.Dir = m.root
	return cmd
}

// diffCommand handles `:diff [<rev> | <a>..<b> | --staged]`: the diff of the project against something else than HEAD.
// Without an argument it is the plain diff of the working tree. The other commands choose the other side from a list.
func (m ProjectModel) diffCommand(argument string) (tea.Model, tea.Cmd) {
	if argument == "" {
		return m.showDiff()
	}
	args := strings.Fields(argument)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && arg != "--staged" {
			m.say("Usage: :diff [--staged] [<revision> | <a>..<b>]", true)
			return m, nil
		}
	}
	if len(args) > 1 && !(len(args) == 2 && args[0] == "--staged") {
		m.say("Usage: :diff [--staged] [<revision> | <a>..<b>] (one revision)", true)
		return m, nil
	}
	return m.runGitDiff(args, "")
}

// runGitDiff shows `jd git` with the arguments, limited to one file when path is not empty. The revision is checked first, so a wrong
// one is a message here instead of a screen that opens and closes.
func (m ProjectModel) runGitDiff(args []string, path string) (tea.Model, tea.Cmd) {
	if !inGitRepository(m.root) {
		m.say("E1: "+m.root+" is not a git repository, there is no diff to show", true)
		return m, nil
	}
	spec := source.GitSpec{}
	for _, arg := range args {
		if arg == "--staged" {
			spec.Staged = true
		} else {
			spec.Rev = arg
		}
	}
	if _, err := source.OpenGit(m.root, spec); err != nil {
		m.say("Cannot compare: "+err.Error(), true)
		return m, nil
	}
	full := append([]string{"git"}, args...)
	if path != "" {
		full = append(full, "--", path)
	}
	return m.runExternal("diff", m.selfCommand(full...))
}

// openRevPicker lists the branches, tags and recent commits of the project; choosing one runs then with its name.
func (m ProjectModel) openRevPicker(title string, then func(ProjectModel, string) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !inGitRepository(m.root) {
		m.say("E1: "+m.root+" is not a git repository, there is no diff to show", true)
		return m, nil
	}
	refs, err := source.ListRefs(m.root)
	if err != nil {
		m.say("Cannot list the revisions: "+err.Error(), true)
		return m, nil
	}
	if len(refs) == 0 {
		m.say("No branch, tag or commit to compare with", true)
		return m, nil
	}
	items := make([]PickItem, len(refs))
	values := make([]string, len(refs))
	for i, ref := range refs {
		items[i] = PickItem{Lead: ref.Kind, Label: sanitize(ref.Name), Detail: sanitize(ref.Detail)}
		values[i] = ref.Name
	}
	m.revs = &revPicker{picker: newPicker(m.theme, title, items), values: values, then: then}
	return m, nil
}

// diffAgainst compares the project (the working tree) with a branch, tag or commit.
func (m ProjectModel) diffAgainst() (tea.Model, tea.Cmd) {
	return m.openRevPicker("Diff against a branch, tag or commit", func(next ProjectModel, rev string) (tea.Model, tea.Cmd) {
		return next.runGitDiff([]string{rev}, "")
	})
}

// diffBetween compares two revisions: the first choice is the old side, the second the new one.
func (m ProjectModel) diffBetween() (tea.Model, tea.Cmd) {
	return m.openRevPicker("Diff between two revisions: the old side", func(next ProjectModel, from string) (tea.Model, tea.Cmd) {
		return next.openRevPicker("Diff "+sanitize(from)+" .. which revision?", func(last ProjectModel, to string) (tea.Model, tea.Cmd) {
			return last.runGitDiff([]string{from + ".." + to}, "")
		})
	})
}

// diffFile compares the file of the shown tab with a branch, tag or commit.
func (m ProjectModel) diffFile() (tea.Model, tea.Cmd) {
	path := m.editor.Path()
	if path == "" {
		m.say("E32: No file name", true)
		return m, nil
	}
	rel, err := filepath.Rel(m.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		m.say("The file is not in this project", true)
		return m, nil
	}
	return m.openRevPicker("Diff "+sanitize(filepath.ToSlash(rel))+" against", func(next ProjectModel, rev string) (tea.Model, tea.Cmd) {
		return next.runGitDiff([]string{rev}, filepath.ToSlash(rel))
	})
}

// diffWorktree compares this project with another working tree of the same repository (the directory diff: the other tree is the old side).
func (m ProjectModel) diffWorktree(argument string) (tea.Model, tea.Cmd) {
	if argument != "" {
		return m.compareWith(m.resolve(argument))
	}
	trees, err := source.ListWorktrees(m.root)
	if err != nil {
		m.say("Cannot list the worktrees: "+err.Error(), true)
		return m, nil
	}
	here := m.root
	if abs, err := filepath.EvalSymlinks(here); err == nil {
		here = abs
	}
	var items []PickItem
	var values []string
	for _, tree := range trees {
		if tree.Path == here || tree.Path == m.root {
			continue
		}
		label := tree.Branch
		if label == "" {
			label = "(detached " + tree.Head + ")"
		}
		items = append(items, PickItem{Lead: "worktree", Label: sanitize(label), Detail: sanitize(tree.Path)})
		values = append(values, tree.Path)
	}
	if len(items) == 0 {
		m.say("This repository has no other worktree (git worktree add <path>)", true)
		return m, nil
	}
	m.revs = &revPicker{picker: newPicker(m.theme, "Diff against another worktree", items), values: values,
		then: func(next ProjectModel, path string) (tea.Model, tea.Cmd) { return next.compareWith(path) }}
	return m, nil
}

// compareWith shows the directory diff of another folder (the old side) and this project (the new one).
func (m ProjectModel) compareWith(other string) (tea.Model, tea.Cmd) {
	if !isDir(other) {
		m.say(fmt.Sprintf("E344: Can't find directory \"%s\"", other), true)
		return m, nil
	}
	return m.runExternal("diff", m.selfCommand(other, m.root))
}

// updateRevPicker passes a key or click to the open list; the chosen entry runs its action.
func (m ProjectModel) updateRevPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.revs.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	switch outcome {
	case pickNone:
		revs := *m.revs
		revs.picker = picker
		m.revs = &revs
		return m, nil
	case pickCancelled:
		m.revs = nil
		return m, nil
	}
	chosen, values, then := picker.Chosen(), m.revs.values, m.revs.then
	m.revs = nil
	if chosen < 0 || chosen >= len(values) {
		return m, nil
	}
	return then(m, values[chosen])
}
