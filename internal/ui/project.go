package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/editor"
	"github.com/eutima/jelly-dev/internal/filetree"
	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/terminal"
)

const (
	minTreeWidth     = 18
	maxTreeWidth     = 40
	treePercent      = 25
	minProjectWidth  = 50 // narrower terminals show only the focused pane
	treeHeaderLines  = 1  // the folder name above the tree
	treeFooterBars   = 2  // the hint line wraps to at most this many lines
	treeMessageMax   = 3  // a long message wraps to at most this many lines
	topBarHeight     = 1  // the bar with the project, terminal and diff buttons
	focusTree        = 0
	focusProjectEdit = 1
	focusTerminal    = 2
)

// ProjectHooks connect the project window to the outside world; every hook may be nil.
type ProjectHooks struct {
	// NewWindow opens a directory in a new terminal window.
	NewWindow func(dir string) error
	// SaveSession stores what is open when the window is closed.
	SaveSession func(root string, session project.Session)
	// Navigator answers gd (definition) and gr (references) in the editor; nil disables them.
	Navigator CodeNavigator
	// Tools installs language servers and formatters; nil disables the command :tools.
	Tools ToolInstaller
	// AutoInstall installs the missing tools when the window opens.
	AutoInstall bool
	// Recent lists the recently opened projects, newest first (for the project picker).
	Recent func() []project.Entry
	// Forget removes a project from the recent list.
	Forget func(path string)
	// Open is called when this window switches to another project: it records the project as the most recent one,
	// points the language servers at it and returns what was open in it last time.
	Open func(root string) project.Session
	// ReadClipboard and WriteClipboard connect the editor to the system clipboard; either may be nil.
	ReadClipboard  func() (string, error)
	WriteClipboard func(text string) error
	// Theme resolves a theme name for the command :theme; nil disables the command.
	Theme func(name string) (Theme, bool)
	// Terminal returns the command that opens a shell in dir; nil starts $SHELL.
	Terminal func(dir string) *exec.Cmd
	// Diff returns the command that shows the changes of the project in dir; nil starts `jd git`.
	Diff func(dir string) *exec.Cmd
}

// ProjectModel is the project window: the file tree on the left, the editor on the right.
type ProjectModel struct {
	theme  Theme
	width  int
	height int

	root   string
	tree   *filetree.Tree
	sel    int // selected row of the visible tree rows
	top    int // first tree row shown
	focus  int
	editor EditorModel   // the editor of the shown tab
	files  []EditorModel // the editors of all tabs; the entry of the shown tab is out of date, see openEditors
	tab    int           // the shown tab
	hooks  ProjectHooks

	toolRun       *toolRun            // the running installation of language servers and formatters
	toolStatus    string              // what the installation is doing now, shown while it runs
	picker        *WelcomeModel       // the open list of recent projects, nil when closed
	pickerOutcome *WelcomeOutcome     // what the picker chose
	search        *SearchModel        // the open project search, nil when closed
	knownFiles    []string            // the file list of the last search, shown at once the next time
	dlg           *unsavedDialog      // the question about unsaved changes, nil when none is open
	pick          *formatPick         // the list of formatters to choose from, nil when closed
	langs         *languagePick       // the list of languages (:addlang), nil when closed
	terms         []*terminal.Session // the shells (tabs) of the panel under the editor
	termActive    int                 // the tab that is shown
	termShown     bool                // whether the panel is visible (hidden shells keep running)

	status string
	warn   bool
}

// NewProjectModel opens the project in root with the tree focused. If file is not empty it is opened in the editor
// (a file that does not exist yet starts a new one) and revealed in the tree; session restores the cursor.
func NewProjectModel(root, file string, session project.Session, hooks ProjectHooks) (ProjectModel, error) {
	tree, err := filetree.New(root)
	if err != nil {
		return ProjectModel{}, err
	}
	m := ProjectModel{theme: DarkTheme(), root: tree.Root.Path, tree: tree, hooks: hooks, focus: focusTree}
	m.editor = m.newEditor(editor.Document{LineEnding: "\n", FinalNewline: true, New: true})
	m.editor.doc.New = true
	m.files = []EditorModel{m.editor}
	if file == "" && session.Active != "" {
		file = session.Active
	}
	// the files that were open come back as tabs, the wanted one is shown
	paths := append([]string{}, session.Files...)
	if file != "" {
		paths = append(paths, file)
	}
	wanted := ""
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.root, path)
		}
		if err := m.openFile(path, true); err != nil {
			if path == file || path == filepath.Join(m.root, file) {
				m.notice(err.Error(), true)
			}
			continue
		}
		if cursor, ok := session.Cursors[m.relative(path)]; ok {
			m.editor = m.editor.At(cursor.Row, cursor.Col)
		}
		m.focus = focusProjectEdit
		if file != "" && (path == file || path == filepath.Join(m.root, file)) {
			wanted = path
		}
	}
	if wanted != "" {
		m.selectTab(m.tabIndex(wanted))
	}
	return m, nil
}

