package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/editor"
	"github.com/eutima/jelly-dev/internal/project"
)

// The keys of the regions of the tab bar.
const (
	actionSelectFile = "file:"
	actionCloseFile  = "closefile:"
)

// newEditor builds the editor of a tab.
func (m ProjectModel) newEditor(doc editor.Document) EditorModel {
	return NewEditorModel(doc).WithForwardedCommands().WithNavigator(m.hooks.Navigator, m.root).
		WithClipboard(m.hooks.ReadClipboard, m.hooks.WriteClipboard).WithTheme(m.theme)
}

// openEditors returns the editors of all tabs, the shown one up to date.
func (m ProjectModel) openEditors() []EditorModel {
	all := slices.Clone(m.files)
	if m.tab >= 0 && m.tab < len(all) {
		all[m.tab] = m.editor
	}
	return all
}

// tabIndex returns the tab that shows path, -1 when it is not open.
func (m ProjectModel) tabIndex(path string) int {
	if path == "" {
		return -1
	}
	for i, e := range m.openEditors() {
		if e.Path() == path {
			return i
		}
	}
	return -1
}

// selectTab shows tab i.
func (m *ProjectModel) selectTab(i int) {
	all := m.openEditors()
	if i < 0 || i >= len(all) {
		return
	}
	m.files, m.tab, m.editor = all, i, all[i]
	*m = m.resizeEditor()
}

// addTab opens doc in a new tab and shows it.
func (m *ProjectModel) addTab(doc editor.Document) {
	m.files = append(m.openEditors(), m.newEditor(doc))
	m.tab, m.editor = len(m.files)-1, m.files[len(m.files)-1]
	*m = m.resizeEditor()
}

// closeTab closes tab i without asking; the last tab is replaced by an empty buffer.
func (m *ProjectModel) closeTab(i int) {
	all := m.openEditors()
	if i < 0 || i >= len(all) {
		return
	}
	all = slices.Delete(all, i, i+1)
	if len(all) == 0 {
		all = []EditorModel{m.newEditor(editor.Document{LineEnding: "\n", FinalNewline: true, New: true})}
		all[0].doc.New = true
	}
	next := m.tab
	if i < m.tab || m.tab >= len(all) {
		next = max(min(m.tab-1, len(all)-1), 0)
	}
	m.files, m.tab, m.editor = all, next, all[next]
	*m = m.resizeEditor()
}

// closeTabAsking closes tab i; changes in it ask what to do first.
func (m ProjectModel) closeTabAsking(i int) (tea.Model, tea.Cmd) {
	m.selectTab(i)
	if blocked, asked := m.confirmUnsaved(func(next ProjectModel) (tea.Model, tea.Cmd) {
		next.closeTab(next.tab)
		return next, nil
	}); asked {
		return blocked, nil
	}
	m.closeTab(m.tab)
	return m, nil
}

// askToClose handles ctrl+c and ctrl+d (outside insert mode and the terminal). From the editor with several tabs it closes the shown
// tab at once (changes in it ask what to do with them first); closing the window always asks.
func (m ProjectModel) askToClose(fromEditor bool) (tea.Model, tea.Cmd) {
	if !fromEditor || len(m.files) <= 1 {
		return m.quitAsking(false)
	}
	closeIt := func(next ProjectModel) (tea.Model, tea.Cmd) {
		next.closeTab(next.tab)
		return next, nil
	}
	if blocked, asked := m.confirmUnsaved(closeIt); asked {
		return blocked, nil
	}
	return closeIt(m)
}

// quitAsking leaves the window after asking: first about every tab with changes, then (unless that was asked already)
// whether to quit at all.
func (m ProjectModel) quitAsking(asked bool) (tea.Model, tea.Cmd) {
	if i := m.firstModified(); i >= 0 {
		m.selectTab(i)
		if blocked, ok := m.confirmUnsaved(func(next ProjectModel) (tea.Model, tea.Cmd) { return next.quitAsking(true) }); ok {
			return blocked, nil
		}
	}
	if !asked {
		return m.confirmClose("Quit Jelly-Dev?", func(next ProjectModel) (tea.Model, tea.Cmd) { return next.quit() }), nil
	}
	return m.quit()
}

// stepTab shows the next (direction 1) or previous (-1) tab, wrapping around.
func (m ProjectModel) stepTab(direction int) (tea.Model, tea.Cmd) {
	if count := len(m.files); count > 1 {
		m.selectTab(((m.tab+direction)%count + count) % count)
	}
	return m, nil
}

// firstModified returns the first tab with unsaved changes, -1 when there is none.
func (m ProjectModel) firstModified() int {
	for i, e := range m.openEditors() {
		if e.Modified() {
			return i
		}
	}
	return -1
}

