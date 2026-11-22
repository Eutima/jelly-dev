package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/source"
)

const (
	minListWidth = 20
	maxListWidth = 44
	listDivisor  = 3 // the file list takes at most 1/3 of the terminal
	separator    = "│"
	separatorW   = 1
)

// ChangeSource provides the texts of a changed file (implemented by source.GitChanges).
type ChangeSource interface {
	Labels() (oldLabel, newLabel string)
	// Load returns the old and new text of a file. It must stop early and return ctx.Err() when ctx is
	// cancelled (the user selected another file).
	Load(ctx context.Context, file source.ChangedFile) (oldText, newText string, err error)
}

type focus int

const (
	focusList focus = iota
	focusDiff
)

// GitModel shows a list of changed files next to the diff of the selected file.
type GitModel struct {
	files    []source.ChangedFile
	source   ChangeSource
	selected int
	listTop  int // first visible list row
	focus    focus
	diff     DiffModel
	message  string // shown instead of the diff when the file cannot be displayed
	theme    Theme

	loading     bool               // a file is being loaded in the background
	loadingFile int                // index of the file being loaded
	loadedFile  int                // index of the file whose diff is shown, -1 when none
	loadSeq     int                // number of the current load; results of older loads are discarded
	loadCtx     context.Context    // context of the current load; kept so Init can start the first load
	cancelLoad  context.CancelFunc // cancels the running load; nil when none
	newRaw      string             // the text of the shown file as it is on disk (for edits: line ends and the final newline are kept)
	undo        []string           // the texts the file had before each edit and revert of the shown file, newest last
	editor      *diffEditor        // the full-screen editor, nil when closed
	palette     *diffPalette       // the list of all actions, nil when closed
	arrive      int                // after the load: 1 selects the first change, -1 the last (n and p crossing a file border)
	ticking     bool               // the wobble animation is scheduled
	frame       int                // current animation frame
	interval    time.Duration

	width, height int
}

// loadedMsg carries the result of a background file load.
type loadedMsg struct {
	seq     int // the load this result belongs to; stale results are ignored
	index   int
	diff    DiffModel
	message string // set instead of a meaningful diff when the file cannot be shown
	newText string // the text of the new side as loaded
}

// NewGitModel builds the viewer; the diff of the first file is loaded in the background (see Init).
func NewGitModel(files []source.ChangedFile, src ChangeSource) GitModel {
	model := GitModel{files: files, source: src, theme: DarkTheme(), loadedFile: -1, interval: JellyInterval}
	if len(files) > 0 {
		model.beginLoad(0)
		model.ticking = true
	}
	return model
}

// FileCount returns the number of changed files.
func (m GitModel) FileCount() int { return len(m.files) }

// Init implements tea.Model.
func (m GitModel) Init() tea.Cmd {
	if !m.loading {
		return nil
	}
	return tea.Batch(m.loadCmd(m.loadCtx, m.loadSeq, m.loadingFile), jellyTick(m.interval))
}

func (m GitModel) listWidth() int {
	return min(max(m.width/listDivisor, minListWidth), maxListWidth, max(m.width/2, 0))
}

func (m GitModel) diffWidth() int { return max(m.width-m.listWidth()-separatorW, 0) }

// selectFile selects file i. Its diff is loaded unless it is already shown; a load of another file that
// is still running is cancelled, so only the latest selection is ever loaded.
func (m *GitModel) selectFile(i int) tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	m.selected = min(max(i, 0), len(m.files)-1)
	m.ensureVisible()
	switch {
	case m.selected == m.loadedFile:
		m.abortLoad() // back on the shown file: whatever loads in the background is no longer wanted
		return nil
	case m.loading && m.loadingFile == m.selected:
		return nil
	}
	m.beginLoad(m.selected)
	return tea.Batch(m.loadCmd(m.loadCtx, m.loadSeq, m.selected), m.startTick())
}