// WithTheme returns the model with another theme.
func (m ProjectModel) WithTheme(theme Theme) ProjectModel {
	m.theme = theme
	m.editor = m.editor.WithTheme(theme)
	for i := range m.files {
		m.files[i] = m.files[i].WithTheme(theme)
	}
	return m
}

func (m ProjectModel) relative(path string) string {
	if rel, err := filepath.Rel(m.root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

func (m *ProjectModel) notice(text string, warn bool) { m.status, m.warn = text, warn && text != "" }

// Root returns the project directory.
func (m ProjectModel) Root() string { return m.root }

// Editor returns the editor of the window.
func (m ProjectModel) Editor() EditorModel { return m.editor }

// openFile loads a file into the editor, revealing it in the tree.
func (m *ProjectModel) openFile(path string, _ bool) error {
	if i := m.tabIndex(path); i >= 0 {
		m.selectTab(i) // already open: show its tab
	} else {
		doc, err := editor.Load(path)
		if err != nil {
			return err
		}
		if m.editor.Path() == "" && !m.editor.Modified() { // the empty start buffer makes room for the first file
			m.editor = m.editor.opened(doc)
		} else {
			m.addTab(doc)
		}
	}
	if node := m.tree.Reveal(path); node != nil {
		for i, row := range m.tree.Visible() {
			if row == node {
				m.sel = i
			}
		}
		m.keepSelectionVisible()
	}
	return nil
}

// Init implements tea.Model: it starts checking for missing language servers and formatters.
func (m ProjectModel) Init() tea.Cmd {
	if m.hooks.Tools != nil && m.hooks.AutoInstall {
		return m.checkTools(false)
	}
	return nil
}

// treeWidth is the width of the tree pane (0 when the terminal is too narrow and the editor has the focus).
func (m ProjectModel) treeWidth() int {
	if m.width < minProjectWidth {
		if m.focus == focusTree {
			return m.width
		}
		return 0
	}
	return min(max(m.width*treePercent/100, minTreeWidth), maxTreeWidth)
}

// editorWidth is the width of the editor pane, without the divider.
func (m ProjectModel) editorWidth() int {
	tree := m.treeWidth()
	switch {
	case tree == 0:
		return m.width
	case m.width < minProjectWidth:
		return 0
	}
	return max(m.width-tree-1, 1)
}

func (m ProjectModel) resizeEditor() ProjectModel {
	updated, _ := m.editor.Update(tea.WindowSizeMsg{Width: max(m.editorWidth(), 1), Height: m.editorHeight()})
	m.editor = updated.(EditorModel)
	for _, session := range m.terms {
		session.Resize(max(m.editorWidth(), 1), max(m.termPanelHeight()-1, 1))
	}
	return m
}

// bodyHeight is the height of the panes: the window without the top bar.
func (m ProjectModel) bodyHeight() int { return max(m.height-topBarHeight, 1) }

// treeBody is the number of rows of the tree: the pane without its header and its footer, which takes more lines when the pane is
// narrow and the hints or a message do not fit in one.
func (m ProjectModel) treeBody() int {
	return max(m.bodyHeight()-treeHeaderLines-len(m.treeFooterRows(m.treeFooterWidth())), 1)
}

func (m ProjectModel) treeFooterWidth() int {
	if width := m.treeWidth(); width > 0 {
		return width
	}
	return max(m.width, 1)
}

// treeFooterRows are the lines under the tree: a message or the progress of the tool installation (wrapped at the spaces, not cut
// off), else the hints, wrapped onto a second line when they do not fit.
func (m ProjectModel) treeFooterRows(width int) []string {
	wrapped := func(text string, style lipgloss.Style) []string {
		lines := wrapWords(sanitize(text), max(width-1, 1), treeMessageMax)
		rows := make([]string, len(lines))
		for i, line := range lines {
			rows[i] = style.Render(padRight(" "+line, width))
		}
		return rows
	}
	switch {
	case m.status != "":
		style := m.theme.Footer
		if m.warn {
			style = m.theme.Warning
		}
		return wrapped(m.status, style)
	case m.toolStatus != "":
		return wrapped(m.toolStatus, m.theme.Footer)
	}
	return m.treeFooterBar().renderLines(width, treeFooterBars, m.theme.Footer)
}

func (m *ProjectModel) keepSelectionVisible() {
	body := m.treeBody()
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+body {
		m.top = m.sel - body + 1
	}
	m.top = max(m.top, 0)
}

// Update implements tea.Model.
func (m ProjectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepSelectionVisible()
		if m.search != nil {
			m.search.width, m.search.height = msg.Width, msg.Height
		}
		if m.picker != nil {
			updated, _ := m.picker.Update(msg)
			picker := updated.(WelcomeModel)
			m.picker = &picker
		}
		return m.resizeEditor(), nil
	case toolsPendingMsg:
		return m.startTools(msg)
	case toolDoneMsg:
		return m.toolDone(msg)
	case filesLoadedMsg, searchTickMsg, textResultMsg:
		return m.updateSearch(msg)
	case termUpdateMsg:
		return m.termUpdated(msg)
	case CloseRequestMsg:
		return m.askToClose(true)
	case FormatPickMsg:
		return m.openFormatPicker(msg)
	case languageAddedMsg:
		return m.languageAdded(msg)
	case externalDoneMsg:
		if msg.err != nil {
			m.say(fmt.Sprintf("%s: %v", msg.what, msg.err), true)
		}
		return m, nil
	case ExCommandMsg:
		return m.runProjectCommand(msg.Command)
	case JumpMsg:
		return m.jumpToPlace(msg.Place, msg.Back)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m.forwardToEditor(msg) // answers of the editor's own commands (language server requests)
}

// openPicker opens the list of recent projects over the window.
func (m ProjectModel) openPicker() (tea.Model, tea.Cmd) {
	var entries []project.Entry
	if m.hooks.Recent != nil {
		entries = m.hooks.Recent()
	}
	picker, outcome := NewWelcomeModel(entries, m.hooks.NewWindow, m.hooks.Forget)
	picker = picker.WithEmbedded().WithTheme(m.theme)
	updated, _ := picker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	picker = updated.(WelcomeModel)
	m.picker, m.pickerOutcome = &picker, outcome
	return m, nil
}

// updatePicker passes a message to the open project list; when a project was chosen this window switches to it.
func (m ProjectModel) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.picker.Update(msg)
	picker := updated.(WelcomeModel)
	if !picker.Done() {
		m.picker = &picker
		return m, cmd
	}
	chosen := m.pickerOutcome.Path
	m.picker, m.pickerOutcome = nil, nil
	if chosen == "" || chosen == m.root {
		return m, cmd
	}
	if m.hooks.NewWindow == nil { // no way to open windows here: use this one
		return m.switchProject(chosen, false)
	}
	return m.newWindow(chosen)
}

// switchProject makes this window show another project. Unsaved changes block it unless force is set; what is open in the
// current project is stored first, so it comes back the next time.
func (m ProjectModel) switchProject(root string, force bool) (tea.Model, tea.Cmd) {
	switch {
	case !isDir(root):
		m.say(fmt.Sprintf("E344: Can't find directory \"%s\"", root), true)
		return m, nil
	case m.firstModified() >= 0 && !force:
		m.selectTab(m.firstModified()) // the dialog asks about this file; the next one is asked when the switch is tried again
		if blocked, asked := m.confirmUnsaved(func(next ProjectModel) (tea.Model, tea.Cmd) { return next.switchProject(root, false) }); asked {
			return blocked, nil
		}
		m.say("E37: No write since last change (:w to save, :project! "+root+" to discard)", true)
		return m, nil
	}
	if abs, err := filepath.EvalSymlinks(root); err == nil {
		root = abs
	}
	if root == m.root {
		m.say("Already in this project", false)
		return m, nil
	}
	m.storeSession()
	m.closeTerminal()
	var session project.Session
	if m.hooks.Open != nil {
		session = m.hooks.Open(root)
	}
	next, err := NewProjectModel(root, "", session, m.hooks)
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	next = next.WithTheme(m.theme)
	next.width, next.height = m.width, m.height
	next.status = "Opened " + root
	return next.resizeEditor(), nil
}

// openSearch opens the project search.
func (m ProjectModel) openSearch(mode searchMode, query string) (tea.Model, tea.Cmd) {
	search, cmd := newSearch(m.theme, m.root, mode, query, m.width, m.height, m.knownFiles)
	m.search = &search
	return m, cmd
}

// updateSearch passes a message to the open search and, when it is left, opens what the user chose.
func (m ProjectModel) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.search == nil { // a late answer after the search was closed: keep the file list for the next one
		if loaded, ok := msg.(filesLoadedMsg); ok && loaded.err == nil {
			m.knownFiles = loaded.files
		}
		return m, nil
	}
	search, cmd := m.search.Update(msg)
	m.search = &search
	if !search.done {
		return m, cmd
	}
	m.knownFiles = search.Files()
	m.search = nil
	if search.choice == nil {
		return m, cmd
	}
	m.editor = m.editor.pushJump()
	return m.jumpTo(*search.choice)
}

