package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/editor"
	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

const (
	editorTabLines   = 1 // the tab line
	editorFooterRows = 2 // the status line wraps to at most this many lines
	editorMessageMax = 3 // a long message wraps to at most this many lines
	editorScrollOff  = 3
)

// EditorModel is the editor: one file edited with vim keys.
type EditorModel struct {
	theme  Theme
	width  int
	height int

	doc      editor.Document
	vim      *vimbuf.Buffer
	top      int // first line shown
	left     int // first screen column shown
	showNums bool

	syntax *syntaxCache // the colors of the text; a pointer, because View (a value receiver) fills it and the model is copied on every key

	status string
	warn   bool

	nav   CodeNavigator // answers gd and gr; nil without a language server setup
	root  string        // the project folder, for file names in the place list
	jumps []lsp.Place   // where jumps started, newest last (ctrl+o returns)
	list  *placeList    // the open list of places (references, several definitions)

	completion *completionState // the open completion popup
	ambient    string           // what the window is doing in the background (installing tools), shown when there is no message

	clipboard vimbuf.Clipboard // the system clipboard, set again on every buffer that is opened

	forwardEx bool // commands the editor does not know are sent up as ExCommandMsg
	quitting  bool // the user asked to leave
	quitArmed bool // ctrl+d warned about unsaved changes; the next one quits
}

// CloseRequestMsg tells the project window that ctrl+d was pressed in the editor's normal mode: the window decides
// what closes and asks first.
type CloseRequestMsg struct{}

// ExCommandMsg carries an ":" command the editor does not know to the model around it (the project window).
type ExCommandMsg struct{ Command string }

// NewEditorModel opens a document for editing.
func NewEditorModel(doc editor.Document) EditorModel {
	buffer := vimbuf.New(doc.Lines, nil)
	buffer.PassUnknownEx(true)
	m := EditorModel{theme: DarkTheme(), doc: doc, vim: buffer, showNums: true, syntax: newSyntaxCache()}
	if doc.New && doc.Path != "" {
		m.notice(fmt.Sprintf("\"%s\" [New]", doc.Path), false)
	}
	return m
}

// WithTheme returns the model with another theme.
func (m EditorModel) WithTheme(theme Theme) EditorModel {
	m.theme = theme
	return m
}

// WithClipboard connects the editor to the system clipboard: copied and deleted text goes to it, and a paste takes
// what is on it. Both functions may be nil.
func (m EditorModel) WithClipboard(read func() (string, error), write func(string) error) EditorModel {
	m.clipboard = vimbuf.Clipboard{Read: read, Write: write}
	if read != nil || write != nil {
		m.vim.SetClipboard(m.clipboard)
	}
	return m
}

// markQuit records that the user leaves; text that was copied last is written to the clipboard first.
func (m *EditorModel) markQuit() {
	m.quitting = true
	m.vim.FlushClipboard()
}

// WithForwardedCommands makes the editor send commands it does not know up as ExCommandMsg instead of refusing them.
func (m EditorModel) WithForwardedCommands() EditorModel {
	m.forwardEx = true
	return m
}

// Quitting reports whether the user asked to leave the editor.
func (m EditorModel) Quitting() bool { return m.quitting }

// Modified reports whether the text has unsaved changes.
func (m EditorModel) Modified() bool { return m.vim.Modified() }

// Cursor returns the cursor line and rune column (zero-based).
func (m EditorModel) Cursor() (row, col int) { return m.vim.Cursor() }

// At returns the model with the cursor on a line and column and the view scrolled to it.
func (m EditorModel) At(row, col int) EditorModel {
	m.vim.SetCursor(row, col)
	m.scrollToCursor()
	return m
}

// OpenFile loads a file into the editor; unsaved changes block it unless force is set (the reason is shown as a message).
func (m EditorModel) OpenFile(path string, force bool) EditorModel {
	m.edit(path, force)
	return m
}

// Lines returns the text being edited.
func (m EditorModel) Lines() []string { return m.vim.Lines() }

// Path returns the file being edited.
func (m EditorModel) Path() string { return m.doc.Path }

func (m *EditorModel) notice(text string, warn bool) { m.status, m.warn = text, warn && text != "" }

