package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/search"
)

type searchMode int

const (
	searchFiles searchMode = iota // by file name
	searchText                    // by file content
)

// searchDebounce is the pause after the last key press before a content search starts (a variable so tests can shorten it).
var searchDebounce = 200 * time.Millisecond

const (
	maxFileResults = 200
	searchTop      = 2 // title, input
	searchAir      = 1 // one line of air above the hints
)

// filesLoadedMsg carries the list of the project's files.
type filesLoadedMsg struct {
	files     []string
	truncated bool
	err       error
}

// searchTickMsg ends the pause after the last key press: the content search starts.
type searchTickMsg struct{ seq int }

// textResultMsg carries the result of a content search.
type textResultMsg struct {
	seq       int
	matches   []search.TextMatch
	truncated bool
	err       error
}

// SearchModel is the search over the whole project: file names (fuzzy) and file content. It is part of the project
// window and takes over the screen while it is open.
type SearchModel struct {
	theme         Theme
	width, height int
	root          string
	mode          searchMode
	input         []rune
	regex         bool
	caseSensitive bool // off: smart case (a capital letter makes the query case sensitive)
	containsOnly  bool // file names: a word must appear in one piece (alt+f); off: fuzzy, the letters may be spread over the path

	files       []string
	filesLoaded bool
	filesCut    bool
	fileResults []search.FileMatch
	textResults []search.TextMatch
	textCut     bool
	searching   bool
	err         error
	seq         int
	cancel      context.CancelFunc
	sel, top    int
	preview     *previewCache // the file the code preview shows

	done   bool       // the search was left
	choice *lsp.Place // the place the user chose, when there is one
}

// newSearch opens the search in a mode with a start query.
func newSearch(theme Theme, root string, mode searchMode, query string, width, height int, known []string) (SearchModel, tea.Cmd) {
	m := SearchModel{theme: theme, root: root, mode: mode, input: []rune(query), width: width, height: height, preview: &previewCache{}}
	if known != nil { // the last list is shown at once while a fresh one is read
		m.files, m.filesLoaded = known, true
	}
	m.refilter()
	cmds := []tea.Cmd{m.scanFiles()}
	if mode == searchText && query != "" {
		cmds = append(cmds, m.scheduleText())
	}
	return m, tea.Batch(cmds...)
}

func (m SearchModel) scanFiles() tea.Cmd {
	root := m.root
	return func() tea.Msg {
		files, truncated, err := search.Files(context.Background(), root)
		return filesLoadedMsg{files: files, truncated: truncated, err: err}
	}
}

// Files returns the files that were listed last (nil before the first list arrived).
func (m SearchModel) Files() []string {
	if !m.filesLoaded {
		return nil
	}
	return m.files
}

func (m SearchModel) bodyHeight() int {
	return max(m.height-searchTop-(len(m.inputRows())-1)-m.previewHeight()-searchAir-len(m.footerRows()), 1)
}

// footerRows are the hints as wrapped bars.
func (m SearchModel) footerRows() []*bar { return m.hintBar().wrap(m.width, footerMax) }

func (m SearchModel) resultCount() int {
	if m.mode == searchFiles {
		return len(m.fileResults)
	}
	return len(m.textResults)
}

// matchFiles matches the typed query against the file names the way the search is set (fuzzy or contains).
func (m SearchModel) matchFiles() []search.FileMatch {
	return search.MatchFiles(m.files, string(m.input), maxFileResults, !m.containsOnly)
}

// refilter recomputes the file name matches for the typed query.
func (m *SearchModel) refilter() {
	if m.mode == searchFiles {
		m.fileResults = m.matchFiles()
	}
	m.sel, m.top = 0, 0
}

// scheduleText starts the pause before a content search; typing again restarts it.
func (m *SearchModel) scheduleText() tea.Cmd {
	m.seq++
	seq := m.seq
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.searching = true
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTickMsg{seq: seq} })
}