// jumpTo opens the file of a place (also files outside the project, like library sources) and puts the cursor on it.
func (m ProjectModel) jumpTo(place lsp.Place) (tea.Model, tea.Cmd) {
	return m.jumpToPlace(place, false)
}

// jumpToPlace is jumpTo; back tells that the place came off the jump list, which then gets it back when the file cannot be
// opened (unsaved changes), instead of forgetting the jump that was just made.
func (m ProjectModel) jumpToPlace(place lsp.Place, back bool) (tea.Model, tea.Cmd) {
	jumps := m.editor.jumps // the jump list belongs to the window: it goes along to the file that is shown next
	m.editor.jumps = nil
	if err := m.openFile(place.Path, false); err != nil {
		m.editor.jumps = jumps
		if back {
			m.editor = m.editor.restoreJump(place)
		} else {
			m.editor = m.editor.dropJump()
		}
		m.editor.notice(err.Error(), true)
		return m, nil
	}
	m.editor.jumps = jumps
	m.editor = m.editor.At(place.Row, place.Col)
	m.focus = focusProjectEdit
	return m.resizeEditor(), nil
}

func (m ProjectModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dlg != nil {
		return m.dialogKey(msg)
	}
	if m.pick != nil {
		return m.updateFormatPicker(msg)
	}
	if m.langs != nil {
		return m.updateLanguagePicker(msg)
	}
	if m.picker != nil {
		return m.updatePicker(msg)
	}
	if m.search != nil {
		return m.updateSearch(msg)
	}
	if m.focus == focusTerminal && m.termPanelHeight() > 0 {
		return m.terminalKey(msg)
	}
	if m.focus == focusTree {
		switch msg.String() {
		case "P", "ctrl+p":
			return m.openPicker()
		case "ctrl+t":
			return m.openTerminal()
		case "T":
			return m.openTerminal()
		case "D":
			return m.showDiff()
		case "ctrl+f", "/":
			return m.openSearch(searchFiles, "")
		case "ctrl+g":
			return m.openSearch(searchText, "")
		}
	}
	if msg.String() == "ctrl+e" {
		return m.toggleFocus(), nil
	}
	if m.focus == focusProjectEdit {
		return m.forwardToEditor(msg)
	}
	m.notice("", false)
	rows := m.tree.Visible()
	var node *filetree.Node
	if m.sel >= 0 && m.sel < len(rows) {
		node = rows[m.sel]
	}
	switch msg.String() {
	case "j", "down":
		m.sel++
	case "k", "up":
		m.sel--
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = len(rows) - 1
	case "pgdown":
		m.sel += m.treeBody()
	case "pgup":
		m.sel -= m.treeBody()
	case "enter", "o", "l", "right":
		return m.activate(node, msg.String() != "l" && msg.String() != "right")
	case "h", "left":
		if node != nil && node.Dir && node.Expanded {
			m.tree.Collapse(node)
		} else if node != nil {
			if parent := m.tree.Parent(node); parent != nil {
				m.sel = m.indexOf(parent)
			}
		}
	case "R":
		m.tree.Refresh()
		m.notice("Tree refreshed", false)
	case "w":
		if node != nil {
			return m.newWindow(m.dirOf(node))
		}
	case "tab", "i":
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case "ctrl+c", "ctrl+d":
		return m.askToClose(false)
	case "q":
		if i := m.firstModified(); i >= 0 {
			m.say("E37: unsaved changes in "+m.tabNames()[i]+" (:wa to save all, :qa! to quit without saving)", true)
			return m, nil
		}
		return m.quit()
	}
	m.sel = min(max(m.sel, 0), max(len(m.tree.Visible())-1, 0))
	m.keepSelectionVisible()
	return m, nil
}

