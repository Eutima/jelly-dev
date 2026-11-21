package ui

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-diff/internal/merge"
)

// moveCursor moves the line cursor by delta rows and scrolls it into view.
func (m *MergeModel) moveCursor(delta int) { m.setCursor(m.cursor + delta) }

// setCursor puts the line cursor on a row, keeps it visible and selects the change it is in.
func (m *MergeModel) setCursor(row int) {
	m.cursor = min(max(row, 0), max(len(m.rows)-1, 0))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.bodyHeight() {
		m.offset = m.cursor - m.bodyHeight() + 1
	}
	m.syncCurrent()
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
	m.rebuild()
	left := m.Unresolved()
	m.notice(fmt.Sprintf("✓ Auto-resolved %d of %d conflict(s), %d left (u undoes)", len(steps), total, left), false)
	if next := m.nextUnresolved(); next >= 0 {
		m.gotoChange(next)
	} else {
		m.keepVisible()
	}
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
	m.rebuild()
	m.notice(fmt.Sprintf("✓ Used %s for %d conflict(s) in the whole file (u undoes)", choiceName(choice), len(chunks)), false)
	m.keepVisible()
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

// startEdit opens the editor on the result line under the cursor.
func (m *MergeModel) startEdit() {
	chunk, index, ok := m.resultLineAt(m.cursor)
	if !ok {
		m.notice("No result line here to edit (use i to insert one)", true)
		return
	}
	m.editing, m.editNew = true, false
	m.editRow, m.editChunk, m.editLine = m.cursor, chunk, index
	m.editor = newLineEditor(m.rows[m.cursor].cells[paneResult].Text)
	m.notice("", false)
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

// insertLine adds an empty result line below the cursor and opens the editor on it.
func (m *MergeModel) insertLine() {
	if len(m.rows) == 0 {
		m.notice("Nothing to insert into", true)
		return
	}
	chunk := m.rows[m.cursor].chunk
	result := m.chunkResult(chunk)
	index := len(result)
	if _, _, ok := m.resultLineAt(m.cursor); ok {
		index = m.cursor - m.chunkRow[chunk] + 1
	}
	m.editBeforeRow = m.cursor
	m.setChoice(chunk, ChoiceCustom, slices.Insert(slices.Clone(result), index, ""))
	m.rebuild()
	m.setCursor(m.chunkRow[chunk] + index)
	m.editing, m.editNew = true, true
	m.editRow, m.editChunk, m.editLine = m.cursor, chunk, index
	m.editor = newLineEditor("")
	m.notice("", false)
}

// handleEditKey routes a key press to the line editor and applies or discards the edit.
func (m MergeModel) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	editor, action := m.editor.handle(msg)
	m.editor = editor
	switch action {
	case editApply:
		m.commitEdit()
	case editCancel:
		m.cancelEdit()
	}
	m.clamp()
	return m, nil
}

// commitEdit stores the edited line. A freshly inserted line is already on the undo history, so the edit
// completes that step instead of adding another one.
func (m *MergeModel) commitEdit() {
	lines := slices.Clone(m.chunkResult(m.editChunk))
	if m.editLine >= len(lines) {
		m.editing, m.editNew = false, false
		return
	}
	lines[m.editLine] = m.editor.String()
	if m.editNew {
		m.setCustom(m.editChunk, lines, true)
		m.markDirty()
	} else if lines[m.editLine] != m.chunkResult(m.editChunk)[m.editLine] {
		m.setChoice(m.editChunk, ChoiceCustom, lines)
	}
	m.editing, m.editNew = false, false
	m.notice("", false)
	m.rebuild()
	m.setCursor(m.cursor)
}

// cancelEdit leaves the editor; an inserted line that was never filled in is removed again.
func (m *MergeModel) cancelEdit() {
	wasNew := m.editNew
	m.editing, m.editNew = false, false
	if wasNew {
		m.undo()
		m.notice("", false)
		m.setCursor(m.editBeforeRow) // undo moves the cursor to the chunk start: go back to where the insert began
	}
}

func nonNil(lines []string) []string {
	if lines == nil {
		return []string{}
	}
	return lines
}

// editingStatus is the footer text while a line is being edited.
func (m MergeModel) editingStatus() string {
	return fmt.Sprintf("Editing line %d · enter apply   esc cancel   ←/→ home end move   ctrl+u/ctrl+k delete to start/end",
		m.rows[m.editRow].cells[paneResult].No)
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
