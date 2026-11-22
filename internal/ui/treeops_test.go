package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestTreeFooter_HasNoHints(t *testing.T) {
	model, _ := newProjectFixture(t, 120, sampleFiles)
	footer := stripANSI(strings.Join(model.treeFooterRows(model.treeFooterWidth()), "\n"))
	for _, gone := range []string{"[", "commands", "[n]", "[d]", "[e]"} {
		if strings.Contains(footer, gone) {
			t.Errorf("%q must not be in the tree footer: %q", gone, footer)
		}
	}
}

func TestTree_NCreatesAFileInTheSelectedFolderAndOpensIt(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "n", "notes/new.txt", "enter") // src/ is the first row and selected: the file goes inside it
	path := filepath.Join(fx.root, "src", "notes", "new.txt")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
	if model.name != nil || model.focus != focusProjectEdit || model.Editor().Path() != path {
		t.Fatalf("prompt %v focus %d path %q", model.name, model.focus, model.Editor().Path())
	}
}

func TestTree_NWithATrailingSlashCreatesAFolder(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "n", "docs/", "enter") // README.md is selected: next to it, in the root
	if info, err := os.Stat(filepath.Join(fx.root, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("the folder must exist: %v", err)
	}
	if model.name != nil {
		t.Fatal("the prompt closes")
	}
}

func TestTree_NRefusesBadNamesAndKeepsThePromptOpen(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	outside := filepath.Join(filepath.Dir(fx.root), "escaped.txt")
	for _, name := range []string{"../escaped.txt", "../../escaped.txt", "/etc/jd-escaped", "", "README.md"} {
		m, _ := projectKeys(model, "G", "n")
		if name != "" {
			m, _ = projectKeys(m, name)
		}
		m, _ = projectKeys(m, "enter")
		if m.name == nil || !m.warn || !strings.Contains(m.status, "Cannot create") {
			t.Errorf("%q must be refused with the prompt still open: prompt %v status %q", name, m.name, m.status)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a name with .. must not create a file outside the project")
	}
	if got, _ := os.ReadFile(filepath.Join(fx.root, "README.md")); string(got) != "# hi\n" {
		t.Fatalf("an existing file must not be overwritten: %q", got)
	}
}

func TestTree_NRefusesASymlinkThatLeavesTheProject(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(fx.root, "link")); err != nil {
		t.Skip("no symbolic links here")
	}
	model.tree.Refresh()
	m, _ := projectKeys(model, "G", "n", "link/evil.txt", "enter")
	if _, err := os.Stat(filepath.Join(elsewhere, "evil.txt")); err == nil {
		t.Fatal("a file must not be created through a link out of the project")
	}
	if m.name == nil {
		t.Fatal("the name is refused: the prompt stays")
	}
}

func TestTree_NPromptEditsAndEscCancels(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "n", "abc")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(ProjectModel)
	if got := string(model.name.input); got != "ab" {
		t.Fatalf("backspace: %q", got)
	}
	if !strings.Contains(stripANSI(model.View()), "New file") {
		t.Fatal("the prompt is drawn")
	}
	model, _ = projectKeys(model, "esc")
	if model.name != nil {
		t.Fatal("esc closes the prompt")
	}
}

func TestTree_DAsksAndOnlyYesDeletes(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	readme := filepath.Join(fx.root, "README.md")
	model, _ = projectKeys(model, "G", "d")
	if model.dlg == nil || !strings.Contains(model.dlg.title, "README.md") || model.dlg.choice != 1 {
		t.Fatalf("a question that starts on No: %+v", model.dlg)
	}
	for _, key := range []string{"enter", "n", "esc"} {
		kept, _ := projectKeys(model, key)
		if _, err := os.Stat(readme); err != nil || kept.dlg != nil {
			t.Fatalf("%s must keep the file and close the question: %v", key, err)
		}
	}
	ctrlD, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if _, err := os.Stat(readme); err != nil || ctrlD.(ProjectModel).dlg != nil {
		t.Fatal("ctrl+d must not confirm a deletion")
	}
	model, _ = projectKeys(model, "y")
	if _, err := os.Stat(readme); err == nil {
		t.Fatal("y deletes the file")
	}
	if !strings.Contains(model.status, "Deleted") {
		t.Fatalf("status %q", model.status)
	}
}

func TestTree_DDeletesAFolderAndClosesItsTabs(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "enter", "j", "j", "enter") // src opens, main.go opens in the editor
	if model.Editor().Path() == "" {
		t.Fatal("a file is open")
	}
	model, _ = projectKeys(model, "ctrl+e", "g", "d", "y") // back to the tree, the first row is src/
	if _, err := os.Stat(filepath.Join(fx.root, "src")); err == nil {
		t.Fatal("the folder must be gone")
	}
	if model.Editor().Path() != "" {
		t.Fatalf("the tab of a deleted file closes: %q", model.Editor().Path())
	}
}

func TestTree_DRefusesTheProjectFolder(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model.tree.Root.Expanded = true
	m := model.askToDelete(model.tree.Root)
	if m.dlg != nil || !m.warn {
		t.Fatal("the project folder cannot be deleted")
	}
	if _, err := os.Stat(fx.root); err != nil {
		t.Fatal(err)
	}
}

func TestTree_EOpensTheSelectedFile(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "e")
	if filepath.Base(model.Editor().Path()) != "README.md" || model.focus != focusProjectEdit {
		t.Fatalf("path %q focus %d", model.Editor().Path(), model.focus)
	}
}

