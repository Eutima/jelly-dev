package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// navTimeout is how long a request to a language server may take (the first one starts the server).
const navTimeout = 60 * time.Second

// CodeNavigator answers code navigation questions about a file: where is the symbol at a position defined, where is it
// used. text is the text in the editor, which may differ from the file on disk. Rows are zero-based, columns count runes.
type CodeNavigator interface {
	Definition(ctx context.Context, path, text string, row, col int) ([]lsp.Place, error)
	References(ctx context.Context, path, text string, row, col int) ([]lsp.Place, error)
	// Format returns the text of a file formatted.
	Format(ctx context.Context, path, text string) (string, error)
	// FormatterChoices lists the formatters to pick from when Format finds none for a file, FormatWith formats with the
	// one picked and RememberFormatter makes Format use it for this file type from then on.
	FormatterChoices(path string) []lsp.FormatterChoice
	FormatWith(ctx context.Context, path, text string, choice lsp.FormatterChoice) (string, error)
	RememberFormatter(path string, choice lsp.FormatterChoice) error
	// Complete returns the suggestions at a position.
	Complete(ctx context.Context, path, text string, row, col int) ([]lsp.CompletionItem, error)
	// Hover returns the documentation of the symbol at a position ("" when there is none).
	Hover(ctx context.Context, path, text string, row, col int) (string, error)
}

// navKind tells which question was asked.
type navKind int

const (
	navDefinition navKind = iota
	navReferences
)

func (k navKind) noun() string {
	if k == navReferences {
		return "references"
	}
	return "definition"
}

// navResultMsg is the answer of a navigation request.
type navResultMsg struct {
	kind   navKind
	origin lsp.Place // where the question was asked: the place that ctrl+o returns to
	places []lsp.Place
	err    error
}

// JumpMsg asks the model around the editor (the project window) to open a file at a place; the editor sends it for
// places in other files.
type JumpMsg struct {
	Place lsp.Place
	Back  bool // a jump back (ctrl+o, ctrl+c, ctrl+d): the place came off the jump list
}

// placeList is the list of places under the text: the references of a symbol, or several definitions.
type placeList struct {
	title  string
	places []lsp.Place
	origin lsp.Place
	sel    int
	top    int

	searching bool   // the search prompt is open: keys go to the pattern
	query     string // the pattern being typed
	last      string // the pattern of the last confirmed search (n and N repeat it)
	backward  bool   // the last search went upwards (?)
	from      int    // the selected place before the search started (esc goes back to it)
}

const maxListHeight = 10

// WithNavigator returns the editor with a code navigator (gd and gr); root is the project folder file names are shown
// relative to.
func (m EditorModel) WithNavigator(nav CodeNavigator, root string) EditorModel {
	m.nav, m.root = nav, root
	return m
}

// listHeight is the number of lines the place list takes (0 when it is closed).
func (m EditorModel) listHeight() int {
	if m.list == nil {
		return 0
	}
	return min(len(m.list.places)+1, maxListHeight, max(m.bodyHeight()/2, 2))
}

// textHeight is the number of lines left for the text.
func (m EditorModel) textHeight() int { return max(m.bodyHeight()-m.listHeight(), 1) }

// navigationKey handles gd, gr and ctrl+o in normal mode; it reports whether the key was used.
func (m EditorModel) navigationKey(key string) (EditorModel, tea.Cmd, bool) {
	if m.nano != nil {
		return m.nanoNavigationKey(key)
	}
	if m.vim.Mode() != vimbuf.Normal {
		return m, nil, false
	}
	switch pending := m.vim.Pending(); {
	case pending == "g" && (key == "d" || key == "r"):
		m.vim.Handle("esc") // the g is used up
		kind := navDefinition
		if key == "r" {
			kind = navReferences
		}
		model, cmd := m.askNavigator(kind)
		return model, cmd, true
	case pending == "g" && m.forwardEx && (key == "t" || key == "T"): // gt, gT: the next and previous tab
		m.vim.Handle("esc") // the g is used up
		command := map[string]string{"t": "tabnext", "T": "tabprev"}[key]
		return m, func() tea.Msg { return ExCommandMsg{Command: command} }, true
	case pending == "" && m.forwardEx && forwardedCommand(key) != "": // the project window opens its search, project list or tab list
		command := forwardedCommand(key)
		return m, func() tea.Msg { return ExCommandMsg{Command: command} }, true
	case pending == "" && keys.Is(keymap.Back, key):
		model, cmd := m.jumpBack()
		return model, cmd, true
	}
	return m, nil, false
}