// startText runs the content search for the current query.
func (m *SearchModel) startText() tea.Cmd {
	query := string(m.input)
	if query == "" || !m.filesLoaded {
		m.searching = query != "" // waits for the file list
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	seq, root, files := m.seq, m.root, m.files
	options := search.TextOptions{Regex: m.regex, CaseSensitive: m.caseSensitive}
	return func() tea.Msg {
		matches, truncated, err := search.Text(ctx, root, files, query, options)
		return textResultMsg{seq: seq, matches: matches, truncated: truncated, err: err}
	}
}

// Update handles the messages of the search: keys, the mouse and the answers of the background work.
func (m SearchModel) Update(msg tea.Msg) (SearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepVisible()
	case filesLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.files, m.filesLoaded, m.filesCut = msg.files, true, msg.truncated
		if m.mode == searchFiles {
			selected := m.selectedPath()
			m.fileResults = m.matchFiles()
			m.restoreSelection(selected)
			return m, nil
		}
		if len(m.input) > 0 && m.searching {
			return m, m.startText()
		}
	case searchTickMsg:
		if msg.seq == m.seq {
			return m, m.startText()
		}
	case textResultMsg:
		if msg.seq != m.seq {
			return m, nil // the answer to an older query
		}
		m.searching, m.err = false, msg.err
		m.textResults, m.textCut = msg.matches, msg.truncated
		m.sel, m.top = 0, 0
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m SearchModel) selectedPath() string {
	if m.mode == searchFiles && m.sel < len(m.fileResults) {
		return m.fileResults[m.sel].Path
	}
	return ""
}

func (m *SearchModel) restoreSelection(path string) {
	m.sel, m.top = 0, 0
	for i, match := range m.fileResults {
		if match.Path == path && path != "" {
			m.sel = i
		}
	}
	m.keepVisible()
}

func (m SearchModel) handleKey(msg tea.KeyMsg) (SearchModel, tea.Cmd) {
	step := 0
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+d":
		return m.close(), nil
	case "enter":
		return m.choose()
	case "tab":
		return m.switchMode()
	case "ctrl+r":
		m.regex = !m.regex
		if m.mode == searchText && len(m.input) > 0 {
			return m, m.scheduleText()
		}
		return m, nil
	case "alt+f":
		if m.mode != searchFiles {
			return m, nil
		}
		m.containsOnly = !m.containsOnly
		m.refilter()
		return m, nil
	case "alt+c":
		m.caseSensitive = !m.caseSensitive
		if m.mode == searchText && len(m.input) > 0 {
			return m, m.scheduleText()
		}
		return m, nil
	case "down", "ctrl+n":
		step = 1
	case "up", "ctrl+p":
		step = -1
	case "pgdown":
		step = m.bodyHeight()
	case "pgup":
		step = -m.bodyHeight()
	case "backspace", "ctrl+h":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			return m.queryChanged()
		}
	case "ctrl+u":
		m.input = nil
		return m.queryChanged()
	case " ":
		m.input = append(m.input, ' ')
		return m.queryChanged()
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			m.input = append(m.input, []rune(sanitizeEditable(string(msg.Runes)))...)
			return m.queryChanged()
		}
	}
	if step != 0 {
		m.sel = min(max(m.sel+step, 0), max(m.resultCount()-1, 0))
		m.keepVisible()
	}
	return m, nil
}

func (m SearchModel) queryChanged() (SearchModel, tea.Cmd) {
	m.err = nil
	if m.mode == searchFiles {
		m.refilter()
		return m, nil
	}
	if len(m.input) == 0 {
		if m.cancel != nil {
			m.cancel()
		}
		m.seq++ // answers of the old query are dropped
		m.textResults, m.searching, m.textCut = nil, false, false
		return m, nil
	}
	return m, m.scheduleText()
}

func (m SearchModel) switchMode() (SearchModel, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.seq++
	m.searching, m.err = false, nil
	if m.mode == searchFiles {
		m.mode = searchText
		m.sel, m.top = 0, 0
		if len(m.input) > 0 {
			return m, m.scheduleText()
		}
		return m, nil
	}
	m.mode = searchFiles
	m.refilter()
	return m, nil
}

func (m SearchModel) close() SearchModel {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.done = true
	return m
}

// choose ends the search with the selected result.
func (m SearchModel) choose() (SearchModel, tea.Cmd) {
	if m.sel >= m.resultCount() {
		return m, nil
	}
	var place lsp.Place
	if m.mode == searchFiles {
		place = lsp.Place{Path: filepath.Join(m.root, filepath.FromSlash(m.fileResults[m.sel].Path))}
	} else {
		match := m.textResults[m.sel]
		place = lsp.Place{Path: filepath.Join(m.root, filepath.FromSlash(match.Path)), Row: match.Row, Col: match.Col, Text: match.Text}
	}
	m = m.close()
	m.choice = &place
	return m, nil
}