func (m ProjectModel) indexOf(node *filetree.Node) int {
	for i, row := range m.tree.Visible() {
		if row == node {
			return i
		}
	}
	return m.sel
}

// dirOf returns the directory a tree row stands for: itself, or the directory of a file.
func (m ProjectModel) dirOf(node *filetree.Node) string {
	if node.Dir {
		return node.Path
	}
	return filepath.Dir(node.Path)
}

// activate opens a file in the editor, or toggles a directory; focus moves to the editor for files when focusEditor is set.
func (m ProjectModel) activate(node *filetree.Node, toggle bool) (tea.Model, tea.Cmd) {
	if node == nil {
		return m, nil
	}
	if node.Dir {
		if toggle {
			m.tree.Toggle(node)
		} else {
			m.tree.Expand(node)
		}
		return m, nil
	}
	if err := m.openFile(node.Path, false); err != nil {
		m.notice(err.Error(), true)
		return m, nil
	}
	m.focus = focusProjectEdit
	return m.resizeEditor(), nil
}

func (m ProjectModel) toggleFocus() ProjectModel {
	if m.focus == focusTree {
		m.focus = focusProjectEdit
	} else {
		m.focus = focusTree
	}
	return m.resizeEditor()
}

func (m ProjectModel) forwardToEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.editor.Update(msg)
	m.editor = updated.(EditorModel)
	if m.editor.Quitting() {
		if len(m.files) > 1 { // :q, :wq and ctrl+d close the tab; the last one leaves
			m.closeTab(m.tab)
			return m, nil
		}
		return m.quit()
	}
	return m, cmd
}