func (m EditorModel) askNavigator(kind navKind) (EditorModel, tea.Cmd) {
	switch {
	case m.nav == nil:
		m.notice("E1: No language server available in this editor", true)
		return m, nil
	case m.doc.Path == "":
		m.notice("E32: No file name: save the file first", true)
		return m, nil
	}
	row, col := m.vim.Cursor()
	origin := lsp.Place{Path: m.doc.Path, Row: row, Col: col}
	text := strings.Join(m.vim.Lines(), "\n")
	nav := m.nav
	m.notice("Asking the language server for the "+kind.noun()+"...", false)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		var places []lsp.Place
		var err error
		if kind == navReferences {
			places, err = nav.References(ctx, origin.Path, text, row, col)
		} else {
			places, err = nav.Definition(ctx, origin.Path, text, row, col)
		}
		return navResultMsg{kind: kind, origin: origin, places: places, err: err}
	}
}

// handleNavResult shows the answer: a message, a jump, or the list of places.
func (m EditorModel) handleNavResult(msg navResultMsg) (EditorModel, tea.Cmd) {
	var missing *lsp.NotInstalledError
	switch {
	case errors.As(msg.err, &missing):
		m.notice(msg.err.Error(), true)
	case errors.Is(msg.err, lsp.ErrNoServer):
		m.notice("No language server for this file type (add one in lsp.json)", true)
	case msg.err != nil:
		m.notice("Language server: "+msg.err.Error(), true)
	case len(msg.places) == 0:
		m.notice("No "+msg.kind.noun()+" found", true)
	case len(msg.places) == 1:
		return m.jump(msg.origin, msg.places[0])
	default:
		m.list = &placeList{title: fmt.Sprintf("%d %s", len(msg.places), msg.kind.noun()), places: msg.places, origin: msg.origin}
		if msg.kind == navDefinition {
			m.list.title = fmt.Sprintf("%d definitions", len(msg.places))
		}
		m.scrollToCursor()
	}
	return m, nil
}

// jump goes to a place: in this file by moving the cursor, in another file by asking the window to open it. The
// place the jump started from is remembered for ctrl+o.
func (m EditorModel) jump(origin, place lsp.Place) (EditorModel, tea.Cmd) {
	m.jumps = append(m.jumps[:len(m.jumps):len(m.jumps)], origin)
	m.list = nil
	if place.Path == m.doc.Path {
		m = m.At(place.Row, place.Col)
		m.notice(fmt.Sprintf("%s:%d", filepath.Base(place.Path), place.Row+1), false)
		return m, nil
	}
	return m, func() tea.Msg { return JumpMsg{Place: place} }
}

// jumpBack returns to where the last jump started.
func (m EditorModel) jumpBack() (EditorModel, tea.Cmd) {
	if len(m.jumps) == 0 {
		m.notice("E959: Jump list is empty", true)
		return m, nil
	}
	place := m.jumps[len(m.jumps)-1]
	m.jumps = m.jumps[:len(m.jumps)-1]
	if place.Path == m.doc.Path {
		m = m.At(place.Row, place.Col)
		m.notice(fmt.Sprintf("Back at %s:%d", filepath.Base(place.Path), place.Row+1), false)
		return m, nil
	}
	return m, func() tea.Msg { return JumpMsg{Place: place, Back: true} }
}

