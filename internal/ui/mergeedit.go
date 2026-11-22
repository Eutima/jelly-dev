package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/merge"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// moveCursor moves the line cursor by delta rows and scrolls it into view.
func (m *MergeModel) moveCursor(delta int) { m.setCursor(m.cursor + delta) }

// setCursor puts the line cursor on a row, keeps it visible and selects the change it is in.
func (m *MergeModel) setCursor(row int) {
	m.cursor = min(max(row, 0), max(len(m.rows)-1, 0))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if !m.rowFullyVisible(m.cursor) {
		m.offset = m.firstRowFor(m.cursor)
	}
	m.syncCurrent()
}

// rebuildKeepingCursor rebuilds the rows after a change of the result and keeps the cursor on the line it was on
// (the same line of the same chunk, or the chunk's last line when the chunk became shorter). The scroll position
// stays unless the cursor would leave the screen.
func (m *MergeModel) rebuildKeepingCursor() {
	if len(m.rows) == 0 {
		m.rebuild()
		return
	}
	chunk := m.rows[m.cursor].chunk
	offset := m.cursor - m.chunkRow[chunk]
	m.rebuild()
	start, end := m.chunkRow[chunk], len(m.rows)-1
	if chunk+1 < len(m.chunkRow) {
		end = m.chunkRow[chunk+1] - 1
	}
	m.setCursor(min(start+offset, max(end, start)))
}

// syncCurrent selects the change that contains the cursor row (-1 when the cursor is on unchanged lines).
func (m *MergeModel) syncCurrent() {
	m.current = -1
	if len(m.rows) == 0 {
		return
	}
	chunk := m.rows[m.cursor].chunk
	for index, changed := range m.changed {
		if changed == chunk {
			m.current = index
		}
	}
}

// setChoice records how chunk is resolved; custom holds the hand-edited lines for ChoiceCustom. The step is
// pushed on the undo history. The caller rebuilds the rows.
func (m *MergeModel) setChoice(chunk int, choice Choice, custom []string) {
	previous, hadCustom := m.custom[chunk]
	m.history = append(m.history[:len(m.history):len(m.history)], historyEntry{
		chunk: chunk, prev: m.choices[chunk], prevCustom: previous, hadCustom: hadCustom,
	})
	m.choices = slices.Clone(m.choices)
	m.choices[chunk] = choice
	m.setCustom(chunk, custom, custom != nil)
	m.markDirty()
}

// autoResolve settles every unresolved conflict that is none in fact (whitespace-only or different words
// of the same lines, see merge.ResolveSimple) and leaves the real conflicts alone. One undo reverts it all.
func (m *MergeModel) autoResolve() {
	type step struct {
		chunk int
		lines []string
	}
	var steps []step
	for _, chunk := range m.changed {
		if m.chunks[chunk].Kind != merge.Conflict || m.choices[chunk] != ChoiceAuto {
			continue
		}
		if lines, ok := merge.ResolveSimple(m.chunks[chunk]); ok {
			steps = append(steps, step{chunk, slices.Clone(lines)})
		}
	}
	total := m.Unresolved()
	if len(steps) == 0 {
		m.notice(fmt.Sprintf("Nothing to resolve automatically: %d conflict(s) need a decision", total), true)
		return
	}
	for i, s := range steps {
		m.setChoice(s.chunk, ChoiceCustom, s.lines)
		m.history[len(m.history)-1].joined = i > 0
	}
	m.rebuildKeepingCursor()
	left := m.Unresolved()
	m.notice(fmt.Sprintf("✓ Auto-resolved %d of %d conflict(s), %d left (u undoes)", len(steps), total, left), false)
}

// resolveAll takes one side for every conflict that is still unresolved. Conflicts that were decided already and
// changes that merged automatically stay as they are. One undo reverts it all.
func (m *MergeModel) resolveAll(choice Choice) {
	var chunks []int
	for _, chunk := range m.changed {
		if m.chunks[chunk].Kind == merge.Conflict && m.choices[chunk] == ChoiceAuto {
			chunks = append(chunks, chunk)
		}
	}
	if len(chunks) == 0 {
		m.notice("No unresolved conflicts left", true)
		return
	}
	for i, chunk := range chunks {
		m.setChoice(chunk, choice, nil)
		m.history[len(m.history)-1].joined = i > 0
	}
	m.rebuildKeepingCursor()
	m.notice(fmt.Sprintf("✓ Used %s for %d conflict(s) in the whole file (u undoes)", choiceName(choice), len(chunks)), false)
}