func (m *SearchModel) keepVisible() {
	m.top = scrollToShow(m.sel, m.top, m.bodyHeight(), m.resultCount(), func(row int) int { return len(m.resultRowLines(row)) })
}

func (m SearchModel) handleMouse(msg tea.MouseMsg) (SearchModel, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.top = max(m.top-wheelLines, 0)
	case msg.Button == tea.MouseButtonWheelDown:
		m.top = min(m.top+wheelLines, max(m.resultCount()-m.bodyHeight(), 0))
	case leftPress(msg):
		if msg.Y >= m.height-len(m.footerRows()) {
			if keys := rowKeysAt(m.footerRows(), msg.X, msg.Y, m.width, m.height); keys != nil {
				model, cmd := replay(searchKeyTarget{m}, keys)
				return model.(searchKeyTarget).SearchModel, cmd
			}
			return m, nil
		}
		if msg.Y == 0 { // the tabs of the title line
			if (msg.X < 14) != (m.mode == searchFiles) {
				return m.switchMode()
			}
			return m, nil
		}
		first := 1 + len(m.inputRows()) // the title and the input
		if _, rowOf := drawRows(m.top, m.resultCount(), m.bodyHeight(), m.resultRowLines); msg.Y >= first && msg.Y-first < len(rowOf) {
			m.sel = rowOf[msg.Y-first]
			return m.choose()
		}
	}
	return m, nil
}

// searchKeyTarget lets a click on a hint replay its keys through the search's own key handler.
type searchKeyTarget struct{ SearchModel }

func (t searchKeyTarget) Init() tea.Cmd { return nil }
func (t searchKeyTarget) View() string  { return "" }
func (t searchKeyTarget) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := t.SearchModel.Update(msg)
	return searchKeyTarget{model}, cmd
}

func (m SearchModel) hintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	regex := "regex"
	if m.regex {
		regex = "regex on"
	}
	caseHint := "case"
	if m.caseSensitive {
		caseHint = "case on"
	}
	result.command("enter", "open", m.resultCount() > 0, m.theme)
	result.command("tab", "files/text", true, m.theme)
	match := "fuzzy"
	if m.containsOnly {
		match = "contains"
	}
	result.command("alt+f", match, m.mode == searchFiles, m.theme)
	result.command("ctrl+r", regex, m.mode == searchText, m.theme)
	result.command("alt+c", caseHint, m.mode == searchText, m.theme)
	result.command("↑↓", "choose", m.resultCount() > 0, m.theme)
	result.command("esc", "close", true, m.theme)
	return result
}

// View draws the search over the whole window.
func (m SearchModel) View() string {
	if m.width == 0 {
		return ""
	}
	tab := func(label string, active bool) string {
		if active {
			return m.theme.Selected.Bold(true).Render(" " + label + " ")
		}
		return m.theme.Header.Render(" " + label + " ")
	}
	title := tab("Files", m.mode == searchFiles) + tab("Text", m.mode == searchText) + m.theme.Header.Render(padRight("  search the project", max(m.width-14, 0)))
	lines := append([]string{title}, m.inputRows()...)
	body := m.bodyHeight()
	shown, _ := drawRows(m.top, m.resultCount(), body, m.resultRowLines)
	if m.resultCount() == 0 {
		shown = []string{m.theme.Equal.Faint(true).Render(padRight(" "+m.emptyMessage(), m.width))}
	}
	for len(shown) < body {
		shown = append(shown, strings.Repeat(" ", m.width))
	}
	lines = append(lines, shown...)
	lines = append(lines, m.previewRows()...)
	lines = append(lines, strings.Repeat(" ", m.width))
	lines = append(lines, renderRowLines(m.footerRows(), m.width, m.theme.Footer)...)
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}

func (m SearchModel) emptyMessage() string {
	switch {
	case m.err != nil:
		return "! " + sanitize(m.err.Error())
	case !m.filesLoaded:
		return "Reading the files of the project..."
	case m.mode == searchText && len(m.input) == 0:
		return "Type to search the content of all files (node_modules, .venv and ignored files are skipped)"
	case m.searching:
		return "Searching..."
	}
	return "No match"
}