// pushJump remembers the cursor position as the place ctrl+o returns to.
func (m EditorModel) pushJump() EditorModel {
	if m.doc.Path != "" {
		row, col := m.vim.Cursor()
		m.jumps = append(m.jumps[:len(m.jumps):len(m.jumps)], lsp.Place{Path: m.doc.Path, Row: row, Col: col})
	}
	return m
}

// restoreJump puts a place back on the jump list (the window could not go back to it).
func (m EditorModel) restoreJump(place lsp.Place) EditorModel {
	m.jumps = append(m.jumps[:len(m.jumps):len(m.jumps)], place)
	return m
}

// dropJump forgets the last jump (the window could not open the file it led to).
func (m EditorModel) dropJump() EditorModel {
	if len(m.jumps) > 0 {
		m.jumps = m.jumps[:len(m.jumps)-1]
	}
	return m
}

// handleJump opens the file of a place when the editor is used without a project window around it.
func (m EditorModel) handleJump(msg JumpMsg) EditorModel {
	if msg.Place.Path != m.doc.Path {
		m = m.OpenFile(msg.Place.Path, false)
		if m.doc.Path != msg.Place.Path {
			if msg.Back {
				return m.restoreJump(msg.Place)
			}
			return m.dropJump()
		}
	}
	return m.At(msg.Place.Row, msg.Place.Col)
}

// handleListKey runs the keys of the open place list.
func (m EditorModel) handleListKey(msg tea.KeyMsg) (EditorModel, tea.Cmd) {
	list := m.list
	if list.searching {
		m.searchKey(msg)
		m.keepListVisible()
		return m, nil
	}
	switch msg.String() {
	case "/", "?":
		list.searching, list.query, list.from, list.backward = true, "", list.sel, msg.String() == "?"
	case "n", "N":
		if list.last == "" {
			m.notice("E35: No previous search pattern", true)
			break
		}
		backward := list.backward != (msg.String() == "N")
		if !m.selectMatch(list.last, list.sel, backward, false) {
			m.notice("E486: Pattern not found: "+list.last, true)
		}
	case "esc", "q", "ctrl+c", "ctrl+d":
		m.list = nil
		m.scrollToCursor()
	case "j", "down", "ctrl+n":
		list.sel = min(list.sel+1, len(list.places)-1)
	case "k", "up", "ctrl+p":
		list.sel = max(list.sel-1, 0)
	case "g", "home":
		list.sel = 0
	case "G", "end":
		list.sel = len(list.places) - 1
	case "enter", "o":
		return m.openListed()
	}
	m.keepListVisible()
	return m, nil
}

// searchKey edits the pattern of the search prompt of the place list. The selection follows the pattern while it is typed;
// enter keeps it, esc goes back to where the search started.
func (m *EditorModel) searchKey(msg tea.KeyMsg) {
	list := m.list
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlD:
		list.searching, list.sel = false, list.from
		return
	case tea.KeyEnter:
		list.searching = false
		if list.query == "" {
			return
		}
		list.last = list.query
		if !m.selectMatch(list.last, list.from, list.backward, true) {
			list.sel = list.from
			m.notice("E486: Pattern not found: "+list.last, true)
		}
		return
	case tea.KeyBackspace, tea.KeyCtrlH:
		if list.query == "" { // like vim: backspace on an empty prompt leaves it
			list.searching, list.sel = false, list.from
			return
		}
		runes := []rune(list.query)
		list.query = string(runes[:len(runes)-1])
	case tea.KeyRunes:
		list.query += string(msg.Runes)
	case tea.KeySpace:
		list.query += " "
	default:
		return
	}
	if list.query != "" {
		m.selectMatch(list.query, list.from, list.backward, true)
	} else {
		list.sel = list.from
	}
}