// setCustom stores (or removes) the hand-edited lines of a chunk in a fresh map, so older copies of the
// model are not affected.
func (m *MergeModel) setCustom(chunk int, lines []string, present bool) {
	updated := make(map[int][]string, len(m.custom)+1)
	for key, value := range m.custom {
		updated[key] = value
	}
	if present {
		updated[chunk] = lines
	} else {
		delete(updated, chunk)
	}
	m.custom = updated
}

// resultLineAt returns the chunk and the index inside the chunk's result of the result line shown on row.
func (m MergeModel) resultLineAt(row int) (chunk, index int, ok bool) {
	if row < 0 || row >= len(m.rows) {
		return 0, 0, false
	}
	chunk = m.rows[row].chunk
	index = row - m.chunkRow[chunk]
	return chunk, index, m.rows[row].cells[paneResult].Kind != CellFiller
}

// deleteLine removes the result line under the cursor.
func (m *MergeModel) deleteLine() {
	chunk, index, ok := m.resultLineAt(m.cursor)
	if !ok {
		m.notice("No result line here to delete", true)
		return
	}
	lines := slices.Delete(slices.Clone(m.chunkResult(chunk)), index, index+1)
	m.setChoice(chunk, ChoiceCustom, nonNil(lines))
	m.notice("", false)
	m.rebuild()
	m.setCursor(m.cursor)
}

func nonNil(lines []string) []string {
	if lines == nil {
		return []string{}
	}
	return lines
}

// changeAfterCursor returns the index (into changed) of the first change that starts below the cursor row,
// wrapping to the first change; -1 when there are no changes.
func (m MergeModel) changeAfterCursor() int {
	for index, start := range m.starts {
		if start > m.cursor {
			return index
		}
	}
	if len(m.starts) > 0 {
		return 0
	}
	return -1
}

// changeBeforeCursor returns the index (into changed) of the last change that starts above the cursor row,
// wrapping to the last change; -1 when there are no changes.
func (m MergeModel) changeBeforeCursor() int {
	for index := len(m.starts) - 1; index >= 0; index-- {
		if m.starts[index] < m.cursor {
			return index
		}
	}
	return len(m.starts) - 1
}

// startEdit opens the vim-style editor on the result lines of the chunk under the result cursor, with the cursor on
// the line under it. All other result lines are shown above and below, read-only. With below set it adds an
// empty line under the cursor line and starts typing in it.
func (m *MergeModel) startEdit(below bool) {
	if len(m.rows) == 0 {
		m.notice("Nothing to edit", true)
		return
	}
	chunk := m.rows[m.cursor].chunk
	var before, after []string
	for i := 0; i < len(m.chunks); i++ {
		switch lines := m.chunkResult(i); {
		case i < chunk:
			before = append(before, lines...)
		case i > chunk:
			after = append(after, lines...)
		}
	}
	m.editFirst = len(before) + 1
	m.editBefore, m.editAfter = before, after

	lines := m.chunkResult(chunk)
	buffer := vimbuf.New(lines, make([]int, len(lines)))
	buffer.SetPage(m.bodyHeight())
	if _, index, ok := m.resultLineAt(m.cursor); ok && len(lines) > 0 {
		buffer.GotoLine(index + 1)
	}
	switch {
	case !below:
	case len(lines) == 0:
		buffer.Handle("i")
	default:
		buffer.OpenLineBelow()
	}
	m.editing, m.vim, m.editChunk, m.editTop, m.editCtx, m.editGPending = true, buffer, chunk, 0, -1, false
	m.keepEditorVisible()
	m.notice("", false)
}

// editorKeys turns a key message into the key names the editor understands.
func editorKeys(msg tea.KeyMsg) []string {
	switch msg.Type {
	case tea.KeyRunes:
		if msg.Alt {
			return nil
		}
		keys := make([]string, 0, len(msg.Runes))
		for _, r := range msg.Runes {
			keys = append(keys, string(r))
		}
		return keys
	case tea.KeySpace:
		return []string{" "}
	}
	return []string{msg.String()}
}

// handleEditKey feeds a key press to the editor and carries out what it asks for (:w, :wq, :q, :q!).
func (m MergeModel) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notice("", false)
	if m.editConfirm {
		return m.answerEditPrompt(msg)
	}
	if msg.String() == "ctrl+d" { // like q in the merge view: leave, but ask first when there are unapplied edits
		if !m.vim.Modified() {
			m.closeEditor()
			m.clamp()
			return m, nil
		}
		m.editConfirm = true
		return m, nil
	}
	if m.editNavigate(msg.String()) {
		m.keepEditorVisible()
		return m, nil
	}
	for _, key := range editorKeys(msg) {
		switch m.vim.Handle(key) {
		case vimbuf.Write:
			m.applyEdit()
		case vimbuf.WriteQuit:
			m.applyEdit()
			m.closeEditor()
		case vimbuf.Quit, vimbuf.ForceQuit:
			m.closeEditor()
		}
		if !m.editing {
			break
		}
	}
	if m.editing {
		if text := m.vim.Message(); text != "" {
			m.notice(text, true)
		}
		m.keepEditorVisible()
	}
	m.clamp()
	return m, nil
}

