package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/source"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// ChangeWriter is implemented by change sources whose new side is a working tree: the git view can then revert and edit
// changes (implemented by source.GitChanges).
type ChangeWriter interface {
	// Editable reports whether the new side of a file can be written.
	Editable(file source.ChangedFile) bool
	// WriteWorktree writes text to the file; it refuses when the file is not expected any more.
	WriteWorktree(ctx context.Context, file source.ChangedFile, expected, text string) error
}

var _ ChangeWriter = (*source.GitChanges)(nil) // without it the view would silently stay read-only

// diffEditor is the full-screen vim-style editor of the new side of the shown file.
type diffEditor struct {
	vim *vimbuf.Buffer
	top int // first visible line
}

// fileEditable reports whether the new side of a file can be changed in this view.
func (m GitModel) fileEditable(file source.ChangedFile) bool {
	writer, ok := m.source.(ChangeWriter)
	return ok && writer.Editable(file)
}

// say shows a message in the footer of the diff until the next key.
func (m *GitModel) say(text string, warn bool) {
	m.diff.notice, m.diff.noticeWarn = text, warn
}

// joinText is the inverse of diff.SplitLines for a file that was read as like: it keeps the line ends (CRLF when the file
// uses them) and the final newline (or the lack of it).
func joinText(lines []string, like string) string {
	if len(lines) == 0 {
		return ""
	}
	eol := "\n"
	if strings.Contains(like, "\r\n") {
		eol = "\r\n"
	}
	text := strings.Join(lines, eol)
	if like == "" || strings.HasSuffix(like, "\n") {
		text += eol
	}
	return text
}

// commit writes a new text of the shown file and shows the result: the diff is built again, the selected change stays.
func (m *GitModel) commit(text string) error {
	file := m.files[m.selected]
	writer, ok := m.source.(ChangeWriter)
	if !ok || !m.diff.editable {
		return source.ErrReadOnlySide
	}
	if err := writer.WriteWorktree(context.Background(), file, m.newRaw, text); err != nil {
		return err
	}
	m.undo = append(m.undo, m.newRaw)
	m.show(text)
	return nil
}

// show builds the diff of the shown file again for a new text of its new side.
func (m *GitModel) show(text string) {
	file := m.files[m.selected]
	old := m.diff
	fresh := NewDiffModel(old.leftTitle, old.rightTitle, old.oldLines, diff.SplitLines(text)).WithTheme(m.theme).
		WithHighlight(file.Path, file.OldPath)
	fresh.showUnified, fresh.editable, fresh.footerMin, fresh.offset = old.showUnified, old.editable, old.footerMin, old.offset
	fresh.current = min(old.current, fresh.HunkCount()-1)
	fresh.canUndo = len(m.undo) > 0
	m.newRaw = text
	m.diff = m.sizedDiff(fresh)
	if fresh.current >= 0 {
		m.diff.gotoHunk(fresh.current)
	}
}

// revertChange gives the change under the cursor back its old lines.
func (m GitModel) revertChange() (tea.Model, tea.Cmd) {
	switch {
	case !m.diff.editable:
		m.say("This file cannot be changed here", true)
	case m.diff.current < 0 || m.diff.current >= len(m.diff.changes):
		m.say("Select a change first (↓)", true)
	default:
		block := m.diff.changes[m.diff.current]
		lines := slices.Concat(m.diff.newLines[:block.NewStart], block.Old, m.diff.newLines[block.NewStart+len(block.New):])
		if err := m.commit(joinText(lines, m.newRaw)); err != nil {
			m.say(describeWriteError(err), true)
		} else {
			m.say("✓ Change reverted (u undoes)", false)
		}
	}
	return m, nil
}

// undoEdit takes back the last edit or revert of the shown file.
func (m GitModel) undoEdit() (tea.Model, tea.Cmd) {
	if !m.diff.editable || len(m.undo) == 0 {
		m.say("Nothing to undo", true)
		return m, nil
	}
	previous := m.undo[len(m.undo)-1]
	writer := m.source.(ChangeWriter) // editable implies a writer
	if err := writer.WriteWorktree(context.Background(), m.files[m.selected], m.newRaw, previous); err != nil {
		m.say(describeWriteError(err), true)
		return m, nil
	}
	m.undo = m.undo[:len(m.undo)-1]
	m.show(previous)
	m.say("✓ Undone", false)
	return m, nil
}

func describeWriteError(err error) string {
	switch {
	case errors.Is(err, source.ErrChangedOnDisk), errors.Is(err, source.ErrReadOnlySide):
		return err.Error()
	}
	firstLine, _, _ := strings.Cut(err.Error(), "\n")
	return "Cannot write the file: " + firstLine
}

// openEditor opens the editor on the new side of the shown file, on the first line of the selected change.
func (m GitModel) openEditor() (tea.Model, tea.Cmd) {
	if !m.diff.editable {
		m.say("This file cannot be changed here", true)
		return m, nil
	}
	lines := slices.Clone(m.diff.newLines)
	buffer := vimbuf.New(lines, make([]int, len(lines)))
	buffer.SetPage(m.editorBodyHeight())
	if m.diff.current >= 0 && m.diff.current < len(m.diff.changes) && len(lines) > 0 {
		buffer.GotoLine(min(m.diff.changes[m.diff.current].NewStart+1, len(lines)))
	}
	m.editor = &diffEditor{vim: buffer}
	m.keepEditorVisible()
	m.say("", false)
	return m, nil
}