// selectMatch selects the next place (from the selected one, wrapping around) whose text matches the pattern; with inclusive
// the start place itself counts. It reports whether there was a match.
func (m *EditorModel) selectMatch(pattern string, start int, backward, inclusive bool) bool {
	list := m.list
	matches := patternMatcher(pattern)
	step := 1
	if backward {
		step = -1
	}
	count := len(list.places)
	first := 1 // the start place is the last candidate unless it counts at once
	if inclusive {
		first = 0
	}
	for offset := first; offset < first+count; offset++ {
		index := ((start+step*offset)%count + count) % count
		if matches(m.listEntry(list.places[index])) {
			list.sel = index
			return true
		}
	}
	return false
}

// patternMatcher returns the test of a search pattern like vim's default: a regular expression (the text itself when the
// pattern is not one), ignoring case unless the pattern has an upper case letter (smartcase).
func patternMatcher(pattern string) func(string) bool {
	fold := pattern == strings.ToLower(pattern)
	if re, err := regexp.Compile(pattern); err == nil {
		if fold {
			re = regexp.MustCompile("(?i)" + pattern)
		}
		return re.MatchString
	}
	if fold {
		return func(text string) bool { return strings.Contains(strings.ToLower(text), pattern) }
	}
	return func(text string) bool { return strings.Contains(text, pattern) }
}

// listEntry is the text of a place as the list shows it, and as the search sees it.
func (m EditorModel) listEntry(place lsp.Place) string {
	return fmt.Sprintf("%s:%d  %s", m.shortPath(place.Path), place.Row+1, strings.TrimSpace(sanitize(place.Text)))
}

// openListed jumps to the selected place of the list.
func (m EditorModel) openListed() (EditorModel, tea.Cmd) {
	list := m.list
	return m.jump(list.origin, list.places[list.sel])
}

func (m *EditorModel) keepListVisible() {
	list, rows := m.list, m.listHeight()-1
	if rows < 1 {
		return
	}
	if list.sel < list.top {
		list.top = list.sel
	}
	if list.sel >= list.top+rows {
		list.top = list.sel - rows + 1
	}
}

// listLines draws the place list: a title line and the places, in exactly width columns each.
func (m EditorModel) listLines() []string {
	list := m.list
	height := m.listHeight()
	title := " " + list.title + "  [enter] open  [j/k] choose  [/] search  [esc] close"
	if list.searching {
		prefix := "/"
		if list.backward {
			prefix = "?"
		}
		title = " " + prefix + list.query + "▏  (enter keeps the match, esc cancels)"
	} else if list.last != "" {
		title = " " + list.title + "  /" + list.last + "  [n/N] next/previous  [enter] open  [/] search  [esc] close"
	}
	lines := []string{m.theme.Header.Render(padRight(title, m.width))}
	for i := list.top; i < len(list.places) && len(lines) < height; i++ {
		place := list.places[i]
		style := m.theme.Equal
		if i == list.sel {
			style = m.theme.Selected
		}
		lines = append(lines, style.Render(padRight(" "+m.listEntry(place), m.width)))
	}
	return lines
}

