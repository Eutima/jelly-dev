package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-diff/internal/merge"
)

const (
	mergePanes   = 3
	paneOurs     = 0
	paneResult   = 1
	paneTheirs   = 2
	statusFormat = " %s"
)

// Choice is how a changed chunk is resolved in the result pane.
type Choice int

const (
	// ChoiceAuto takes the automatic merge result; unresolved conflicts keep the base lines.
	ChoiceAuto Choice = iota
	// ChoiceOurs takes the lines of the left side.
	ChoiceOurs
	// ChoiceTheirs takes the lines of the right side.
	ChoiceTheirs
	// ChoiceBoth takes the left lines followed by the right lines (once, when both sides are identical).
	ChoiceBoth
	// ChoiceBase discards both changes and keeps the base lines.
	ChoiceBase
)

// MergeOutcome reports what happened in the merge viewer after it quit.
type MergeOutcome struct {
	Saved bool
}

// MergeTitles are the headers of the three panes.
type MergeTitles struct {
	Ours, Result, Theirs string
}

type mergeRow struct {
	cells [mergePanes]Cell
	chunk int
	first bool // first row of its chunk
}

type historyEntry struct {
	chunk int
	prev  Choice
}

// MergeModel is the three-pane merge viewer: ours | result | theirs.
type MergeModel struct {
	titles  MergeTitles
	chunks  []merge.Chunk
	choices []Choice
	history []historyEntry
	changed []int // indexes of chunks that are not Unchanged
	theme   Theme
	save    func(lines []string) error
	outcome *MergeOutcome

	syntaxNames []string                   // file names the highlighting lexer is chosen from; empty means no highlighting
	paneSyntax  [mergePanes][][]SyntaxSpan // spans per pane and line; the result pane is recomputed on every change

	rows    []mergeRow
	starts  []int // first row of every changed chunk
	current int   // index into changed, -1 when none selected

	width, height int
	offset        int
	status        string
	dirty         bool // result differs from what was last saved
	quitArmed     bool
}

// NewMergeModel builds the viewer. save is called with the result lines when the user saves; the
// returned outcome tells the caller afterwards whether the result was saved.
func NewMergeModel(titles MergeTitles, chunks []merge.Chunk, save func([]string) error) (MergeModel, *MergeOutcome) {
	outcome := &MergeOutcome{}
	model := MergeModel{
		titles: titles, chunks: chunks, choices: make([]Choice, len(chunks)), theme: DarkTheme(),
		save: save, outcome: outcome, current: -1, dirty: true,
	}
	for i, chunk := range chunks {
		if chunk.Kind != merge.Unchanged {
			model.changed = append(model.changed, i)
		}
	}
	model.rebuild()
	return model, outcome
}

// ChangeCount returns the number of changed chunks (including conflicts).
func (m MergeModel) ChangeCount() int { return len(m.changed) }

// Unresolved returns the number of conflicts that still wait for a decision.
func (m MergeModel) Unresolved() int {
	count := 0
	for i, chunk := range m.chunks {
		if chunk.Kind == merge.Conflict && m.choices[i] == ChoiceAuto {
			count++
		}
	}
	return count
}

// Result returns the lines of the result pane.
func (m MergeModel) Result() []string {
	var lines []string
	for i := range m.chunks {
		lines = append(lines, m.chunkResult(i)...)
	}
	return lines
}