// editTotal is the number of lines of the editor view: read-only context, edited chunk, read-only context.
func (m MergeModel) editTotal() int { return len(m.editBefore) + m.vim.LineCount() + len(m.editAfter) }

// editViewRow is the index of the cursor line in the editor view.
func (m MergeModel) editViewRow() int {
	if m.editCtx >= 0 {
		return m.editCtx
	}
	row, _ := m.vim.Cursor()
	return len(m.editBefore) + row
}

// moveEditCursor puts the cursor on a line of the editor view: a read-only context line, or a line of the edited
// chunk (which hands the cursor back to the editor).
func (m *MergeModel) moveEditCursor(target int) {
	target = min(max(target, 0), m.editTotal()-1)
	before, count := len(m.editBefore), m.vim.LineCount()
	if target >= before && target < before+count {
		m.editCtx = -1
		m.vim.GotoLine(target - before + 1)
		return
	}
	m.editCtx = target
}

// editNavigate lets the cursor leave the edited chunk and walk over the read-only lines (which cannot be changed).
// It handles only plain navigation keys in normal mode; everything else goes to the editor. It reports whether the
// key was used.
func (m *MergeModel) editNavigate(key string) bool {
	if m.vim.Mode() == vimbuf.Command {
		return false // typing a command: every key belongs to the command line
	}
	page := max(m.bodyHeight()-1, 1)
	step := 0
	switch key {
	case "j", "down", "ctrl+n":
		step = 1
	case "k", "up", "ctrl+p":
		step = -1
	case "pgdown", "ctrl+f":
		step = page
	case "pgup", "ctrl+b":
		step = -page
	}
	onContext := m.editCtx >= 0
	if !onContext {
		if m.vim.Mode() != vimbuf.Normal || m.vim.Pending() != "" {
			return false
		}
		row, _ := m.vim.Cursor()
		last := m.vim.LineCount() - 1
		// only the moves that would run into the edge of the chunk continue over the context lines
		switch {
		case step > 0 && row+step > last && len(m.editAfter) > 0 && (row == last || step > 1):
		case step < 0 && row+step < 0 && len(m.editBefore) > 0 && (row == 0 || step < -1):
		case key == "G" && row == last && len(m.editAfter) > 0:
			m.moveEditCursor(m.editTotal() - 1)
			return true
		case key == "g" && row == 0 && len(m.editBefore) > 0 && m.editGPending:
			m.editGPending = false
			m.vim.Handle("esc")
			m.moveEditCursor(0)
			return true
		default:
			m.editGPending = key == "g" && row == 0
			return false
		}
		m.moveEditCursor(m.editViewRow() + step)
		return true
	}
	switch key {
	case ":":
		return false // command line: handled by the editor, it needs no cursor in the chunk
	case "G":
		m.moveEditCursor(m.editTotal() - 1)
	case "g":
		if m.editGPending {
			m.editGPending = false
			m.moveEditCursor(0)
		} else {
			m.editGPending = true
		}
	default:
		if step != 0 {
			m.moveEditCursor(m.editCtx + step)
		} else if len(key) == 1 || key == "enter" || key == "backspace" || key == "delete" {
			m.notice("Read-only line - move into the change to edit it", true)
		}
	}
	if key != "g" {
		m.editGPending = false
	}
	return true
}

// answerEditPrompt handles the answer to "apply the edits?": y applies and leaves, n discards and leaves, anything
// else keeps editing.
func (m MergeModel) answerEditPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.editConfirm = false
	switch msg.String() {
	case "y", "Y", "enter":
		m.applyEdit()
		m.closeEditor()
	case "n", "N", "ctrl+d":
		m.closeEditor()
	}
	m.clamp()
	return m, nil
}

func (m *MergeModel) closeEditor() {
	m.editing, m.vim, m.editConfirm = false, nil, false
	m.setCursor(m.cursor)
}