// Init implements tea.Model.
func (m EditorModel) Init() tea.Cmd { return nil }

// bodyHeight is the height of the text: the window without the tab line, the message and the status line, which grow with their
// text when the window is narrow.
func (m EditorModel) bodyHeight() int {
	return max(m.height-editorTabLines-len(m.messageRows())-len(m.footerRows()), 1)
}

// footerRows is the status line as wrapped bars: it takes more than one line when its commands do not fit in the width.
func (m EditorModel) footerRows() []*bar { return m.footerBar().wrap(m.width, editorFooterRows) }

// Update implements tea.Model.
func (m EditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(syntaxTickMsg); ok {
		if tick.cache != m.syntax { // the tick of another file
			return m, nil
		}
		return m, m.syntax.tick(m.doc.Path, m.vim)
	}
	model, cmd := m.update(msg)
	if updated, ok := model.(EditorModel); ok {
		return model, tea.Batch(cmd, updated.syntax.schedule(updated.vim))
	}
	return model, cmd
}

func (m EditorModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vim.SetPage(m.bodyHeight())
		m.scrollToCursor()
	case completionResultMsg:
		return m.handleCompletionResult(msg), nil
	case formatResultMsg:
		return m.handleFormatResult(msg)
	case navResultMsg:
		return m.handleNavResult(msg)
	case JumpMsg:
		return m.handleJump(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m EditorModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notice("", false)
	if m.forwardEx && (msg.String() == paletteKey || msg.String() == "f1") { // the project window opens its palette
		return m, func() tea.Msg { return ExCommandMsg{Command: "palette"} }
	}
	if msg.String() == "ctrl+s" { // like :w, in every mode: the editor stays as it is (an insert stays an insert)
		m.save("")
		return m, nil
	}
	if m.list != nil {
		return m.handleListKey(msg)
	}
	if m.vim.Mode() == vimbuf.Insert {
		key := msg.String()
		if m.completion != nil {
			if model, used := m.completionKey(key); used {
				return model, nil
			}
		}
		if isCompletionKey(key) {
			return m.startCompletion()
		}
	}
	if model, cmd, used := m.navigationKey(msg.String()); used {
		return model, cmd
	}
	// After a jump (gd, gr, a search result) ctrl+c and ctrl+d go back to where it started, one step at a time; only without
	// a jump to undo they close the editor.
	if key := msg.String(); (key == "ctrl+d" || key == "ctrl+c") && m.vim.Mode() == vimbuf.Normal && m.vim.Pending() == "" && len(m.jumps) > 0 {
		return m.jumpBack()
	}
	// ctrl+d closes the editor from normal mode (ctrl+c never closes: it only returns to normal mode, like in vim). In the project window (forwardEx) the key becomes a
	// CloseRequestMsg and the window asks before it closes anything. Standing alone the editor closes at once when nothing
	// changed, otherwise the first press warns and the second one quits without saving. In the other modes ctrl+c still
	// returns to normal mode and ctrl+d keeps its vim meaning.
	if msg.String() == "ctrl+d" && m.vim.Mode() == vimbuf.Normal && m.vim.Pending() == "" {
		if m.forwardEx { // in the project window: it asks before it closes
			return m, func() tea.Msg { return CloseRequestMsg{} }
		}
		if !m.vim.Modified() || m.quitArmed {
			m.markQuit()
			return m, tea.Quit
		}
		m.quitArmed = true
		m.notice("Unsaved changes: press ctrl+d again to quit without saving, or :w to save", true)
		return m, nil
	}
	m.quitArmed = false
	if msg.Paste && msg.Type == tea.KeyRunes { // text pasted by the terminal is text, never commands
		m.vim.PasteText(string(msg.Runes))
		m.scrollToCursor()
		return m, nil
	}
	var cmds []tea.Cmd
	for _, key := range editorKeys(msg) {
		if key == "ctrl+d" {
			key = "ctrl+c" // in insert, visual and command mode, and with a pending command, ctrl+d does what ctrl+c does
		}
		switch m.vim.Handle(key) {
		case vimbuf.Write:
			m.save("")
		case vimbuf.WriteQuit:
			if m.save("") {
				m.markQuit()
				return m, tea.Quit
			}
		case vimbuf.Quit, vimbuf.ForceQuit:
			m.markQuit()
			return m, tea.Quit
		case vimbuf.Ex:
			cmd := m.runEx(m.vim.ExCommand())
			if m.quitting {
				return m, tea.Quit
			}
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if text := m.vim.Message(); text != "" {
		m.notice(text, strings.HasPrefix(text, "E") || strings.HasPrefix(text, "search hit"))
	}
	if m.completion != nil {
		if m.vim.Mode() != vimbuf.Insert {
			m.completion = nil
		} else {
			m.refilterCompletion()
		}
	}
	m.scrollToCursor()
	return m, tea.Batch(cmds...)
}

// save writes the text to path (the current file when empty) and reports whether it worked.
func (m *EditorModel) save(path string) bool {
	if path == "" {
		path = m.doc.Path
	}
	if path == "" {
		m.notice("E32: No file name", true)
		return false
	}
	lines := m.vim.Lines()
	doc, err := m.doc.Save(path, lines)
	if err != nil {
		m.notice("E212: Can't write file: "+err.Error(), true)
		return false
	}
	if m.doc.Path == "" || path == m.doc.Path {
		m.doc = doc
		m.vim.MarkWritten()
	}
	m.notice(fmt.Sprintf("\"%s\" %dL written", path, len(lines)), false)
	return true
}

// runEx runs a command the vim engine did not handle itself.
func (m *EditorModel) runEx(command string) tea.Cmd {
	name, argument, _ := strings.Cut(command, " ")
	argument = strings.TrimSpace(argument)
	switch name {
	case "w", "write":
		m.save(argument)
	case "wq", "x", "xit":
		if m.save(argument) {
			m.markQuit()
			return tea.Quit
		}
	case "e", "edit", "e!", "edit!":
		m.edit(argument, strings.HasSuffix(name, "!"))
	case "set":
		m.set(argument)
	case "format", "fmt", "Format":
		return m.formatCommand()
	default:
		if m.forwardEx {
			return func() tea.Msg { return ExCommandMsg{Command: command} }
		}
		m.notice("E492: Not an editor command: "+command, true)
	}
	return nil
}

// edit loads another file into the buffer; unsaved changes need the bang.
func (m *EditorModel) edit(path string, force bool) {
	if path == "" {
		path = m.doc.Path
		force = true // ":e" with no name re-reads the file
	}
	if m.vim.Modified() && !force {
		m.notice("E37: No write since last change (add ! to override)", true)
		return
	}
	doc, err := editor.Load(path)
	if err != nil {
		m.notice(err.Error(), true)
		return
	}
	*m = m.opened(doc)
}

// opened returns the model showing doc.
func (m EditorModel) opened(doc editor.Document) EditorModel {
	m.doc, m.vim = doc, vimbuf.New(doc.Lines, nil)
	m.vim.PassUnknownEx(true)
	if m.clipboard.Read != nil || m.clipboard.Write != nil {
		m.vim.SetClipboard(m.clipboard)
	}
	m.vim.SetPage(m.bodyHeight())
	m.top, m.left, m.syntax = 0, 0, newSyntaxCache()
	if doc.New {
		m.notice(fmt.Sprintf("\"%s\" [New]", doc.Path), false)
	} else {
		m.notice(fmt.Sprintf("\"%s\" %dL", doc.Path, len(doc.Lines)), false)
	}
	return m
}

func (m *EditorModel) set(argument string) {
	switch argument {
	case "nu", "number":
		m.showNums = true
	case "nonu", "nonumber":
		m.showNums = false
	default:
		m.notice("E518: Unknown option: "+argument, true)
	}
}

// numberWidth is the width of the line number column.
func (m EditorModel) numberWidth() int {
	if !m.showNums {
		return 0
	}
	return len(fmt.Sprint(max(m.vim.LineCount(), 1))) + 1
}

func (m EditorModel) textWidth() int { return max(m.width-m.numberWidth(), 1) }

// scrollToCursor moves the view so that the cursor line (with a few lines around it) and column are visible.
func (m *EditorModel) scrollToCursor() {
	row, col := m.vim.Cursor()
	body, count := m.textHeight(), m.vim.LineCount()
	margin := min(editorScrollOff, (body-1)/2)
	if row < m.top+margin {
		m.top = row - margin
	}
	if row > m.top+body-1-margin {
		m.top = row - (body - 1 - margin)
	}
	m.top = max(min(m.top, count-1), 0)
	column := cursorColumn([]rune(m.vim.Line(row)), col)
	if column < m.left {
		m.left = column
	}
	if column >= m.left+m.textWidth() {
		m.left = column - m.textWidth() + 1
	}
	m.left = max(m.left, 0)
}

func (m EditorModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.top = max(m.top-wheelLines, 0)
	case tea.MouseButtonWheelDown:
		m.top = min(m.top+wheelLines, max(m.vim.LineCount()-1, 0))
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			break
		}
		if model, cmd, used := m.clickList(msg.Y); used {
			return model, cmd
		}
		if keys := m.footerKeys(msg.X, msg.Y); keys != nil {
			if m.vim.Mode() != vimbuf.Normal { // a command is not text: leave insert, visual or command mode first
				keys = append([]string{"esc"}, keys...)
			}
			return replay(m, keys)
		}
		m.clickText(msg.X, msg.Y)
	}
	return m, nil
}

// clickText puts the cursor where the text was clicked.
func (m *EditorModel) clickText(x, y int) {
	line := m.top + y - 1
	if y < 1 || y > m.textHeight() || line >= m.vim.LineCount() || m.vim.Mode() != vimbuf.Normal {
		return
	}
	column := m.left + x - m.numberWidth()
	if x < m.numberWidth() {
		column = m.left
	}
	m.vim.SetCursor(line, runeAtColumn([]rune(m.vim.Line(line)), column))
	m.scrollToCursor()
}

// footerBar builds the status line: mode, file and position on the left, the clickable commands on the right.
func (m EditorModel) footerBar() *bar {
	result := &bar{}
	name := filepath.Base(m.doc.Path)
	if m.doc.Path == "" {
		name = "[No Name]"
	}
	modified := ""
	if m.vim.Modified() {
		modified = " [+]"
	}
	row, col := m.vim.Cursor()
	result.add(barPart{fmt.Sprintf(" %s  %s%s  %d:%d   ", m.vim.Mode(), name, modified, row+1, col+1), m.theme.Header})
	if m.forwardEx { // in the project window every action is in the palette, which the top bar offers
		return result
	}
	result.command(":w", "save", true, m.theme)
	result.command(":q", "quit", true, m.theme)
	result.command("gd", "def", m.nav != nil, m.theme)
	result.command("gr", "refs", m.nav != nil, m.theme)
	result.command(":fmt", "format", m.nav != nil, m.theme)
	return result
}

func (m EditorModel) footerKeys(x, y int) []string {
	rows := m.footerRows()
	first := m.height - len(rows)
	if y < first || y >= m.height || x < 0 || x >= m.width {
		return nil
	}
	return rows[y-first].keysAt(x)
}

// View implements tea.Model.
func (m EditorModel) View() string {
	if m.width == 0 {
		return ""
	}
	syntax := m.syntax.spansFor(m.doc.Path, m.vim)
	var out strings.Builder
	title := " " + filepath.Base(m.doc.Path)
	if m.doc.Path == "" {
		title = " [No Name]"
	}
	if m.vim.Modified() {
		title += " [+]"
	}
	out.WriteString(m.theme.Header.Render(padRight(title, m.width)))
	out.WriteByte('\n')

	styles := codeLineStyles{
		base: m.theme.Equal, selected: m.theme.Selected,
		cursor: m.theme.CursorStyle(),
		match:  m.theme.Warning.Reverse(true),
	}
	numWidth, textWidth := m.numberWidth(), m.textWidth()
	row, col := m.vim.Cursor()
	popup, popupFrom, popupX, popupWidth := m.popupPlacement()
	height := m.textHeight() // once: it lays out the footer, which is not cheap
	for y := 0; y < height; y++ {
		i := m.top + y
		if i >= m.vim.LineCount() {
			if index := y - popupFrom; popup != nil && index >= 0 && index < len(popup) { // the popup reaches below the text
				out.WriteString(m.theme.LineNumber.Render(strings.Repeat(" ", numWidth)))
				out.WriteString(m.theme.Equal.Render(strings.Repeat(" ", popupX)))
				out.WriteString(popup[index])
				out.WriteString(m.theme.Equal.Render(strings.Repeat(" ", max(textWidth-popupX-popupWidth, 0))))
			} else {
				out.WriteString(m.theme.Filler.Render(padRight("~", m.width)))
			}
			out.WriteByte('\n')
			continue
		}
		if numWidth > 0 {
			style := m.theme.LineNumber
			if i == row {
				style = style.Bold(true)
			}
			out.WriteString(style.Render(fmt.Sprintf("%*d ", numWidth-1, i+1)))
		}
		cursor, from, to := -1, 0, 0
		if i == row {
			cursor = col
		}
		if f, t, ok := m.vim.Selected(i); ok {
			from, to = f, t
		}
		var spans []SyntaxSpan
		if i < len(syntax) {
			spans = syntax[i]
		}
		runes := []rune(m.vim.Line(i))
		lineStyles := styles
		lineStyles.matches = m.vim.Matches(i)
		if index := y - popupFrom; popup != nil && index >= 0 && index < len(popup) {
			// the popup covers part of this line: the text left of it, the popup, the text right of it
			out.WriteString(m.theme.renderCodeLine(runes, spans, m.left, popupX, cursor, from, to, lineStyles))
			out.WriteString(popup[index])
			out.WriteString(m.theme.renderCodeLine(runes, spans, m.left+popupX+popupWidth, max(textWidth-popupX-popupWidth, 0), -1, from, to, lineStyles))
		} else {
			out.WriteString(m.theme.renderCodeLine(runes, spans, m.left, textWidth, cursor, from, to, lineStyles))
		}
		out.WriteByte('\n')
	}
	if m.list != nil {
		for _, line := range m.listLines() {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	for _, row := range m.messageRows() {
		out.WriteString(row)
		out.WriteByte('\n')
	}
	out.WriteString(strings.Join(m.footerBar().renderLines(m.width, editorFooterRows, m.theme.Footer), "\n"))
	return out.String()
}

// popupPlacement returns the lines of the completion popup, the text line (counted from the first shown one) of its first
// row and its column and width in the text area. It appears below the line of the word being completed, or above it when
// there is no room below.
func (m EditorModel) popupPlacement() (lines []string, firstRow, x, width int) {
	if m.completion == nil || len(m.completion.items) == 0 {
		return nil, 0, 0, 0
	}
	textWidth := m.textWidth()
	width = m.completionWidth(textWidth)
	lines = m.completionLines(width)
	row, _ := m.vim.Cursor()
	cursorY := row - m.top
	firstRow = cursorY + 1
	if firstRow+len(lines) > m.textHeight() && cursorY-len(lines) >= 0 {
		firstRow = cursorY - len(lines)
	}
	column := cursorColumn([]rune(m.vim.Line(row)), m.completion.start) - m.left
	x = min(max(column, 0), max(textWidth-width, 0))
	return lines, firstRow, x, width
}

// messageRows are the lines above the status line: the command being typed, or the last message, wrapped at the spaces when it is
// longer than the window is wide (instead of cut off).
func (m EditorModel) messageRows() []string {
	width := max(m.width, 1)
	wrapped := func(text string, style lipgloss.Style) []string {
		return messageLines(text, width, editorMessageMax, style)
	}
	switch {
	case m.vim.Mode() == vimbuf.Command:
		return []string{renderText(m.vim.CommandPrefix()+m.vim.CommandLine(), nil, width, m.theme.Equal, m.theme.Equal)}
	case m.ambient != "" && m.status == "" && m.completion == nil:
		return wrapped(sanitize(m.ambient), m.theme.Equal.Faint(true))
	case m.completion != nil && m.status == "":
		return []string{m.theme.Equal.Faint(true).Render(padRight(m.completionHint(), width))}
	case m.status != "" && m.warn:
		return wrapped(sanitize(m.status), m.theme.Warning)
	case m.status != "":
		return wrapped(sanitize(m.status), m.theme.Equal)
	case m.vim.Pending() != "":
		return []string{m.theme.Equal.Render(padRight(" "+m.vim.Pending(), width))}
	}
	return []string{lipgloss.NewStyle().Render(strings.Repeat(" ", width))}
}