func (m MergeModel) chunkResult(i int) []string {
	chunk := m.chunks[i]
	switch m.choices[i] {
	case ChoiceOurs:
		return chunk.Ours
	case ChoiceTheirs:
		return chunk.Theirs
	case ChoiceBoth:
		if equalLines(chunk.Ours, chunk.Theirs) {
			return chunk.Ours
		}
		return append(append([]string(nil), chunk.Ours...), chunk.Theirs...)
	case ChoiceBase:
		return chunk.Base
	}
	if lines, ok := chunk.Resolved(); ok {
		return lines
	}
	return chunk.Base // unresolved conflict
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rebuild recomputes the aligned rows after the choices changed.
func (m *MergeModel) rebuild() {
	m.rows, m.starts = nil, nil // fresh slices: older copies of the model must not change
	if len(m.syntaxNames) > 0 {
		m.paneSyntax[paneResult] = highlightLines(m.syntaxNames, m.Result())
	}
	var numbers [mergePanes]int
	changedIndex := 0
	for i, chunk := range m.chunks {
		result := m.chunkResult(i)
		panes := [mergePanes][]string{chunk.Ours, result, chunk.Theirs}
		kinds := m.paneKinds(i, result)
		height := max(len(panes[paneOurs]), len(panes[paneResult]), len(panes[paneTheirs]))
		if chunk.Kind != merge.Unchanged && changedIndex < len(m.changed) && m.changed[changedIndex] == i {
			m.starts = append(m.starts, len(m.rows))
			changedIndex++
		}
		for row := 0; row < height; row++ {
			entry := mergeRow{chunk: i, first: row == 0}
			for pane := 0; pane < mergePanes; pane++ {
				if row >= len(panes[pane]) {
					entry.cells[pane] = Cell{Kind: CellFiller}
					continue
				}
				numbers[pane]++
				entry.cells[pane] = Cell{
					Kind: kinds[pane], No: numbers[pane], Text: panes[pane][row],
					Syntax: lineSpans(m.paneSyntax[pane], numbers[pane]),
				}
			}
			m.rows = append(m.rows, entry)
		}
	}
}

// paneKinds decides how the three panes of chunk i are colored.
func (m MergeModel) paneKinds(i int, result []string) [mergePanes]CellKind {
	chunk := m.chunks[i]
	if chunk.Kind == merge.Unchanged {
		return [mergePanes]CellKind{CellEqual, CellEqual, CellEqual}
	}
	side := func(lines []string) CellKind {
		switch {
		case equalLines(lines, chunk.Base):
			return CellEqual
		case chunk.Kind == merge.Conflict:
			return CellConflict
		default:
			return CellModified
		}
	}
	resultKind := CellModified
	switch {
	case chunk.Kind == merge.Conflict && m.choices[i] == ChoiceAuto:
		resultKind = CellConflict
	case equalLines(result, chunk.Base):
		resultKind = CellEqual
	}
	return [mergePanes]CellKind{side(chunk.Ours), resultKind, side(chunk.Theirs)}
}

// Init implements tea.Model.
func (m MergeModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m MergeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.offset -= wheelLines
		case tea.MouseButtonWheelDown:
			m.offset += wheelLines
		}
		m.clamp()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m MergeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key != "q" && key != "esc" {
		m.quitArmed = false
	}
	page := max(m.bodyHeight()-1, 1)
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		return m.quit()
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown", " ":
		m.offset += page
	case "pgup":
		m.offset -= page
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = len(m.rows)
	case "n", "tab":
		m.gotoChange(m.current + 1)
	case "p", "shift+tab":
		if m.current < 0 {
			m.gotoChange(len(m.changed) - 1)
		} else {
			m.gotoChange(m.current - 1)
		}
	case "o":
		m.resolve(ChoiceOurs)
	case "t":
		m.resolve(ChoiceTheirs)
	case "b":
		m.resolve(ChoiceBoth)
	case "r":
		m.resolve(ChoiceBase)
	case "u":
		m.undo()
	case "s":
		m.saveResult()
	}
	m.clamp()
	return m, nil
}

// quit leaves the viewer; unsaved work needs a second press.
func (m MergeModel) quit() (tea.Model, tea.Cmd) {
	if !m.dirty || m.quitArmed {
		return m, tea.Quit
	}
	m.quitArmed = true
	m.status = "Unsaved result - press q again to discard it, or s to save"
	return m, nil
}

func (m *MergeModel) gotoChange(index int) {
	count := len(m.changed)
	if count == 0 {
		return
	}
	m.current = (index%count + count) % count
	m.offset = max(m.starts[m.current]-m.bodyHeight()/3, 0)
}

// resolve applies a choice to the selected change and moves on to the next unresolved conflict.
func (m *MergeModel) resolve(choice Choice) {
	if m.current < 0 {
		m.status = "Select a change first (n/p)"
		return
	}
	chunk := m.changed[m.current]
	m.history = append(m.history[:len(m.history):len(m.history)], historyEntry{chunk: chunk, prev: m.choices[chunk]})
	m.choices = append([]Choice(nil), m.choices...)
	m.choices[chunk] = choice
	m.markDirty()
	m.status = ""
	m.rebuild()
	if next := m.nextUnresolved(); next >= 0 {
		m.gotoChange(next)
	} else {
		m.keepVisible()
	}
}

// nextUnresolved returns the index (into changed) of the next unresolved conflict after the current change.
func (m MergeModel) nextUnresolved() int {
	for step := 1; step <= len(m.changed); step++ {
		index := (m.current + step) % len(m.changed)
		chunk := m.changed[index]
		if m.chunks[chunk].Kind == merge.Conflict && m.choices[chunk] == ChoiceAuto {
			return index
		}
	}
	return -1
}

func (m *MergeModel) keepVisible() {
	if m.current >= 0 {
		start := m.starts[m.current]
		if start < m.offset || start >= m.offset+m.bodyHeight() {
			m.offset = max(start-m.bodyHeight()/3, 0)
		}
	}
}

func (m *MergeModel) undo() {
	if len(m.history) == 0 {
		m.status = "Nothing to undo"
		return
	}
	last := m.history[len(m.history)-1]
	m.history = m.history[: len(m.history)-1 : len(m.history)-1]
	m.choices = append([]Choice(nil), m.choices...)
	m.choices[last.chunk] = last.prev
	m.markDirty()
	m.status = ""
	m.rebuild()
	for index, chunk := range m.changed {
		if chunk == last.chunk {
			m.current = index
		}
	}
	m.keepVisible()
}