// beginLoad cancels a running load and registers a new one for file index. (abortLoad and beginLoad both
// advance the sequence number; only the final value matters, so the double step is harmless.)
func (m *GitModel) beginLoad(index int) {
	m.abortLoad()
	m.loadSeq++
	m.loadCtx, m.cancelLoad = context.WithCancel(context.Background())
	m.loading, m.loadingFile = true, index
}

// abortLoad cancels the running load, if any; its result will be ignored.
func (m *GitModel) abortLoad() {
	if m.cancelLoad != nil {
		m.cancelLoad()
		m.cancelLoad = nil
	}
	if m.loading {
		m.loading = false
		m.loadSeq++
	}
}

// startTick starts the wobble animation unless it already runs.
func (m *GitModel) startTick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return jellyTick(m.interval)
}

// loadCmd loads file index and computes its diff off the UI goroutine. It returns early once ctx is cancelled.
func (m GitModel) loadCmd(ctx context.Context, seq, index int) tea.Cmd {
	file := m.files[index]
	src, theme := m.source, m.theme
	oldLabel, newLabel := src.Labels()
	oldPath := file.Path
	if file.OldPath != "" {
		oldPath = file.OldPath
	}
	leftTitle, rightTitle := oldLabel+": "+oldPath, newLabel+": "+file.Path
	return func() tea.Msg {
		cancelled := loadedMsg{seq: seq, index: index}
		oldText, newText, err := src.Load(ctx, file)
		if ctx.Err() != nil {
			return cancelled
		}
		if err != nil {
			return loadedMsg{
				seq: seq, index: index, message: describeLoadError(err),
				diff: NewDiffModel(leftTitle, rightTitle, nil, nil).WithTheme(theme),
			}
		}
		model := NewDiffModel(leftTitle, rightTitle, diff.SplitLines(oldText), diff.SplitLines(newText)).
			WithTheme(theme)
		if ctx.Err() != nil {
			return cancelled // skip the expensive highlighting
		}
		return loadedMsg{seq: seq, index: index, newText: newText, diff: model.WithHighlight(file.Path, file.OldPath)}
	}
}

// applyLoaded shows a finished load; results of cancelled or replaced loads are ignored.
func (m *GitModel) applyLoaded(msg loadedMsg) tea.Cmd {
	if msg.seq != m.loadSeq || !m.loading {
		return nil
	}
	m.loading = false
	if m.cancelLoad != nil {
		m.cancelLoad() // release the context's resources
		m.cancelLoad = nil
	}
	m.loadedFile = msg.index
	m.message = msg.message
	m.newRaw, m.undo, m.editor = msg.newText, nil, nil
	msg.diff.editable = msg.message == "" && m.fileEditable(m.files[msg.index])
	m.diff = m.sizedDiff(msg.diff)
	if m.arrive != 0 {
		m.diff.gotoHunk(map[bool]int{true: 0, false: -1}[m.arrive > 0])
		m.arrive = 0
	}
	return nil
}

// diffCommand is the command of the diff scope a key runs in the git view. A few keys do something else here than in the diff view: esc
// steps from the diff back to the list (it quits from the list), and enter and backspace only edit and revert in the diff pane.
func (m GitModel) diffCommand(key string) keymap.Command {
	command, _ := keys.Lookup(keymap.ScopeDiff, key)
	switch {
	case command == keymap.DiffQuit && key == "esc":
		return ""
	case (command == keymap.DiffEdit || command == keymap.DiffRevert) && m.focus != focusDiff && (key == "enter" || key == "backspace" || key == "delete"):
		return ""
	case m.focus != focusDiff && (command == keymap.DiffNext || command == keymap.DiffPrev):
		return ""
	}
	return command
}