// applyEdit stores the edited lines in the chunk; it becomes a hand-edited one when its lines changed.
func (m *MergeModel) applyEdit() {
	edited := m.vim.Lines()
	m.vim.MarkWritten()
	if equalLines(edited, m.chunkResult(m.editChunk)) {
		m.notice("No changes to apply", false)
		return
	}
	m.setChoice(m.editChunk, ChoiceCustom, nonNil(edited))
	m.rebuild()
	m.notice(fmt.Sprintf("✓ Applied the edit · %d conflict(s) left (u undoes)", m.Unresolved()), false)
}

// keepEditorVisible scrolls the editor view so that its cursor line is shown.
func (m *MergeModel) keepEditorVisible() {
	row := m.editViewRow()
	body := m.bodyHeight()
	if row < m.editTop {
		m.editTop = row
	}
	if row >= m.editTop+body {
		m.editTop = row - body + 1
	}
	total := len(m.editBefore) + m.vim.LineCount() + len(m.editAfter)
	m.editTop = max(min(m.editTop, total-1), 0)
}

// editingStatus is the footer text while the editor is open: the command line, or the mode and position.
func (m MergeModel) editingStatus() string {
	if m.editConfirm {
		return "Apply the changes before leaving?  y apply and leave · n discard and leave · any other key keeps editing"
	}
	if m.vim.Mode() == vimbuf.Command {
		return ":" + m.vim.CommandLine()
	}
	if m.editCtx >= 0 {
		return fmt.Sprintf("-- READ-ONLY --   %d   (move into the change to edit · :q leaves · ctrl+d leaves)", m.editFirst-len(m.editBefore)+m.editCtx)
	}
	row, col := m.vim.Cursor()
	pending := m.vim.Pending()
	if pending != "" {
		pending = "   " + pending
	}
	modified := ""
	if m.vim.Modified() {
		modified = "  [+]"
	}
	return fmt.Sprintf("%s%s%s   %d:%d   (dimmed lines are read-only)", m.vim.Mode(), modified, pending, m.editFirst+row, col+1)
}

// editorBody draws the editor view (full width) below the header: the read-only lines above, the edited chunk, the
// read-only lines below. Context lines are dimmed.
func (m MergeModel) editorBody(width int) string {
	before, count := len(m.editBefore), m.vim.LineCount()
	total := before + count + len(m.editAfter)
	row, col := m.vim.Cursor()
	lastNo := m.editFirst - before + total - 1
	numWidth := len(fmt.Sprint(lastNo)) + 1
	textWidth := max(width-numWidth, 0)
	styles := editLineStyles{base: m.theme.Equal, selected: m.theme.Selected, cursor: m.theme.CursorStyle()}
	context := editLineStyles{base: m.theme.Equal.Faint(true), selected: m.theme.Equal.Faint(true), cursor: m.theme.Equal.Faint(true)}
	var out strings.Builder
	for i := m.editTop; i < m.editTop+m.bodyHeight(); i++ {
		out.WriteString(m.theme.Gutter.Render(strings.Repeat(" ", min(markerWidth, m.width))))
		if i >= total {
			out.WriteString(m.theme.Equal.Render(strings.Repeat(" ", width)))
			out.WriteByte('\n')
			continue
		}
		label := fmt.Sprintf("%*d ", numWidth-1, m.editFirst-before+i)
		tw := textWidth
		if width < numWidth {
			label, tw = "", width
		}
		var line string
		var style editLineStyles
		cursor, from, to := -1, 0, 0
		switch {
		case i < before:
			line, style = m.editBefore[i], context
			if i == m.editCtx {
				style = editLineStyles{base: m.theme.Selected, selected: m.theme.Selected, cursor: m.theme.Selected}
			}
		case i >= before+count:
			line, style = m.editAfter[i-before-count], context
			if i == m.editCtx {
				style = editLineStyles{base: m.theme.Selected, selected: m.theme.Selected, cursor: m.theme.Selected}
			}
		default:
			index := i - before
			line, style = m.vim.Line(index), styles
			if index == row && m.editCtx < 0 {
				cursor = col
			}
			if f, t, ok := m.vim.Selected(index); ok {
				from, to = f, t
			}
		}
		out.WriteString(m.theme.LineNumber.Render(label))
		out.WriteString(renderEditLine([]rune(line), tw, cursor, from, to, style))
		out.WriteByte('\n')
	}
	return out.String()
}

// editorBar builds the hint bar under the editor.
func (m MergeModel) editorBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range [][2]string{{"esc", "normal"}, {"i", "insert"}, {"v/V", "visual"}, {":w", "apply"}, {":wq", "apply+leave"},
		{":q", "leave"}, {":q!", "discard"}, {"ctrl+d", "leave (asks)"}} {
		result.command(h[0], h[1], !m.editConfirm, m.theme)
	}
	return result
}