// markDirty records that the result changed after the last save; the outcome then counts as not saved.
func (m *MergeModel) markDirty() {
	m.dirty = true
	m.outcome.Saved = false
}

func (m *MergeModel) saveResult() {
	if left := m.Unresolved(); left > 0 {
		m.status = fmt.Sprintf("Cannot save: %d conflict(s) unresolved", left)
		return
	}
	if err := m.save(m.Result()); err != nil {
		m.status = "Save failed: " + err.Error()
		return
	}
	m.outcome.Saved = true
	m.dirty = false
	m.status = "Saved"
}

func (m MergeModel) bodyHeight() int { return max(m.height-chromeLines, 1) }

func (m *MergeModel) clamp() {
	m.offset = min(max(m.offset, 0), max(len(m.rows)-m.bodyHeight(), 0))
}

// View implements tea.Model.
func (m MergeModel) View() string {
	if m.width == 0 {
		return ""
	}
	paneWidth := max((m.width-2*gutterWidth)/mergePanes, 0)
	widths := [mergePanes]int{paneWidth, paneWidth, max(m.width-2*gutterWidth-2*paneWidth, 0)}
	numWidth := len(fmt.Sprint(max(len(m.rows), 1))) + 1

	var out strings.Builder
	out.WriteString(m.header(widths))
	out.WriteByte('\n')
	end := min(m.offset+m.bodyHeight(), len(m.rows))
	for i := m.offset; i < end; i++ {
		row := m.rows[i]
		out.WriteString(m.theme.renderCell(row.cells[paneOurs], numWidth, widths[paneOurs]))
		out.WriteString(m.gutter(row, paneOurs))
		out.WriteString(m.theme.renderCell(row.cells[paneResult], numWidth, widths[paneResult]))
		out.WriteString(m.gutter(row, paneTheirs))
		out.WriteString(m.theme.renderCell(row.cells[paneTheirs], numWidth, widths[paneTheirs]))
		out.WriteByte('\n')
	}
	for i := end - m.offset; i < m.bodyHeight(); i++ {
		out.WriteByte('\n')
	}
	out.WriteString(m.footer())
	return out.String()
}

func (m MergeModel) header(widths [mergePanes]int) string {
	titles := [mergePanes]string{m.titles.Ours, m.titles.Result, m.titles.Theirs}
	var out strings.Builder
	for pane, title := range titles {
		if pane > 0 {
			out.WriteString(strings.Repeat(" ", gutterWidth))
		}
		out.WriteString(padRight(" "+sanitize(title), widths[pane]))
	}
	return m.theme.Header.Render(out.String())
}

func (m MergeModel) footer() string {
	text := m.status
	if text == "" {
		state := "all conflicts resolved"
		if left := m.Unresolved(); left > 0 {
			state = fmt.Sprintf("%d conflict(s) left", left)
		}
		text = state + "   n/p change   o ours   t theirs   b both   r base   u undo   s save   q quit"
	}
	return m.theme.Footer.Render(padRight(fmt.Sprintf(statusFormat, text), m.width))
}

// gutter draws the strip between two panes: an arrow on the first row of a change that the
// neighbouring side contributes to, a bar on the other rows of a change.
func (m MergeModel) gutter(row mergeRow, side int) string {
	chunk := m.chunks[row.chunk]
	if chunk.Kind == merge.Unchanged {
		return m.theme.Gutter.Render(" │ ")
	}
	style := m.theme.Gutter
	if m.current >= 0 && m.changed[m.current] == row.chunk {
		style = m.theme.GutterCur
	}
	changedBySide := !equalLines(chunk.Ours, chunk.Base)
	arrow := " » "
	if side == paneTheirs {
		changedBySide = !equalLines(chunk.Theirs, chunk.Base)
		arrow = " « "
	}
	if row.first && changedBySide {
		return style.Render(arrow)
	}
	return style.Render(" ┃ ")
}

// WithHighlight returns the model with syntax highlighting; the lexer is chosen by the first file name
// that has one. Without a lexer, or for very large files, nothing is highlighted.
func (m MergeModel) WithHighlight(filenames ...string) MergeModel {
	var ours, theirs []string
	for _, chunk := range m.chunks {
		ours = append(ours, chunk.Ours...)
		theirs = append(theirs, chunk.Theirs...)
	}
	// The ours and theirs panes always show the complete Ours/Theirs lines of every chunk, so the line
	// numbers of these concatenations match the pane line numbers used in rebuild.
	m.syntaxNames = filenames
	m.paneSyntax[paneOurs] = highlightLines(filenames, ours)
	m.paneSyntax[paneTheirs] = highlightLines(filenames, theirs)
	m.rebuild()
	return m
}