// handleEditorKey feeds a key press to the editor and carries out :w, :wq, :q and :q!.
func (m GitModel) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.editor
	m.say("", false)
	if msg.String() == "ctrl+s" { // like :w, in every mode
		m.saveEditor()
		return m, nil
	}
	if msg.String() == "ctrl+d" && e.vim.Mode() == vimbuf.Normal && e.vim.Pending() == "" { // like :q
		if e.vim.Modified() {
			m.say("Unsaved changes: :w saves, :q! discards", true)
			return m, nil
		}
		m.editor = nil
		return m, nil
	}
	for _, k := range editorKeys(msg) {
		switch e.vim.Handle(k) {
		case vimbuf.Write:
			m.saveEditor()
		case vimbuf.WriteQuit:
			if m.saveEditor() {
				m.editor = nil
			}
		case vimbuf.Quit, vimbuf.ForceQuit:
			m.editor = nil
		}
		if m.editor == nil {
			return m, nil
		}
	}
	if text := e.vim.Message(); text != "" {
		m.say(text, true)
	}
	m.keepEditorVisible()
	return m, nil
}

// saveEditor writes the edited text to the file; it reports whether that worked (or there was nothing to write).
func (m *GitModel) saveEditor() bool {
	e := m.editor
	text := joinText(e.vim.Lines(), m.newRaw)
	if text == m.newRaw {
		e.vim.MarkWritten()
		m.say("No changes to write", false)
		return true
	}
	if err := m.commit(text); err != nil {
		m.say(describeWriteError(err), true)
		return false
	}
	e.vim.MarkWritten()
	m.say("✓ Saved (u undoes)", false)
	return true
}

func (m GitModel) editorFooterRows() []*bar {
	status := m.editorStatus()
	rows := textRows(status, m.width, 1, m.theme.Header)
	return append(rows, m.editorBar().wrap(m.width, footerMax)...)
}

// editorBodyHeight is the number of text lines of the editor: the status line is always one line high.
func (m GitModel) editorBodyHeight() int {
	return max(m.height-headerLines-1-len(m.editorBar().wrap(m.width, footerMax)), 1)
}

func (m GitModel) editorStatus() string {
	e := m.editor
	switch {
	case m.diff.notice != "":
		return m.diff.notice
	case e.vim.Mode() == vimbuf.Command:
		return ":" + e.vim.CommandLine()
	}
	row, col := e.vim.Cursor()
	pending := ""
	if p := e.vim.Pending(); p != "" {
		pending = "   " + p
	}
	modified := ""
	if e.vim.Modified() {
		modified = "  [+]"
	}
	return fmt.Sprintf("%s%s%s   %d:%d", e.vim.Mode(), modified, pending, row+1, col+1)
}

func (m GitModel) editorBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range [][2]string{{"esc", "normal"}, {"i", "insert"}, {"v/V", "visual"}, {":w", "save"}, {":wq", "save+leave"},
		{":q", "leave"}, {":q!", "discard"}} {
		result.command(h[0], h[1], true, m.theme)
	}
	return result
}

// keepEditorVisible scrolls the editor so that its cursor line is shown.
func (m *GitModel) keepEditorVisible() {
	e := m.editor
	row, _ := e.vim.Cursor()
	body := m.editorBodyHeight()
	if row < e.top {
		e.top = row
	}
	if row >= e.top+body {
		e.top = row - body + 1
	}
	e.top = max(min(e.top, e.vim.LineCount()-1), 0)
}

// editorView draws the editor: a title line, the text, the status line and the commands.
func (m GitModel) editorView() string {
	e := m.editor
	file := m.files[m.selected]
	var out strings.Builder
	out.WriteString(m.theme.Header.Render(padRight(" Editing "+sanitize(file.Path), m.width)))
	out.WriteByte('\n')
	row, col := e.vim.Cursor()
	numWidth := len(fmt.Sprint(max(e.vim.LineCount(), 1))) + 1
	textWidth := max(m.width-numWidth, 0)
	styles := editLineStyles{base: m.theme.Equal, selected: m.theme.Selected, cursor: m.theme.Selected.Reverse(!m.theme.Selected.GetReverse())}
	for i := e.top; i < e.top+m.editorBodyHeight(); i++ {
		if i >= e.vim.LineCount() {
			out.WriteString(m.theme.Equal.Render(strings.Repeat(" ", m.width)))
			out.WriteByte('\n')
			continue
		}
		label, width := fmt.Sprintf("%*d ", numWidth-1, i+1), textWidth
		if m.width < numWidth {
			label, width = "", m.width
		}
		cursor, from, to := -1, 0, 0
		if i == row {
			cursor = col
		}
		if f, t, ok := e.vim.Selected(i); ok {
			from, to = f, t
		}
		out.WriteString(m.theme.LineNumber.Render(label))
		out.WriteString(renderEditLine([]rune(e.vim.Line(i)), width, cursor, from, to, styles))
		out.WriteByte('\n')
	}
	out.WriteString(renderRows(m.editorFooterRows(), m.width, m.theme.Footer))
	return out.String()
}