// storeSession stores what is open in this project.
func (m ProjectModel) storeSession() {
	if m.hooks.SaveSession == nil {
		return
	}
	m.hooks.SaveSession(m.root, m.session())
}

// quit stores the session and leaves.
func (m ProjectModel) quit() (tea.Model, tea.Cmd) {
	m.storeSession()
	m.closeTerminal()
	return m, tea.Quit
}

// runProjectCommand handles the commands the editor does not know: project and window commands.
func (m ProjectModel) runProjectCommand(command string) (tea.Model, tea.Cmd) {
	name, argument, _ := strings.Cut(command, " ")
	argument = strings.TrimSpace(argument)
	switch name {
	case "project", "project!":
		if argument == "" {
			return m.openPicker()
		}
		return m.switchProject(m.resolve(argument), strings.HasSuffix(name, "!"))
	case "terminal", "term", "shell":
		if argument == "new" { // another tab
			return m.newTerminal()
		}
		return m.openTerminal()
	case "diff", "changes":
		return m.showDiff()
	case "tabnext", "tabn", "bnext", "bn":
		return m.stepTab(1)
	case "tabprevious", "tabprev", "tabp", "bprevious", "bprev", "bp":
		return m.stepTab(-1)
	case "bdelete", "bd", "tabclose", "tabc":
		return m.closeTabAsking(m.tab)
	case "wa", "wall":
		return m.saveAll(), nil
	case "qa", "qall":
		if i := m.firstModified(); i >= 0 {
			m.say("E37: unsaved changes in "+m.tabNames()[i]+" (:wa to save all, :qa! to quit without saving)", true)
			return m, nil
		}
		return m.quit()
	case "qa!", "qall!":
		return m.quit()
	case "theme", "colors", "colorscheme":
		return m.themeCommand(argument)
	case "addlang", "addlanguage", "language", "lang", "languages":
		return m.openLanguagePicker()
	case "tools":
		return m.toolsCommand(argument)
	case "projects", "recent":
		return m.openPicker()
	case "find", "files", "ff":
		return m.openSearch(searchFiles, argument)
	case "grep", "search", "fg":
		return m.openSearch(searchText, argument)
	case "window", "win":
		dir := m.root
		if argument != "" {
			dir = m.resolve(argument)
		}
		return m.newWindow(dir)
	default:
		m.editor.notice("E492: Not an editor command: "+command, true)
	}
	return m, nil
}

