package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/source"
)

// conflictCheckInterval is the shortest time between two looks for merge conflicts while keys are pressed: a merge started
// in the terminal tab or in another window shows up in the top bar.
const conflictCheckInterval = 5 * time.Second

// conflictsMsg carries the files that have unresolved merge conflicts in a project.
type conflictsMsg struct {
	root  string
	files []string
}

// checkConflicts looks for the files with merge conflicts in the background.
func (m ProjectModel) checkConflicts() tea.Cmd {
	root := m.root
	if !inGitRepository(root) {
		return func() tea.Msg { return conflictsMsg{root: root} }
	}
	return func() tea.Msg {
		files, _ := source.UnmergedFiles(root) // an error counts as no conflicts: the next check tries again
		return conflictsMsg{root: root, files: files}
	}
}

// conflictsLabel is the text of the top bar button, "" without conflicts.
func (m ProjectModel) conflictsLabel() string {
	switch len(m.conflicts) {
	case 0:
		return ""
	case 1:
		return " ! 1 conflict "
	}
	return fmt.Sprintf(" ! %d conflicts ", len(m.conflicts))
}

// startMergeTool suspends the window and runs git's merge tool (jd) for the conflicting files; the window comes back
// when it is done.
func (m ProjectModel) startMergeTool() (tea.Model, tea.Cmd) {
	if !inGitRepository(m.root) {
		m.say("E1: "+m.root+" is not a git repository, there is nothing to merge", true)
		return m, nil
	}
	files, err := source.UnmergedFiles(m.root)
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	m.conflicts = files
	if len(files) == 0 {
		m.say("No merge conflicts", false)
		return m, nil
	}
	command := defaultMergeTool
	if m.hooks.MergeTool != nil {
		command = m.hooks.MergeTool
	}
	return m.runExternal("mergetool", command(m.root))
}

// defaultMergeTool runs `git mergetool` with this program as the tool, whatever git is configured with.
func defaultMergeTool(dir string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	quoted := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	cmd := exec.Command("git", "-c", "merge.tool=jd",
		"-c", "mergetool.jd.cmd="+quoted+` merge "$BASE" "$LOCAL" "$REMOTE" "$MERGED"`,
		"-c", "mergetool.jd.trustExitCode=true", "-c", "mergetool.keepBackup=false",
		"mergetool", "--no-prompt")
	cmd.Dir = dir
	return cmd
}