// shortPath is a file name relative to the project when it is inside it.
func (m EditorModel) shortPath(path string) string {
	if m.root != "" {
		if rel, err := filepath.Rel(m.root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return path
}

// clickList selects or opens the place that was clicked.
func (m EditorModel) clickList(y int) (EditorModel, tea.Cmd, bool) {
	if m.list == nil {
		return m, nil, false
	}
	first := 1 + m.textHeight() // the title line of the list
	if y < first || y >= first+m.listHeight() {
		return m, nil, false
	}
	if index := m.list.top + y - first - 1; index >= 0 && index < len(m.list.places) {
		m.list.sel = index
		model, cmd := m.openListed()
		return model, cmd, true
	}
	return m, nil, true
}

// formatResultMsg is the answer of a format request.
type formatResultMsg struct {
	original  string // the text that was sent
	formatted string
	err       error
	remember  *lsp.FormatterChoice // the formatter to remember for the file type once its result was applied
}

// FormatPickMsg asks the project window to let the user choose a formatter for the file: :format found none.
type FormatPickMsg struct{ Path, Text string }

// formatCommand starts formatting the file in the editor (:format).
func (m *EditorModel) formatCommand() tea.Cmd {
	switch {
	case m.nav == nil:
		m.notice("E1: No formatter available in this editor", true)
		return nil
	case m.doc.Path == "":
		m.notice("E32: No file name: save the file first", true)
		return nil
	}
	path, nav := m.doc.Path, m.nav
	original := strings.Join(m.vim.Lines(), "\n")
	m.notice("Formatting...", false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		formatted, err := nav.Format(ctx, path, original)
		return formatResultMsg{original: original, formatted: formatted, err: err}
	}
}

// rememberFormatter stores the formatter the user picked for this file type, now that it has worked on this file; the note
// tells the user. Nothing to remember (a normal :format) gives no note.
func (m EditorModel) rememberFormatter(choice *lsp.FormatterChoice) string {
	if choice == nil || m.nav == nil {
		return ""
	}
	if err := m.nav.RememberFormatter(m.doc.Path, *choice); err != nil {
		return " · could not remember " + choice.Name + ": " + err.Error()
	}
	return " · " + choice.Name + " is used for this file type from now on"
}

// handleFormatResult replaces the text by the formatted one, as one undoable change.
func (m EditorModel) handleFormatResult(msg formatResultMsg) (EditorModel, tea.Cmd) {
	var missing *lsp.NotInstalledError
	noFormatter := errors.As(msg.err, &missing) || errors.Is(msg.err, lsp.ErrNoServer) || errors.Is(msg.err, lsp.ErrCannotFormat)
	switch {
	case noFormatter && m.forwardEx && m.doc.Path != "":
		// the project window lets the user choose a formatter from a searchable list
		m.notice("No formatter found for this file: choose one", false)
		path := m.doc.Path
		return m, func() tea.Msg { return FormatPickMsg{Path: path, Text: msg.original} }
	case errors.As(msg.err, &missing):
		m.notice(msg.err.Error(), true)
		return m, nil
	case errors.Is(msg.err, lsp.ErrNoServer):
		m.notice("No formatter for this file type (add a server or formatter in lsp.json)", true)
		return m, nil
	case errors.Is(msg.err, lsp.ErrCannotFormat):
		m.notice("The language server cannot format this file (set a formatter in lsp.json)", true)
		return m, nil
	case msg.err != nil:
		m.notice("Format failed: "+msg.err.Error(), true)
		return m, nil
	case strings.Join(m.vim.Lines(), "\n") != msg.original:
		m.notice("The text changed while it was formatted: not applied, run :format again", true)
		return m, nil
	}
	lines := diff.SplitLines(msg.formatted)
	note := m.rememberFormatter(msg.remember)
	if strings.Join(lines, "\n") == msg.original {
		m.notice("Already formatted"+note, false)
		return m, nil
	}
	m.vim.ReplaceLines(lines)
	m.scrollToCursor()
	undo := "u undoes"
	if m.nano != nil {
		undo = keymap.Pretty(keys.Key(keymap.NanoUndo)) + " undoes"
	}
	m.notice(fmt.Sprintf("Formatted (%d lines) - %s", len(lines), undo)+note, false)
	return m, nil
}

// forwardedCommand is the Ex command of the project window that a key of the editor's normal mode stands for ("" for any other key): the
// search, the project list and the tab list.
func forwardedCommand(key string) string {
	switch command, _ := keys.Lookup(keymap.ScopeIDE, key); command {
	case keymap.Find:
		return "find"
	case keymap.Grep:
		return "grep"
	case keymap.Projects:
		return "project"
	case keymap.Tabs:
		return "tabs"
	}
	return ""
}