// stepChange handles arrow down (direction 1) and up (-1) in the diff pane: the next or previous change, and at the last or first change of the
// file the first or last change of the next or previous file. At the end of the list the change wraps within the file.
func (m GitModel) stepChange(msg tea.KeyMsg, direction int) (tea.Model, tea.Cmd) {
	if m.loading { // the shown diff is about to be replaced
		return m, nil
	}
	count, current := len(m.diff.starts()), m.diff.current
	atEnd := count == 0 || (direction > 0 && current == count-1) || (direction < 0 && current <= 0)
	next := m.selected + direction
	if !atEnd || next < 0 || next >= len(m.files) {
		return m.forwardToDiff(msg)
	}
	cmd := m.selectFile(next)
	m.arrive = direction
	return m, cmd
}

func describeLoadError(err error) string {
	switch {
	case errors.Is(err, source.ErrBinary):
		return "Binary file - not shown"
	case errors.Is(err, source.ErrTooLarge):
		return "File too large - not shown"
	case errors.Is(err, source.ErrNotRegular):
		return "Not a regular file (directory, submodule or special file) - not shown"
	case errors.Is(err, source.ErrOutsideRepository):
		return "Path leaves the repository through a symbolic link - not shown"
	case errors.Is(err, source.ErrUnmerged):
		return "Unmerged file (conflict) - resolve it with the merge tool first"
	case errors.Is(err, source.ErrSubmodule):
		return "Submodule - commit change not shown"
	default:
		firstLine, _, _ := strings.Cut(err.Error(), "\n")
		return "Cannot show this file: " + firstLine
	}
}

func (m GitModel) listBodyHeight() int { return max(m.height-headerLines-m.footerHeight(), 1) }

// footerHeight is the number of footer lines of both panes: the diff footer is as high as the taller of the two.
func (m GitModel) footerHeight() int { return len(m.diff.footerRows()) }

// listFooterRows is the footer under the file list as wrapped bars.
// It is padded to the height of the diff footer.
func (m GitModel) listFooterRows() []*bar {
	return padRows(m.listFooterBar().wrap(m.listWidth(), footerMax), m.footerHeight())
}

// sizedDiff fits a diff into the right pane and tells it how high the list footer is.
func (m GitModel) sizedDiff(d DiffModel) DiffModel {
	d.footerMin = len(m.listFooterBar().wrap(m.listWidth(), footerMax))
	return d.withSize(m.diffWidth(), m.height)
}

func (m *GitModel) ensureVisible() {
	width := m.listWidth()
	m.listTop = scrollToShow(m.selected, m.listTop, m.listBodyHeight(), len(m.files), func(row int) int { return len(m.fileRowLines(row, width)) })
}

// Update implements tea.Model.
func (m GitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.diff = m.sizedDiff(m.diff)
		m.ensureVisible()
		if m.editor != nil {
			m.editor.vim.SetPage(m.editorBodyHeight())
			m.keepEditorVisible()
		}
		return m, nil
	case loadedMsg:
		cmd := m.applyLoaded(msg)
		return m, cmd
	case jellyTickMsg:
		if !m.isLoadingHint() {
			m.ticking = false // nothing waits any more: stop animating
			return m, nil
		}
		m.frame++
		return m, jellyTick(m.interval)
	case tea.MouseMsg:
		if m.palette != nil {
			return m.updatePalette(msg)
		}
		if m.editor != nil {
			if leftPress(msg) {
				if keys := rowKeysAt(m.editorFooterRows(), msg.X, msg.Y, m.width, m.height); keys != nil {
					return replay(m, keys)
				}
			}
			return m, nil
		}
		if leftPress(msg) {
			if keys := m.footerKeys(msg.X, msg.Y); keys != nil {
				return replay(m, keys)
			}
		}
		return m.forwardToDiff(msg)
	case tea.KeyMsg:
		if m.palette != nil {
			return m.updatePalette(msg)
		}
		if m.editor != nil {
			return m.handleEditorKey(msg)
		}
		m.say("", false)
		return m.handleKey(msg)
	}
	return m, nil
}

