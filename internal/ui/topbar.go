package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/source"
)

// externalDoneMsg reports that a program started from the window (shell, diff view) has ended.
type externalDoneMsg struct {
	what string
	err  error
}

// topBar is the line above the panes: the project name and the buttons to switch project, open a terminal and show the diff.
func (m ProjectModel) topBar() *bar {
	result := &bar{}
	result.add(barPart{" " + sanitize(filepath.Base(m.root)) + " ", m.theme.Footer.Bold(true)})
	if branch := currentBranch(m.root); branch != "" { // plain ASCII: a symbol like ⎇ is drawn wider by some terminals and shifts what is clickable
		result.add(barPart{"(" + sanitize(branch) + ") ", m.theme.Footer})
	}
	if owner, ok := project.NoteOwner(m.editor.Path()); ok { // the shown file is a note: say whose
		result.add(barPart{" Note of " + sanitize(filepath.Base(owner)) + " ", m.theme.Header.Bold(true)})
	}
	result.add(barPart{" ", m.theme.Footer})
	label := barPart{" Commands ", m.theme.Header}
	if shown := keys.Label(keymap.Palette); shown != "" {
		result.button([]string{paletteClick}, []barPart{label, {"(" + shown + ") ", m.theme.Header}}, []barPart{label})
	} else {
		result.button([]string{paletteClick}, []barPart{label}, []barPart{label})
	}
	result.add(barPart{" ", m.theme.Footer})
	if label := m.conflictsLabel(); label != "" { // merge conflicts: a click starts the merge tool
		result.button([]string{mergeToolKey}, []barPart{{label, m.theme.Header.Bold(true)}, {"(:mergetool) ", m.theme.Header}}, []barPart{{label, m.theme.Header.Bold(true)}})
		result.add(barPart{" ", m.theme.Footer})
	}
	return result
}

// clickTopBar runs the action of the button at column x.
func (m ProjectModel) clickTopBar(x int) (tea.Model, tea.Cmd) {
	clicked := m.topBar().visibleKeysAt(x, m.width)
	if len(clicked) == 0 {
		return m, nil
	}
	switch clicked[0] {
	case paletteClick:
		return m.openPalette()
	case mergeToolKey:
		return m.startMergeTool()
	}
	return m, nil
}

// noticeDuration is how long a message that goes away by itself stays.
const noticeDuration = 3 * time.Second

// noticeExpiredMsg ends a message that was shown for a moment; a newer message stays.
type noticeExpiredMsg struct{ text string }

// noChanges reports whether git knows the project and lists no change in it (no modified, added, deleted or untracked
// file). When that cannot be told (no git, no repository) the diff is shown as usual.
func noChanges(root string) bool {
	changes, err := source.OpenGit(root, source.GitSpec{})
	if err != nil {
		return false
	}
	files, err := changes.List()
	return err == nil && len(files) == 0
}

// showDiff suspends the window and shows the changes of the project; the window comes back when the view is closed.
func (m ProjectModel) showDiff() (tea.Model, tea.Cmd) {
	if !inGitRepository(m.root) {
		m.say("E1: "+m.root+" is not a git repository, there is no diff to show", true)
		return m, nil
	}
	if noChanges(m.root) { // the diff view would open and close at once: say it instead, for a moment
		text := "No changes: the working tree is clean"
		m.say(text, false)
		return m, tea.Tick(noticeDuration, func(time.Time) tea.Msg { return noticeExpiredMsg{text: text} })
	}
	command := defaultDiff
	if m.hooks.Diff != nil {
		command = m.hooks.Diff
	}
	return m.runExternal("diff", command(m.root))
}

func (m ProjectModel) runExternal(what string, cmd *exec.Cmd) (tea.Model, tea.Cmd) {
	if cmd == nil {
		m.say(fmt.Sprintf("E1: Cannot start the %s", what), true)
		return m, nil
	}
	m.notice("", false)
	m.editor.notice("", false)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return externalDoneMsg{what: what, err: err} })
}

// defaultTerminal runs the user's shell in dir.
func defaultTerminal(dir string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" && runtime.GOOS == "windows" {
		shell = os.Getenv("ComSpec")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	return cmd
}

// defaultDiff runs this program's git view (`jd git`) in dir.
func defaultDiff(dir string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	cmd := exec.Command(exe, "git")
	cmd.Dir = dir
	return cmd
}

// inGitRepository reports whether dir or one of its parents holds a .git entry.
func inGitRepository(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// currentBranch returns the branch checked out in the repository of dir ("" outside a repository), or the short commit id when HEAD
// is detached. It reads .git/HEAD instead of running git, because the top bar is drawn on every key press.
func currentBranch(dir string) string {
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			return branchFromGit(gitPath, info.IsDir())
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// branchFromGit reads HEAD of a .git folder, or of the folder a .git file ("gitdir: <path>", used by worktrees) points to.
func branchFromGit(gitPath string, isDir bool) string {
	gitDir := gitPath
	if !isDir {
		data, err := os.ReadFile(gitPath) // #nosec G304 -- a .git entry of the user's own project
		if err != nil {
			return ""
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(gitPath), target)
		}
		gitDir = target
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) // #nosec G304 -- see above
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(head))
	if branch, ok := strings.CutPrefix(text, "ref: refs/heads/"); ok {
		return branch
	}
	if len(text) > 7 {
		return text[:7]
	}
	return text
}