// resolve makes a path from a command absolute (relative paths are relative to the project).
func (m ProjectModel) resolve(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.root, path)
	}
	return filepath.Clean(path)
}

// newWindow opens a directory in a new terminal window.
func (m ProjectModel) newWindow(dir string) (tea.Model, tea.Cmd) {
	switch {
	case m.hooks.NewWindow == nil:
		m.say("E1: Opening windows is not available here", true)
	case !isDir(dir):
		m.say(fmt.Sprintf("E344: Can't find directory \"%s\"", dir), true)
	default:
		err := m.hooks.NewWindow(dir)
		if err == nil {
			m.say(fmt.Sprintf("Opened %s in a new window", dir), false)
			return m, nil
		}
		if dir == m.root {
			m.say(err.Error(), true)
			return m, nil
		}
		// no terminal to open a window in (a container, an ssh session): this window shows the project instead
		next, cmd := m.switchProject(dir, false)
		if got, ok := next.(ProjectModel); ok && got.root != m.root {
			got.status, got.warn = "No new window possible ("+shorten(err.Error(), 60)+"): opened here", false
			return got, cmd
		}
		return next, cmd
	}
	return m, nil
}

// say shows a message in the pane that has the focus.
func (m *ProjectModel) say(text string, warn bool) {
	if m.focus != focusTree {
		m.editor.notice(text, warn)
		return
	}
	m.notice(text, warn)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m ProjectModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.dlg != nil {
		return m.clickDialog(msg)
	}
	if m.pick != nil {
		return m.updateFormatPicker(msg)
	}
	if m.langs != nil {
		return m.updateLanguagePicker(msg)
	}
	if m.picker != nil {
		return m.updatePicker(msg)
	}
	if m.search != nil {
		return m.updateSearch(msg)
	}
	if msg.Y < topBarHeight {
		if leftPress(msg) {
			return m.clickTopBar(msg.X)
		}
		return m, nil
	}
	msg.Y -= topBarHeight
	tree := m.treeWidth()
	inTree := tree > 0 && msg.X < tree
	if !inTree && m.termPanelHeight() > 0 && msg.Y >= m.editorHeight() {
		return m.clickTerminal(msg, msg.X-m.editorOffset(), msg.Y-m.editorHeight())
	}
	if !inTree && m.focus == focusTerminal && leftPress(msg) {
		m.focus = focusProjectEdit
	}
	if !inTree && m.editorWidth() > 0 {
		msg.X -= m.editorOffset()
		if msg.Y == 0 && leftPress(msg) { // the tab bar
			return m.clickTabBar(msg.X)
		}
		return m.forwardToEditor(msg)
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.top = max(m.top-wheelLines, 0)
	case msg.Button == tea.MouseButtonWheelDown:
		m.top = min(m.top+wheelLines, max(len(m.tree.Visible())-m.treeBody(), 0))
	case leftPress(msg):
		if m.status == "" && m.toolStatus == "" { // the hints are clickable, a message is not
			if keys := m.treeFooterBar().keysAtLine(msg.X, msg.Y-treeHeaderLines-m.treeBody(), tree, treeFooterBars); keys != nil {
				return replay(m, keys)
			}
		}
		row := m.top + msg.Y - 1
		rows := m.tree.Visible()
		if msg.Y >= 1 && msg.Y <= m.treeBody() && row < len(rows) {
			m.sel = row
			m.focus = focusTree
			return m.activate(rows[row], true)
		}
	}
	return m, nil
}