// updatePalette passes a key or click to the open palette; a chosen action runs as if its key was pressed.
func (m GitModel) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	var command keymap.Command
	m.palette, command = m.palette.update(msg, m.width, m.height)
	if command == "" {
		return m, nil
	}
	return runPaletteCommand(m, command)
}

func (m GitModel) forwardToDiff(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.diff.Update(msg)
	m.diff = updated.(DiffModel)
	return m, cmd
}

func (m GitModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "tab" { // moves the focus between the list and the diff, whatever diff.next says
		m.focus = 1 - m.focus
		return m, nil
	}
	command := m.diffCommand(key)
	if key == "ctrl+c" { // the way out stays, whatever diff.quit is bound to
		command = keymap.DiffQuit
	}
	switch command {
	case keymap.DiffPalette:
		m.palette = newDiffPalette(m.theme, m.paletteActions())
		return m, nil
	case keymap.DiffQuit:
		m.abortLoad() // also stops a running git process
		return m, tea.Quit
	case keymap.DiffSplit:
		return m.forwardToDiff(msg)
	case keymap.DiffRevert:
		return m.revertChange()
	case keymap.DiffUndo:
		return m.undoEdit()
	case keymap.DiffEdit:
		return m.openEditor()
	}
	if m.focus == focusDiff {
		switch {
		case command == keymap.DiffNext:
			return m.stepChange(msg, 1)
		case command == keymap.DiffPrev:
			return m.stepChange(msg, -1)
		case key == "esc" || key == "left" || key == "h":
			m.focus = focusList
			return m, nil
		}
		return m.forwardToDiff(msg)
	}
	var cmd tea.Cmd
	switch key {
	case "esc":
		m.abortLoad()
		return m, tea.Quit
	case "enter", "right", "l":
		m.focus = focusDiff
	case "j", "down":
		cmd = m.selectFile(m.selected + 1)
	case "k", "up":
		cmd = m.selectFile(m.selected - 1)
	case "pgdown":
		cmd = m.selectFile(m.selected + m.listBodyHeight())
	case "pgup":
		cmd = m.selectFile(m.selected - m.listBodyHeight())
	case "g", "home":
		cmd = m.selectFile(0)
	case "G", "end":
		cmd = m.selectFile(len(m.files) - 1)
	}
	return m, cmd
}

// View implements tea.Model.
func (m GitModel) View() string {
	if m.width == 0 {
		return ""
	}
	if m.editor != nil {
		return m.editorView()
	}
	if m.palette != nil {
		return m.palette.picker.overlay(m.plainView(), m.width, m.height)
	}
	return m.plainView()
}

// plainView is the list next to the diff.
func (m GitModel) plainView() string {
	list := m.listView()
	right := m.diff.View()
	if message := m.paneMessage(); message != "" {
		right = m.messageView(message)
	}
	divider := strings.TrimSuffix(strings.Repeat(m.theme.Gutter.Render(separator)+"\n", m.height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, divider, right)
}

func (m GitModel) listView() string {
	width := m.listWidth()
	lines := make([]string, 0, m.height)
	lines = append(lines, m.theme.Header.Render(padRight(fmt.Sprintf(" Changes (%d)", len(m.files)), width)))
	body := m.listBodyHeight()
	shown := 0
	for index := m.listTop; index < len(m.files) && shown < body; index++ {
		for _, line := range m.fileRowLines(index, width) {
			if shown == body {
				break
			}
			lines = append(lines, line)
			shown++
		}
	}
	for ; shown < body; shown++ {
		lines = append(lines, strings.Repeat(" ", width))
	}
	lines = append(lines, renderRowLines(m.listFooterRows(), width, m.theme.Footer)...)
	return strings.Join(lines, "\n")
}

// listFooterBar builds the footer under the file list.
func (m GitModel) listFooterBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range [][2]string{{"↑↓", "files"}, {"enter", "open"}, {"tab", "focus"}} {
		result.command(h[0], h[1], true, m.theme)
	}
	result.binding(keymap.DiffQuit, "quit", true, m.theme)
	result.binding(keymap.DiffPalette, "actions", true, m.theme)
	return result
}