// tabNames returns the titles of the tabs: the file name, the path in the project when two files share a name.
func (m ProjectModel) tabNames() []string {
	all := m.openEditors()
	names := make([]string, len(all))
	for i, e := range all {
		path := e.Path()
		names[i] = "[No Name]"
		if path == "" {
			continue
		}
		names[i] = filepath.Base(path)
		for j, other := range all {
			if j != i && other.Path() != "" && filepath.Base(other.Path()) == names[i] {
				names[i] = m.relative(path)
				break
			}
		}
	}
	return names
}

// tabBar is the line above the editor: one tab per open file, the shown one highlighted; a click on the name shows the
// file, a click on the × closes it. Tabs that do not fit scroll out on the left.
func (m ProjectModel) tabBar(width int) *bar {
	all, names := m.openEditors(), m.tabNames()
	labels := make([]string, len(all))
	total := 0
	for i, e := range all {
		labels[i] = " " + sanitize(names[i])
		if e.Modified() {
			labels[i] += " [+]"
		}
		total += len([]rune(labels[i])) + 3
	}
	first := 0
	for ; first < m.tab && total > width; first++ { // drop tabs on the left until the shown one fits
		total -= len([]rune(labels[first])) + 3
	}
	result := &bar{}
	for i := first; i < len(all); i++ {
		style := m.theme.Footer
		if i == m.tab {
			style = m.theme.Header
		}
		result.click([]string{actionSelectFile + strconv.Itoa(i)}, barPart{labels[i], style})
		result.click([]string{actionCloseFile + strconv.Itoa(i)}, barPart{" × ", style})
	}
	return result
}

// clickTabBar handles a click on column x of the tab bar.
func (m ProjectModel) clickTabBar(x int) (tea.Model, tea.Cmd) {
	keys := m.tabBar(m.editorWidth()).visibleKeysAt(x, m.editorWidth())
	if len(keys) == 0 {
		return m, nil
	}
	switch {
	case strings.HasPrefix(keys[0], actionSelectFile):
		if i, err := strconv.Atoi(strings.TrimPrefix(keys[0], actionSelectFile)); err == nil {
			m.selectTab(i)
			m.focus = focusProjectEdit
		}
	case strings.HasPrefix(keys[0], actionCloseFile):
		if i, err := strconv.Atoi(strings.TrimPrefix(keys[0], actionCloseFile)); err == nil {
			return m.closeTabAsking(i)
		}
	}
	return m, nil
}

// withTabBar replaces the first line of the editor's view (its own title) by the tab bar.
func (m ProjectModel) withTabBar(view string) string {
	_, rest, found := strings.Cut(view, "\n")
	if !found {
		return view
	}
	return m.tabBar(m.editorWidth()).render(m.editorWidth(), m.theme.Footer) + "\n" + rest
}

// saveAll writes every tab with changes; the first failure stops it and is shown.
func (m ProjectModel) saveAll() ProjectModel {
	saved := 0
	all := m.openEditors()
	for i := range all {
		if !all[i].Modified() {
			continue
		}
		if !all[i].save("") {
			m.files, m.editor = all, all[m.tab]
			m.selectTab(i)
			m.say(m.editor.status, true)
			return m
		}
		saved++
	}
	m.files, m.editor = all, all[m.tab]
	m.say(fmt.Sprintf("%d file(s) written", saved), false)
	return m
}

// session describes what is open for the next start.
func (m ProjectModel) session() project.Session {
	session := project.Session{Cursors: map[string]project.Cursor{}}
	for i, e := range m.openEditors() {
		if e.Path() == "" {
			continue
		}
		rel := m.relative(e.Path())
		row, col := e.Cursor()
		session.Files = append(session.Files, rel)
		session.Cursors[rel] = project.Cursor{Row: row, Col: col}
		if i == m.tab {
			session.Active = rel
		}
	}
	return session
}

// reloadChanged reads the open files again that changed on disk (a merge tool, the diff view or a shell may have written
// them). A tab with unsaved edits keeps its text. The cursor stays on its line. It returns the names of the reloaded files.
func (m *ProjectModel) reloadChanged() []string {
	all := m.openEditors()
	var reloaded []string
	for i, e := range all {
		if e.Path() == "" || e.doc.New || e.Modified() {
			continue
		}
		doc, err := editor.Load(e.Path())
		if err != nil || doc.New || (slices.Equal(doc.Lines, e.doc.Lines) && doc.LineEnding == e.doc.LineEnding) {
			continue
		}
		row, col := e.Cursor()
		e = e.opened(doc).At(min(row, max(len(doc.Lines)-1, 0)), col)
		all[i] = e
		reloaded = append(reloaded, filepath.Base(e.Path()))
	}
	if len(reloaded) > 0 {
		m.files = all
		if m.tab >= 0 && m.tab < len(all) {
			m.editor = all[m.tab]
		}
	}
	return reloaded
}