// editorOffset is the screen column where the editor pane starts.
func (m ProjectModel) editorOffset() int {
	if tree := m.treeWidth(); tree > 0 && m.width >= minProjectWidth {
		return tree + 1
	}
	return 0
}

func (m ProjectModel) treeFooterBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range [][2]string{{"ctrl+p", "projects"}, {"ctrl+f", "files"}, {"ctrl+g", "text"}, {"ctrl+t", "terminal"}, {"enter", "open"}, {"w", "window"}} {
		result.command(h[0], h[1], true, m.theme)
	}
	return result
}

// View implements tea.Model.
func (m ProjectModel) View() string {
	if m.width == 0 {
		return ""
	}
	if m.picker != nil {
		return m.picker.View()
	}
	if m.search != nil {
		return m.search.View()
	}
	screen := m.topBar().render(m.width, m.theme.Footer) + "\n" + m.body()
	if m.dlg != nil {
		return m.overlayDialog(screen)
	}
	if m.pick != nil {
		return m.pick.picker.overlay(screen, m.width, m.height)
	}
	if m.langs != nil {
		return m.langs.picker.overlay(screen, m.width, m.height)
	}
	return screen
}

// body is the tree and the editor under the top bar.
func (m ProjectModel) body() string {
	m.editor.ambient = m.toolStatus
	tree := m.treeWidth()
	switch {
	case tree == 0:
		return m.editorColumn()
	case m.width < minProjectWidth:
		return m.treeView(tree)
	}
	divider := strings.TrimSuffix(strings.Repeat(m.theme.Gutter.Render(separator)+"\n", m.bodyHeight()), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, m.treeView(tree), divider, m.editorColumn())
}

func (m ProjectModel) treeView(width int) string {
	lines := make([]string, 0, m.height)
	header := m.theme.Header
	if m.focus == focusTree {
		header = m.theme.Selected.Bold(true)
	}
	lines = append(lines, header.Render(padRight(" "+sanitize(filepath.Base(m.root)), width)))
	rows := m.tree.Visible()
	for y := 0; y < m.treeBody(); y++ {
		i := m.top + y
		if i >= len(rows) {
			lines = append(lines, strings.Repeat(" ", width))
			continue
		}
		lines = append(lines, m.treeRow(rows[i], i == m.sel, width))
	}
	return strings.Join(append(lines, m.treeFooterRows(width)...), "\n")
}

func (m ProjectModel) treeRow(node *filetree.Node, selected bool, width int) string {
	style := m.theme.Equal
	if selected {
		style = m.theme.SelectedDim
		if m.focus == focusTree {
			style = m.theme.Selected
		}
	}
	mark := "  "
	if node.Dir {
		mark = "▸ "
		if node.Expanded {
			mark = "▾ "
		}
	}
	label := strings.Repeat("  ", node.Depth-1) + mark + sanitize(node.Name)
	if node.Dir {
		label += "/"
	}
	if i := m.tabIndex(node.Path); i >= 0 {
		label += "  ●"
		if m.openEditors()[i].Modified() {
			label += "+"
		}
	}
	return style.Render(padRight(" "+label, width))
}

// themeCommand is :theme [name]: it switches the colours of the window at once. Without a name it lists the themes. A terminal with
// few colours gets the "ansi" theme by itself (auto); this is the way to choose another one when that is not right.
func (m ProjectModel) themeCommand(argument string) (tea.Model, tea.Cmd) {
	if m.hooks.Theme == nil {
		m.say("E1: Switching the theme is not available here", true)
		return m, nil
	}
	name := strings.ToLower(strings.TrimSpace(argument))
	if name == "" {
		m.say("Themes: "+strings.Join(config.ThemeNames(), ", ")+" (:theme <name>; JELLY_DEV_THEME=<name> keeps it)", false)
		return m, nil
	}
	theme, ok := m.hooks.Theme(name)
	if !ok {
		m.say(fmt.Sprintf("E474: Unknown theme %q: %s", name, strings.Join(config.ThemeNames(), ", ")), true)
		return m, nil
	}
	m = m.WithTheme(theme)
	m.say(fmt.Sprintf("Theme %s (set JELLY_DEV_THEME=%s to keep it)", name, name), false)
	return m, nil
}