// footerKeys returns the keys of the footer command at a screen position: the list footer on the left, the diff
// footer on the right of the divider.
func (m GitModel) footerKeys(x, y int) []string {
	list := m.listWidth()
	switch {
	case x < list:
		return rowKeysAt(m.listFooterRows(), x, y, list, m.height)
	case x >= list+separatorW:
		return m.diff.footerKeys(x-list-separatorW, y)
	}
	return nil
}

// fileRowLines draws the row of a file: a long name continues on further lines.
func (m GitModel) fileRowLines(index, width int) []string {
	file := m.files[index]
	label := sanitize(file.Path)
	if file.OldPath != "" {
		label = sanitize(file.OldPath) + " → " + label
	}
	style := m.theme.Equal
	if index == m.selected {
		style = m.theme.SelectedDim
		if m.focus == focusList {
			style = m.theme.Selected
		}
	}
	status := m.theme.statusStyle(file.Status).Inherit(style).Render(" " + string(rune(file.Status)) + " ")
	parts := splitRowLines(label, max(width-3, 1))
	lines := make([]string, len(parts))
	for i, part := range parts {
		text := style.Render(padRight(part, max(width-3, 0)))
		if i == 0 {
			lines[i] = status + text
		} else {
			lines[i] = style.Render("   ") + text
		}
	}
	return lines
}

// isLoadingHint reports whether the right pane currently shows the loading animation.
func (m GitModel) isLoadingHint() bool {
	return len(m.files) > 0 && m.loadedFile != m.selected
}

// paneMessage returns the text that replaces the diff pane: a loading hint or the reason a file is not shown.
func (m GitModel) paneMessage() string {
	if len(m.files) == 0 {
		return ""
	}
	if m.loadedFile != m.selected {
		return "Loading " + m.files[m.selected].Path + " ..."
	}
	return m.message
}

// messageView draws the right pane for files that cannot be shown.
func (m GitModel) messageView(message string) string {
	width := m.diffWidth()
	lines := make([]string, 0, m.height)
	for _, part := range splitRowLines(" "+sanitize(m.files[m.selected].Path), width) {
		lines = append(lines, m.theme.Header.Render(padRight(part, width)))
	}
	plain := m.theme.Equal.UnsetBackground()
	body := m.listBodyHeight() - (len(lines) - 1) // a long path takes more than one header line
	animate := m.isLoadingHint() && body >= minJellyBody
	row := 0
	blank := func() { lines = append(lines, plain.Render(strings.Repeat(" ", width))); row++ }
	if animate {
		blank()
		for _, frame := range JellyFrame(m.frame) {
			lines = append(lines, m.theme.Jelly.UnsetBackground().Render(padRight(centered(sanitize(frame), width), width)))
			row++
		}
		blank()
	} else {
		blank()
	}
	// the message wraps at the spaces (a word longer than a line is cut into pieces); sanitize strips control characters
	text := "  " + message
	if animate {
		text = centered(message, width)
	}
	for _, part := range wrapWords(sanitize(text), max(width, 1), max(body-row, 1)) {
		lines = append(lines, plain.Render(padRight(part, width)))
		row++
	}
	for ; row < body; row++ {
		blank()
	}
	for i := 0; i < m.footerHeight(); i++ {
		lines = append(lines, m.theme.Footer.Render(padRight("", width)))
	}
	return strings.Join(lines, "\n")
}

// withSize returns the model resized to the given terminal area.
func (m DiffModel) withSize(width, height int) DiffModel {
	m.width, m.height = width, height
	m.clamp()
	return m
}
