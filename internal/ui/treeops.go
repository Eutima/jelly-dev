package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/filetree"
)

// namePrompt asks for a file name under a directory of the tree.
type namePrompt struct {
	dir   string // where the new file goes
	input []rune
	home  string // the folder of the selection, which tab goes back to
}

// errOutsideProject is returned for a name that would leave the project folder.
var errOutsideProject = errors.New("the name leaves the project folder")

// resolveNewPath returns the absolute path for name typed in the prompt of dir and whether it is a folder (the name ends with a
// slash). The path must stay inside the project, also through symbolic links of its parent folders.
func (m ProjectModel) resolveNewPath(dir, name string) (path string, folder bool, err error) {
	name = strings.TrimSpace(name)
	folder = strings.HasSuffix(name, "/")
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		if name == "" || clean == "." {
			return "", false, errors.New("type a name")
		}
		return "", false, errOutsideProject
	}
	path = filepath.Join(dir, clean)
	if err := m.insideProject(filepath.Dir(path)); err != nil {
		return "", false, err
	}
	return path, folder, nil
}

// insideProject checks that folder (or, when it does not exist yet, its nearest existing parent) really lies in the project, also
// through symbolic links.
func (m ProjectModel) insideProject(folder string) error {
	root, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return err
	}
	existing := folder
	for {
		if _, statErr := os.Lstat(existing); statErr == nil || filepath.Dir(existing) == existing {
			break
		}
		existing = filepath.Dir(existing)
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	if rel, relErr := filepath.Rel(root, real); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errOutsideProject
	}
	return nil
}

// createEntry makes the file (empty) or folder; an existing name is refused.
func createEntry(path string, folder bool) error {
	if folder {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s exists already", filepath.Base(path))
		}
		return os.MkdirAll(path, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G304 -- inside the project, checked by resolveNewPath
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s exists already", filepath.Base(path))
		}
		return err
	}
	return file.Close()
}

// startCreate opens the prompt for a new file next to the selection (inside it, for a folder).
func (m ProjectModel) startCreate(node *filetree.Node) ProjectModel {
	dir := m.root
	if node != nil {
		dir = m.dirOf(node)
	}
	m.name = &namePrompt{dir: dir, home: dir}
	return m
}

// nameKey handles a key while the name prompt is open.
func (m ProjectModel) nameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	prompt := *m.name
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlD:
		m.name = nil
		return m, nil
	case tea.KeyTab: // the folder of the selection or the project folder: a tree of folders only has no other way to the root
		if prompt.dir == m.root {
			prompt.dir = prompt.home
		} else {
			prompt.dir = m.root
		}
	case tea.KeyBackspace:
		if len(prompt.input) > 0 {
			prompt.input = prompt.input[:len(prompt.input)-1]
		}
	case tea.KeyEnter:
		return m.finishCreate(prompt)
	case tea.KeyRunes, tea.KeySpace:
		prompt.input = append(append([]rune(nil), prompt.input...), msg.Runes...)
	}
	m.name = &prompt
	return m, nil
}

// finishCreate makes what the prompt describes and opens a new file in the editor.
func (m ProjectModel) finishCreate(prompt namePrompt) (tea.Model, tea.Cmd) {
	path, folder, err := m.resolveNewPath(prompt.dir, string(prompt.input))
	if err == nil {
		err = createEntry(path, folder)
	}
	if err != nil {
		m.notice("Cannot create: "+err.Error(), true) // the prompt stays open to fix the name
		return m, nil
	}
	m.name = nil
	m.tree.Refresh()
	m.revealInTree(path)
	if folder {
		m.notice("Created folder "+m.relative(path), false)
		return m, nil
	}
	if err := m.openFile(path, false); err != nil {
		m.notice(err.Error(), true)
		return m, nil
	}
	m.focus = focusProjectEdit
	m.notice("Created "+m.relative(path), false)
	return m.resizeEditor(), nil
}

// revealInTree selects the node of path.
func (m *ProjectModel) revealInTree(path string) {
	if node := m.tree.Reveal(path); node != nil {
		for i, row := range m.tree.Visible() {
			if row == node {
				m.sel = i
			}
		}
		m.keepSelectionVisible()
	}
}

// askToDelete opens the question before a file or folder is removed; the answer starts on "No".
func (m ProjectModel) askToDelete(node *filetree.Node) ProjectModel {
	if node == nil {
		return m
	}
	if node.Path == m.tree.Root.Path {
		m.notice("The project folder itself cannot be deleted here", true)
		return m
	}
	path := node.Path
	if err := m.insideProject(filepath.Dir(path)); err != nil { // a folder that is a link out of the project is not ours to empty
		m.notice("Cannot delete "+m.relative(path)+": it is outside the project (reached through a link)", true)
		return m
	}
	what := "file"
	switch info, err := os.Lstat(path); {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		what = "link (the target stays)"
	case node.Dir:
		what = "folder and everything in it"
	}
	if i := m.modifiedTabUnder(path); i >= 0 {
		what += ", its unsaved changes in " + m.tabNames()[i] + " are lost,"
	}
	m.dlg = &unsavedDialog{
		title:   "Delete " + what + " " + m.relative(path) + "?",
		hint:    " y deletes, n or esc keeps it (this cannot be undone)",
		buttons: closeButtons,
		choice:  1,
		strict:  true,
		then:    func(next ProjectModel) (tea.Model, tea.Cmd) { return next.deletePath(path) },
	}
	return m
}

// modifiedTabUnder returns the index of an open tab with unsaved changes whose file is path or lies in it, -1 when there is none.
func (m ProjectModel) modifiedTabUnder(path string) int {
	for i, open := range m.openEditors() {
		if file := open.Path(); file != "" && open.Modified() && (file == path || strings.HasPrefix(file, path+string(filepath.Separator))) {
			return i
		}
	}
	return -1
}

// deletePath removes a file or folder (a symbolic link only as the link) and closes the tabs of what is gone.
func (m ProjectModel) deletePath(path string) (tea.Model, tea.Cmd) {
	if err := os.RemoveAll(path); err != nil {
		m.notice("Cannot delete: "+err.Error(), true)
		return m, nil
	}
	for i := len(m.openEditors()) - 1; i >= 0; i-- {
		if open := m.openEditors()[i].Path(); open != "" && (open == path || strings.HasPrefix(open, path+string(filepath.Separator))) {
			m.closeTab(i)
		}
	}
	m.tree.Refresh()
	m.sel = max(min(m.sel, len(m.tree.Visible())-1), 0)
	m.keepSelectionVisible()
	m.notice("Deleted "+m.relative(path), false)
	return m, nil
}

// nameView draws the name prompt over the middle of the screen.
func (m ProjectModel) nameView(screen string) string {
	title := "New file in " + m.relative(m.name.dir)
	if m.name.dir == m.root {
		title = "New file in the project folder"
	}
	rows := []string{
		m.theme.Header.Render(padRight(" "+sanitize(title), min(dialogBox, m.width))),
		m.theme.Equal.Render(padRight(" "+sanitize(string(m.name.input))+"█", min(dialogBox, m.width))),
		m.theme.Footer.Render(padRight(m.nameHint(), min(dialogBox, m.width))),
	}
	return m.overlayRows(screen, rows)
}

// nameHint is the key line of the name prompt; it must fit in the dialog box. Tab is only offered when it has somewhere to go.
func (m ProjectModel) nameHint() string {
	if m.name.home == m.root {
		return " enter ok, esc cancel, name/ = folder"
	}
	return " enter ok, tab folder, esc cancel, name/ = folder"
}