func TestTree_NCreatesInTheContextOfTheSelection(t *testing.T) {
	// on a sub folder: inside it; on a file of a sub folder: next to that file; on a file in the root: in the root
	for name, tc := range map[string]struct {
		keys []string
		want string
	}{
		"folder":         {[]string{"n", "a.txt", "enter"}, filepath.Join("src", "a.txt")},                       // src/ is the first row
		"nested folder":  {[]string{"enter", "j", "n", "b.txt", "enter"}, filepath.Join("src", "util", "b.txt")}, // src opened, util/ is below it
		"file in folder": {[]string{"enter", "j", "j", "n", "c.txt", "enter"}, filepath.Join("src", "c.txt")},    // main.go of src
		"file in root":   {[]string{"G", "n", "d.txt", "enter"}, "d.txt"},                                        // README.md
	} {
		model, fx := newProjectFixture(t, 100, sampleFiles)
		_, _ = projectKeys(model, tc.keys...)
		if _, err := os.Stat(filepath.Join(fx.root, tc.want)); err != nil {
			t.Errorf("%s: the file must be created at %s: %v", name, tc.want, err)
		}
	}
}

func TestTree_NPromptNamesTheFolderItCreatesIn(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "n") // src/ is selected
	if view := stripANSI(model.View()); !strings.Contains(view, "New file in src") {
		t.Fatalf("the prompt says where the file goes:\n%s", view)
	}
}

func TestTree_DRemovesOnlyTheLinkOfASymlinkedFolder(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fx.root, "link")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skip("no symbolic links here")
	}
	model.tree.Refresh()
	node := model.tree.Visible()[indexOfPath(model, link)]
	asked := model.askToDelete(node)
	if asked.dlg == nil || !strings.Contains(asked.dlg.title, "link") {
		t.Fatalf("the question says it is a link: %+v", asked.dlg)
	}
	asked.deletePath(link)
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("the link must be gone")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "keep.txt")); err != nil {
		t.Fatalf("the target of the link must stay: %v", err)
	}
}

func TestTree_DRefusesToDeleteThroughALinkOutOfTheProject(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	elsewhere := t.TempDir()
	victim := filepath.Join(elsewhere, "important.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fx.root, "link")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skip("no symbolic links here")
	}
	model.tree.Refresh()
	model.tree.Expand(model.tree.Visible()[indexOfPath(model, link)])
	node := model.tree.Visible()[indexOfPath(model, filepath.Join(link, "important.txt"))]
	m := model.askToDelete(node)
	if m.dlg != nil || !m.warn || !strings.Contains(m.status, "outside the project") {
		t.Fatalf("the deletion must be refused: dlg %v status %q", m.dlg, m.status)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the file outside the project must stay: %v", err)
	}
}

func TestTree_DWarnsAboutUnsavedChangesOfAnOpenFile(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "e", "A", "!", "esc", "ctrl+e") // README.md is open and modified, back in the tree
	model, _ = projectKeys(model, "G", "d")
	if model.dlg == nil || !strings.Contains(model.dlg.title, "unsaved changes") {
		t.Fatalf("the question mentions the unsaved changes: %+v", model.dlg)
	}
}

// indexOfPath is the row of the node with this path, failing the test by returning an out of range index when there is none.
func indexOfPath(model ProjectModel, path string) int {
	for i, row := range model.tree.Visible() {
		if row.Path == path {
			return i
		}
	}
	return -1
}

func TestTree_NTabSwitchesBetweenTheSelectedFolderAndTheProjectFolder(t *testing.T) {
	// a project with folders only: every selection is a folder, tab is the way to the root
	model, fx := newProjectFixture(t, 100, map[string]string{"a/x.txt": "1", "b/y.txt": "2"})
	model, _ = projectKeys(model, "n")
	if model.name.dir != filepath.Join(fx.root, "a") || !strings.Contains(stripANSI(model.View()), "New file in a") {
		t.Fatalf("the prompt starts in the selected folder: %q", model.name.dir)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(ProjectModel)
	if model.name.dir != fx.root || !strings.Contains(stripANSI(model.View()), "New file in the project folder") {
		t.Fatalf("tab moves to the project folder: %q", model.name.dir)
	}
	model, _ = projectKeys(model, "top.txt", "enter")
	if _, err := os.Stat(filepath.Join(fx.root, "top.txt")); err != nil {
		t.Fatalf("the file is in the root: %v", err)
	}
	// tab twice goes back to the selected folder
	again, _ := newProjectFixture(t, 100, map[string]string{"a/x.txt": "1", "b/y.txt": "2"})
	again, _ = projectKeys(again, "n")
	for i := 0; i < 2; i++ {
		u, _ := again.Update(tea.KeyMsg{Type: tea.KeyTab})
		again = u.(ProjectModel)
	}
	if again.name.dir != again.name.home {
		t.Fatalf("a second tab returns to the selected folder: %q", again.name.dir)
	}
}

func TestTree_NPromptHintFitsTheDialogAndOffersTabOnlyWhenUseful(t *testing.T) {
	model, fx := newProjectFixture(t, 100, map[string]string{"a/x.txt": "1"})
	inFolder, _ := projectKeys(model, "n") // a/ is selected
	hint := inFolder.nameHint()
	if got := lipgloss.Width(hint); got > dialogBox {
		t.Fatalf("the hint is %d columns wide, the box %d: %q", got, dialogBox, hint)
	}
	for _, want := range []string{"tab", "name/"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint lacks %q: %q", want, hint)
		}
	}
	if !strings.Contains(stripANSI(inFolder.View()), "name/ = folder") {
		t.Fatal("the folder hint is on the screen, not cut off")
	}
	atRoot := inFolder
	atRoot.name = &namePrompt{dir: fx.root, home: fx.root}
	if strings.Contains(atRoot.nameHint(), "tab") {
		t.Fatalf("tab has nowhere to go when the root is the target: %q", atRoot.nameHint())
	}
}