// inputRows draws the input line: the status stays at the end of the first row, a long query continues on further rows.
func (m SearchModel) inputRows() []string {
	status := ""
	switch {
	case m.mode == searchFiles && m.filesLoaded:
		status = fmt.Sprintf("%d of %d files", len(m.fileResults), len(m.files))
		if m.filesCut {
			status += " (list cut)"
		}
	case m.mode == searchText && m.searching:
		status = "searching..."
	case m.mode == searchText && m.err == nil && len(m.input) > 0:
		status = fmt.Sprintf("%d matches", len(m.textResults))
		if m.textCut {
			status = fmt.Sprintf("first %d matches", len(m.textResults))
		}
	}
	if m.mode == searchFiles && m.containsOnly {
		status = "contains · " + status
	}
	if m.mode == searchText && m.caseSensitive {
		status = "case · " + status
	}
	if m.mode == searchText && m.regex {
		status = "regex · " + status
	}
	left := " > " + string(m.input) + "█"
	room := max(m.width-runewidth.StringWidth(status)-2, 4)
	parts := splitRowLines(sanitize(left), room)
	rows := make([]string, len(parts))
	for i, part := range parts {
		text := part
		if i == 0 {
			gap := max(m.width-runewidth.StringWidth(part)-runewidth.StringWidth(status)-1, 1)
			text += strings.Repeat(" ", gap) + status + " "
		}
		rows[i] = m.theme.Equal.Render(padRight(text, m.width))
	}
	return rows
}

// resultRowLines draws a result: a long name or line continues on further rows, nothing is cut off.
func (m SearchModel) resultRowLines(i int) []string {
	style := m.theme.Equal
	if i == m.sel {
		style = m.theme.Selected
	}
	if m.mode == searchFiles {
		path := m.fileResults[i].Path
		dir, name := "", path
		if cut := strings.LastIndexByte(path, '/'); cut >= 0 {
			dir, name = path[:cut+1], path[cut+1:]
		}
		parts := splitRowLines(" "+sanitize(name)+"  "+sanitize(dir), m.width)
		lines := make([]string, len(parts))
		for n, part := range parts {
			lines[n] = style.Render(padRight(part, m.width))
		}
		return lines
	}
	match := m.textResults[i]
	prefix := fmt.Sprintf(" %s:%d  ", sanitize(match.Path), match.Row+1)
	line := []rune(strings.ReplaceAll(match.Text, "\t", "    "))
	return m.matchRowLines(prefix, line, match.Col-match.Offset, match.Length, style, i == m.sel)
}

// matchRowLines draws a matching line after its prefix, wrapped to the width of the window; the match keeps its own look, the rest of the
// row the row's. The continuation rows start under the text.
func (m SearchModel) matchRowLines(prefix string, line []rune, start, length int, style lipgloss.Style, selected bool) []string {
	hitStyle := m.theme.ModifiedWord.Bold(true)
	if selected {
		hitStyle = style.Bold(true).Underline(true)
	}
	prefixWidth := runewidth.StringWidth(prefix)
	room := max(m.width-prefixWidth, 1)
	hitFrom, hitTo := -1, -1
	if start >= 0 && start <= len(line) {
		hitFrom, hitTo = start, min(start+length, len(line))
	}
	var lines []string
	var out strings.Builder
	used := 0
	flush := func() {
		lead := strings.Repeat(" ", prefixWidth)
		rendered := style.Render(lead)
		if len(lines) == 0 {
			rendered = style.Faint(!selected).Render(prefix)
		}
		if pad := room - used; pad > 0 {
			out.WriteString(style.Render(strings.Repeat(" ", pad)))
		}
		lines = append(lines, rendered+out.String())
		out.Reset()
		used = 0
	}
	var piece strings.Builder
	pieceStyle, pieceHit := style, false
	endPiece := func() {
		if piece.Len() > 0 {
			out.WriteString(pieceStyle.Render(piece.String()))
			piece.Reset()
		}
	}
	for index, r := range line {
		current, inHit := style, index >= hitFrom && index < hitTo
		if inHit {
			current = hitStyle
		}
		cell := sanitize(string(r))
		width := runewidth.StringWidth(cell)
		if used+width > room && used > 0 {
			endPiece()
			flush()
		}
		if piece.Len() > 0 && inHit != pieceHit {
			endPiece()
		}
		pieceStyle, pieceHit = current, inHit
		piece.WriteString(cell)
		used += width
	}
	endPiece()
	if used > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}
