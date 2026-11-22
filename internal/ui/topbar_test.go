package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

// runPalette runs the action of the command palette with the given label.
func runPalette(t *testing.T, m ProjectModel, label string) (ProjectModel, tea.Cmd) {
	t.Helper()
	for _, action := range m.paletteActions() {
		if action.label == label {
			updated, cmd := action.run(m)
			return updated.(ProjectModel), cmd
		}
	}
	t.Fatalf("no palette action %q", label)
	return m, nil
}

// buttonX returns a column inside the top bar button with the given action.
func buttonX(t *testing.T, m ProjectModel, action string) int {
	t.Helper()
	for _, r := range m.topBar().regions {
		if r.keys[0] == action {
			return r.from + 1
		}
	}
	t.Fatalf("no %s button", action)
	return 0
}

func clickBar(m ProjectModel, x int) (ProjectModel, tea.Cmd) {
	updated, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 0})
	return updated.(ProjectModel), cmd
}

func TestTopBar_ShowsOnlyTheCommandsButton(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if err := os.Mkdir(filepath.Join(model.root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := strings.Split(model.View(), "\n")[0]
	if !strings.Contains(first, "Commands") || !strings.Contains(first, "ctrl+a") {
		t.Fatalf("the top bar must offer the palette: %q", first)
	}
	for _, gone := range []string{"Project", "Terminal", "Diff"} {
		if strings.Contains(first, gone) {
			t.Fatalf("the top bar must not show %s any more: %q", gone, first)
		}
	}
}

func TestTopBar_CommandsButtonOpensThePalette(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = clickBar(model, buttonX(t, model, paletteClick))
	if model.palette == nil {
		t.Fatal("a click on the commands button must open the palette")
	}
}

func TestPalette_ProjectActionOpensPicker(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = runPalette(t, model, "Switch project")
	if model.picker == nil {
		t.Fatal("the project action must open the project list")
	}
}

func TestPalette_TerminalActionRunsShellInProject(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	var dir string
	model.hooks.Terminal = func(d string) *exec.Cmd { dir = d; return exec.Command("true") }
	model, cmd := runPalette(t, model, "Show or hide the terminal")
	if cmd == nil || dir != model.root {
		t.Fatalf("terminal must start in the project: cmd %v dir %q", cmd != nil, dir)
	}
}

func TestPalette_DiffNeedsGitRepository(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	called := false
	model.hooks.Diff = func(string) *exec.Cmd { called = true; return exec.Command("true") }
	model, cmd := runPalette(t, model, "Show the git diff")
	if called || cmd != nil || !model.warn {
		t.Fatalf("outside a repository the diff must warn: called %v warn %v status %q", called, model.warn, model.status)
	}
	if err := os.Mkdir(filepath.Join(model.root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, cmd = runPalette(t, model, "Show the git diff"); cmd == nil || !called {
		t.Fatal("in a repository the diff must start")
	}
}

func TestTopBar_ExCommandsAndFailureReported(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("true") }
	_, cmd := model.runProjectCommand("terminal")
	if cmd == nil {
		t.Fatal(":terminal must start a shell")
	}
	updated, _ := model.Update(externalDoneMsg{what: "terminal", err: os.ErrNotExist})
	if got := updated.(ProjectModel); !got.warn {
		t.Fatal("a failing program must be reported")
	}
}

func TestTopBar_ClickInInsertModeOpensThePaletteWithoutTyping(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter") // opens a file in the editor
	model, _ = projectKeys(model, "i")
	before := strings.Join(model.Editor().Lines(), "\n")
	model, _ = clickBar(model, buttonX(t, model, paletteClick))
	if got := strings.Join(model.Editor().Lines(), "\n"); got != before {
		t.Fatalf("a click must not type into the text:\n%q\n%q", before, got)
	}
	if model.palette == nil {
		t.Fatal("the click must open the palette, also in insert mode")
	}
}

func gitRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "first")
	return dir
}

func TestDiff_SaysThereAreNoChangesForThreeSeconds_WhenTheTreeIsClean(t *testing.T) {
	dir := gitRepoWithCommit(t)
	model, err := NewProjectModel(dir, "", project.Session{}, ProjectHooks{Diff: func(string) *exec.Cmd {
		t.Error("with nothing to show the diff view must not open")
		return exec.Command("true")
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model = updated.(ProjectModel)
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true})
	model = updated.(ProjectModel)
	if cmd == nil || !strings.Contains(stripANSI(model.View()), "No changes") {
		t.Fatalf("alt+d in a clean tree must say so:\n%s", model.View())
	}
	msg, ok := cmd().(noticeExpiredMsg)
	if !ok {
		t.Fatal("the message must expire by itself (after noticeDuration)")
	}
	if noticeDuration != 3*time.Second {
		t.Fatalf("the message stays %v, want 3s", noticeDuration)
	}
	updated, _ = model.Update(msg)
	if strings.Contains(stripANSI(updated.(ProjectModel).View()), "No changes") {
		t.Fatal("after the time the message must be gone")
	}
}

func TestDiff_ExpiryKeepsANewerMessage(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.say("something newer", false)
	updated, _ := model.Update(noticeExpiredMsg{text: "No changes: the working tree is clean"})
	if got := updated.(ProjectModel); got.status != "something newer" {
		t.Fatalf("a newer message must stay: %q", got.status)
	}
}

func TestDiff_OpensWhenThereAreChanges(t *testing.T) {
	dir := gitRepoWithCommit(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	model, err := NewProjectModel(dir, "", project.Session{}, ProjectHooks{Diff: func(string) *exec.Cmd { called = true; return exec.Command("true") }})
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := model.showDiff()
	if cmd == nil || !called {
		t.Fatal("with changes the diff must open")
	}
}

func TestTerminalPanel_OpensUnderEditorTakesKeysAndCloses(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "read w; echo got-$w; sleep 30") }
	model, _ = runPalette(t, model, "Show or hide the terminal")
	if len(model.terms) == 0 || model.focus != focusTerminal || !strings.Contains(stripANSI(model.View()), "TERMINAL") {
		t.Fatalf("the terminal must open in a panel:\n%s", model.View())
	}
	t.Cleanup(model.closeTerminal)
	if lines := strings.Split(model.View(), "\n"); len(lines) != model.height {
		t.Fatalf("the panel must fit the window: %d lines", len(lines))
	}
	model, _ = projectKeys(model, "x", "y", "enter")
	deadline := time.After(5 * time.Second)
	for !strings.Contains(stripANSI(model.View()), "got-xy") {
		select {
		case <-model.activeTerm().Updates():
		case <-deadline:
			t.Fatalf("the shell never answered:\n%s", model.View())
		}
	}
	model, _ = projectKeys(model, "ctrl+]")
	if model.focus != focusProjectEdit || model.termPanelHeight() == 0 {
		t.Fatal("ctrl+] must give the focus back and keep the panel")
	}
	model, _ = runPalette(t, model, "Show or hide the terminal") // focuses the running shell again
	if model.focus != focusTerminal {
		t.Fatal("the button must focus the shell")
	}
	model, _ = runPalette(t, model, "Show or hide the terminal") // and hides it
	if model.termShown || len(model.terms) == 0 {
		t.Fatal("a second click must hide the panel but keep the shell")
	}
}

func TestKeyBytes(t *testing.T) {
	tests := map[string]struct {
		msg  tea.KeyMsg
		want string
	}{
		"text":  {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("é")}, "é"},
		"enter": {tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		"ctrl":  {tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		"up":    {tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		"alt":   {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, "\x1bb"},
		"back":  {tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
	}
	for name, tc := range tests {
		if got := string(keyBytes(tc.msg)); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestShortcuts_CtrlPOpensProjectsAndCtrlTTerminal(t *testing.T) {
	for name, start := range map[string]func(ProjectModel) ProjectModel{
		"tree":   func(m ProjectModel) ProjectModel { return m },
		"editor": inEditor,
	} {
		model, _ := newProjectFixture(t, 100, sampleFiles)
		model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
		model = start(model)
		model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlO})
		if model.picker == nil {
			// the editor hands the key over as a command: run it
			updated, _ := model.runProjectCommand("project")
			model = updated.(ProjectModel)
		}
		if model.picker == nil {
			t.Fatalf("%s: ctrl+o must open the project list", name)
		}
	}
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT}) // not projectSend: it would wait for the shell to end
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	if len(model.terms) == 0 || model.focus != focusTerminal {
		t.Fatal("ctrl+t must open the terminal")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if model = updated.(ProjectModel); model.termShown || model.focus == focusTerminal {
		t.Fatal("ctrl+t in the terminal must hide it")
	}
}

func TestTerminalTabs_OpenSwitchAndClose(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	send := func(msg tea.Msg) {
		updated, _ := model.Update(msg)
		model = updated.(ProjectModel)
	}
	t.Cleanup(model.closeTerminal)
	send(tea.KeyMsg{Type: tea.KeyCtrlT})
	for i := 0; i < 2; i++ { // another shell tab comes from the tab list
		send(keyMsg("alt+t"))
		for _, r := range "new terminal" {
			send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		send(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if len(model.terms) != 3 || model.termActive != 2 {
		t.Fatalf("New terminal in the tab list must open tabs: %d tabs, active %d", len(model.terms), model.termActive)
	}
	header := stripANSI(strings.Split(model.View(), "\n")[1+model.editorHeight()])
	for _, want := range []string{"1:sh", "2:sh", "3:sh", "[+]"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header lacks %q: %q", want, header)
		}
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1"), Alt: true})
	if model.termActive != 0 {
		t.Fatalf("alt+1 must show the first tab: %d", model.termActive)
	}
	x := 0 // a click on the header's tab 2
	for _, r := range model.termHeader().regions {
		if r.keys[0] == actionSelectTab+"1" {
			x = model.editorOffset() + r.from + 1
		}
	}
	send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: topBarHeight + model.editorHeight()})
	if model.termActive != 1 {
		t.Fatalf("a click must choose the tab: %d", model.termActive)
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	if len(model.terms) != 2 || model.termActive != 1 {
		t.Fatalf("alt+w must close the shown tab: %d tabs, active %d", len(model.terms), model.termActive)
	}
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w"), Alt: true})
	if len(model.terms) != 0 || model.termPanelHeight() != 0 || model.focus == focusTerminal {
		t.Fatalf("closing the last tab must remove the panel: %d tabs", len(model.terms))
	}
}

func TestClose_CtrlCDoesNotCloseFromTheTree(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if isQuit(cmd) || updated.(ProjectModel).dlg != nil {
		t.Fatal("ctrl+c must neither close nor ask")
	}
}

func TestClose_CtrlDAlwaysAsksFirst(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlD} {
		model, _ := newProjectFixture(t, 100, sampleFiles) // the tree has the focus, nothing is modified
		updated, cmd := model.Update(tea.KeyMsg{Type: key})
		asked := updated.(ProjectModel)
		if isQuit(cmd) || asked.dlg == nil || !strings.Contains(stripANSI(asked.View()), "Quit Jelly-Dev?") {
			t.Fatalf("key %v in the tree must ask, even with no changes:\n%s", key, asked.View())
		}
		stays, cmd := projectKeys(asked, "n")
		if isQuit(cmd) || stays.dlg != nil {
			t.Fatalf("no keeps the window open")
		}
		if _, cmd = projectKeys(asked, "y"); !isQuit(cmd) {
			t.Fatalf("yes closes the window")
		}
		if _, cmd = asked.Update(tea.KeyMsg{Type: key}); !isQuit(cmd) {
			t.Fatalf("the same key again closes the window")
		}
	}
}

func TestClose_ChangesAskWhatToDoWithThemInsteadOfJustClosing(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter", "x") // README.md modified, in the editor
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = settleProject(updated.(ProjectModel), cmd)
	view := stripANSI(model.View())
	if model.dlg == nil || !strings.Contains(view, "Unsaved changes in README.md") || !strings.Contains(view, "Save") {
		t.Fatalf("a file with changes asks save, discard or cancel:\n%s", view)
	}
	if _, cmd = projectKeys(model, "d"); cmd == nil || !isQuit(cmd) {
		// discard on the only tab goes on to quit; the dialog about quitting was this one
		t.Fatalf("discard must go on and close the window")
	}
}

func TestClose_TerminalKeepsCtrlCAndCtrlDForTheShell(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if got := updated.(ProjectModel); got.dlg != nil || isQuit(cmd) {
		t.Fatal("in the terminal ctrl+c belongs to the shell")
	}
}

func TestClose_CtrlDWithSeveralSavedTabsClosesTheShownTabWithoutAsking(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")                              // README.md
	model, _ = projectKeys(model, "ctrl+e", "k", "enter", "j", "j", "enter") // and main.go in a second tab
	if len(model.openEditors()) != 2 {
		t.Fatalf("%d tabs", len(model.openEditors()))
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = settleProject(updated.(ProjectModel), cmd)
	if model.dlg != nil || len(model.openEditors()) != 1 || isQuit(cmd) {
		t.Fatalf("a saved tab closes at once: dialog %v, %d tabs", model.dlg != nil, len(model.openEditors()))
	}
}

func TestClose_CtrlDWithSeveralTabsAsksWhenTheShownTabHasChanges(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	model, _ = projectKeys(model, "ctrl+e", "k", "enter", "j", "j", "enter", "x") // an unsaved change in the second tab
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = settleProject(updated.(ProjectModel), cmd)
	if model.dlg == nil || len(model.openEditors()) != 2 {
		t.Fatalf("unsaved changes must ask what to do first (%d tabs)", len(model.openEditors()))
	}
}

func TestClose_InTheSaveDialogCtrlCAndCtrlDBothCancel(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD} {
		model, _ := newProjectFixture(t, 100, sampleFiles)
		model, _ = projectKeys(model, "G", "enter", "x")
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		model = settleProject(updated.(ProjectModel), cmd)
		if model.dlg == nil {
			t.Fatal("setup: the save dialog must be open")
		}
		after, cmd := model.Update(tea.KeyMsg{Type: key})
		got := after.(ProjectModel)
		if got.dlg != nil || isQuit(cmd) || !got.Editor().Modified() {
			t.Fatalf("key %v in the save dialog must cancel and keep the changes", key)
		}
	}
}

func TestCurrentBranch_ReadsHeadOfRepositoryAndWorktree(t *testing.T) {
	root := t.TempDir()
	if got := currentBranch(root); got != "" {
		t.Fatalf("outside a repository there is no branch, got %q", got)
	}
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(gitDir, "HEAD"), "ref: refs/heads/feature/x\n")
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := currentBranch(sub); got != "feature/x" {
		t.Fatalf("branch = %q", got)
	}
	write(filepath.Join(gitDir, "HEAD"), "0123456789abcdef\n")
	if got := currentBranch(root); got != "0123456" {
		t.Fatalf("detached HEAD = %q", got)
	}
	worktree := t.TempDir()
	write(filepath.Join(worktree, ".git"), "gitdir: "+gitDir+"\n")
	write(filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
	if got := currentBranch(worktree); got != "main" {
		t.Fatalf("worktree branch = %q", got)
	}
}

func TestProject_AltDRunsDiffFromTreeAndEditor(t *testing.T) {
	for _, focus := range []int{focusTree, focusProjectEdit} {
		model, _ := newProjectFixture(t, 100, sampleFiles)
		if err := os.Mkdir(filepath.Join(model.root, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		called := false
		model.hooks.Diff = func(string) *exec.Cmd { called = true; return exec.Command("true") }
		model.focus = focus
		_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true})
		if cmd == nil || !called {
			t.Fatalf("alt+d must start the diff (focus %d)", focus)
		}
	}
}

// cleanRepo makes the project folder of a fixture a git repository whose working tree matches HEAD.
func cleanRepo(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestAltD_SaysThereAreNoChangesForThreeSeconds_WhenTheTreeIsClean(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	cleanRepo(t, model.root)
	called := false
	model.hooks.Diff = func(string) *exec.Cmd { called = true; return exec.Command("true") }
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true})
	model = updated.(ProjectModel)
	if called {
		t.Fatal("with nothing to show the diff view must not open")
	}
	if !strings.Contains(stripANSI(model.View()), "No changes") {
		t.Fatalf("the message must be shown:\n%s", model.View())
	}
	if cmd == nil {
		t.Fatal("the message needs a timer that removes it")
	}
	if noticeDuration != 3*time.Second {
		t.Fatalf("the message stays %v, want 3 seconds", noticeDuration)
	}
	updated, _ = model.Update(noticeExpiredMsg{text: "No changes: the working tree is clean"})
	if strings.Contains(stripANSI(updated.(ProjectModel).View()), "No changes") {
		t.Fatal("the message must be gone when the time is over")
	}
}

func TestAltD_NewerMessageSurvivesTheTimerOfAnOlderOne(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.notice("Tree refreshed", false)
	updated, _ := model.Update(noticeExpiredMsg{text: "No changes: the working tree is clean"})
	if got := updated.(ProjectModel).status; got != "Tree refreshed" {
		t.Fatalf("a message that came later must stay: %q", got)
	}
}

func TestAltD_ShowsTheDiff_WhenThereAreChanges(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	cleanRepo(t, model.root)
	if err := os.WriteFile(filepath.Join(model.root, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	model.hooks.Diff = func(string) *exec.Cmd { called = true; return exec.Command("true") }
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true})
	if cmd == nil || !called {
		t.Fatal("with changes the diff must open")
	}
}

func TestTerminalShell_PrefersPowerShellOnWindows_AndKeepsTheUsersChoice(t *testing.T) {
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	found := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, n := range names {
				if n == name {
					return `C:\bin\` + name, nil
				}
			}
			return "", exec.ErrNotFound
		}
	}
	cases := []struct {
		name, goos string
		env        map[string]string
		have       []string
		want       string
		args       int
	}{
		{"pwsh first", "windows", nil, []string{"pwsh.exe", "powershell.exe"}, `C:\bin\pwsh.exe`, 1},
		{"Windows PowerShell next", "windows", nil, []string{"powershell.exe"}, `C:\bin\powershell.exe`, 1},
		{"ComSpec last", "windows", map[string]string{"ComSpec": `C:\Windows\cmd.exe`}, nil, `C:\Windows\cmd.exe`, 0},
		{"cmd.exe without ComSpec", "windows", nil, nil, "cmd.exe", 0},
		{"SHELL wins", "windows", map[string]string{"SHELL": `C:\git\bash.exe`}, []string{"pwsh.exe"}, `C:\git\bash.exe`, 0},
		{"sh elsewhere", "linux", nil, []string{"pwsh.exe"}, "/bin/sh", 0},
	}
	for _, c := range cases {
		shell, args := terminalShell(c.goos, env(c.env), found(c.have...))
		if c.args == 1 && (len(args) != 1 || args[0] != "-NoLogo") {
			t.Errorf("%s: the arguments are %v, want -NoLogo", c.name, args)
		}
		if shell != c.want || len(args) != c.args {
			t.Errorf("%s: shell %q args %v, want %q with %d arg(s)", c.name, shell, args, c.want, c.args)
		}
	}
}
