package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/ai"
	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/merge"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

const (
	mergePanes = 3
	paneOurs   = 0
	paneResult = 1
	paneTheirs = 2
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
	// ChoiceCustom uses lines the user edited by hand.
	ChoiceCustom
)

// MergeOutcome reports what happened in the merge viewer after it quit.
type MergeOutcome struct {
	Saved bool
}

// MergeTitles are the labels of the merge view: Name identifies the document, the other three are the file
// names shown in the pane headers.
type MergeTitles struct {
	Name                 string
	Ours, Result, Theirs string
}

type mergeRow struct {
	cells [mergePanes]Cell
	chunk int
	first bool // first row of its chunk
}

type historyEntry struct {
	chunk      int
	prev       Choice
	prevCustom []string // the hand-edited lines before the step; only meaningful when hadCustom
	hadCustom  bool
	joined     bool // undone together with the entry before it (one magic step covers several chunks)
}

// MergeModel is the three-pane merge viewer: ours | result | theirs.
type MergeModel struct {
	titles  MergeTitles
	chunks  []merge.Chunk
	choices []Choice
	custom  map[int][]string // hand-edited result lines per chunk, used when the choice is ChoiceCustom
	history []historyEntry
	changed []int // indexes of chunks that are not Unchanged
	theme   Theme
	save    func(lines []string) error
	outcome *MergeOutcome

	assistants []ai.Assistant     // installed assistant CLIs that can resolve conflicts
	aiBusy     bool               // a request to an assistant is running
	aiCancel   context.CancelFunc // stops the running request

	syntaxNames []string                   // file names the highlighting lexer is chosen from; empty means no highlighting
	paneSyntax  [mergePanes][][]SyntaxSpan // spans per pane and line; the result pane is recomputed on every change

	rows     []mergeRow
	starts   []int // first row of every changed chunk
	chunkRow []int // first row of every chunk (a chunk without lines has none: its entry is the next row)
	current  int   // index into changed, -1 when none selected
	cursor   int   // row of the line cursor in the result pane

	noWrap       bool           // long lines are cut with "…" instead of wrapped (toggled with w)
	editing      bool           // the vim-style editor is open on one chunk of the result
	vim          *vimbuf.Buffer // the editor's text and state while editing
	editConfirm  bool           // ctrl+d asked whether to apply the unapplied edits
	editChunk    int            // the chunk being edited
	editBefore   []string       // read-only result lines above the chunk
	editAfter    []string       // read-only result lines below the chunk
	editFirst    int            // result line number of the first edited line
	editCtx      int            // cursor line (index into the context + edited + context view) while it is on a read-only line, else -1
	editGPending bool           // a g was typed on a read-only line (gg goes to the top)
	editTop      int            // first shown line of the editor view (context lines included)

	width, height int
	offset        int
	status        string
	warn          bool // the status line is a warning or an error
	viewPane      int  // pane shown when the terminal is too narrow for all three
	saveFailed    bool // the last save attempt failed and nothing changed since
	dirty         bool // result differs from what was last saved
	quitArmed     bool
	embedded      bool          // shown inside another model: quitting sets done instead of ending the program
	done          bool          // an embedded viewer was left
	palette       *mergePalette // the list of all actions, nil when closed
}

// NewMergeModel builds the viewer. save is called with the result lines when the user saves; the
// returned outcome tells the caller afterwards whether the result was saved.
func NewMergeModel(titles MergeTitles, chunks []merge.Chunk, save func([]string) error) (MergeModel, *MergeOutcome) {
	outcome := &MergeOutcome{}
	model := MergeModel{
		titles: titles, chunks: chunks, choices: make([]Choice, len(chunks)), theme: DarkTheme(),
		save: save, outcome: outcome, current: -1, dirty: true, viewPane: paneResult,
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
	case ChoiceCustom:
		return m.custom[i]
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
	m.rows, m.starts, m.chunkRow = nil, nil, make([]int, len(m.chunks)) // fresh slices: older copies must not change
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
		m.chunkRow[i] = len(m.rows)
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
	m.cursor = min(max(m.cursor, 0), max(len(m.rows)-1, 0))
}

// paneKinds decides how the three panes of chunk i are colored.
func (m MergeModel) paneKinds(i int, result []string) [mergePanes]CellKind {
	chunk := m.chunks[i]
	if chunk.Kind == merge.Unchanged && m.choices[i] != ChoiceCustom {
		return [mergePanes]CellKind{CellEqual, CellEqual, CellEqual}
	}
	// An unresolved conflict is colored as one on every side; afterwards each side shows who changed it.
	unresolved := chunk.Kind == merge.Conflict && m.choices[i] == ChoiceAuto
	side := func(lines []string, changed CellKind) CellKind {
		switch {
		case equalLines(lines, chunk.Base):
			return CellEqual
		case unresolved:
			return CellConflict
		default:
			return changed
		}
	}
	resultKind := CellModified
	switch {
	case chunk.Kind == merge.Conflict && m.choices[i] == ChoiceAuto:
		resultKind = CellConflict
	case equalLines(result, chunk.Base):
		resultKind = CellEqual
	}
	return [mergePanes]CellKind{side(chunk.Ours, CellOurs), resultKind, side(chunk.Theirs, CellTheirs)}
}

// Init implements tea.Model.
func (m MergeModel) Init() tea.Cmd { return nil }

// Update implements tea.Model. When a message resolves the last open conflict, the status line says so.
func (m MergeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.Unresolved()
	updated, cmd := m.update(msg)
	if result, ok := updated.(MergeModel); ok && before > 0 && result.Unresolved() == 0 && !result.warn && !result.editing {
		result.announceAllResolved()
		return result, cmd
	}
	return updated, cmd
}

// allResolvedNotice is shown when the last open conflict was resolved.
func allResolvedNotice() string {
	return "✓ All conflicts resolved - press " + keyLabel(keymap.MergeSave) + " to save"
}

// announceAllResolved adds the completion message to the status line, after the message of the action that
// resolved the last conflict (unless that message says it already).
func (m *MergeModel) announceAllResolved() {
	switch {
	case strings.Contains(strings.ToLower(m.status), "all conflicts resolved"):
	case m.status == "":
		m.notice(allResolvedNotice(), false)
	default:
		m.notice(m.status+" · "+allResolvedNotice(), false)
	}
}

func (m MergeModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case tea.MouseMsg:
		if m.palette != nil {
			return m.updatePalette(msg)
		}
		if leftPress(msg) {
			if keys := m.footerKeys(msg.X, msg.Y); keys != nil {
				return replay(m, keys)
			}
			m.click(msg.X, msg.Y)
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.offset -= wheelLines
		case tea.MouseButtonWheelDown:
			m.offset += wheelLines
		}
		m.clamp()
	case aiDoneMsg:
		m = m.applyAssistant(msg)
		m.clamp()
	case tea.KeyMsg:
		if m.palette != nil {
			return m.updatePalette(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// footerKeys returns the keys of the footer command at the screen position, nil when there is none. The compact
// one-line footer of small terminals is not clickable.
func (m MergeModel) footerKeys(x, y int) []string {
	if m.height < fullChromeHeight {
		return nil
	}
	return rowKeysAt(m.footerBars(), x, y, m.width, m.height)
}

func (m MergeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing {
		return m.handleEditKey(msg)
	}
	key := msg.String()
	m.notice("", false) // a message lives until the next key press
	command, bound := keys.Lookup(keymap.ScopeMerge, key)
	if command != keymap.MergeQuit {
		m.quitArmed = false
	}
	if command == keymap.MergePalette {
		return m.openPalette()
	}
	if m.aiBusy {
		m.notice("Waiting for the assistant... ("+keyLabel(keymap.MergeQuit)+" quits)", false)
	}
	if bound {
		return m.runCommand(command)
	}
	if key == "ctrl+c" { // the way out stays, whatever merge.quit is bound to
		return m.quit()
	}
	page := max(m.bodyHeight()-1, 1)
	switch key { // the keys that are not commands: moving through the rows
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "pgdown", " ":
		m.moveCursor(page)
	case "pgup":
		m.moveCursor(-page)
	case "g", "home":
		m.setCursor(0)
	case "G", "end":
		m.setCursor(len(m.rows) - 1)
	}
	m.clamp()
	return m, nil
}

// runCommand does what a command of the merge view says: a key of it was pressed, or it was chosen in the command palette.
func (m MergeModel) runCommand(command keymap.Command) (tea.Model, tea.Cmd) {
	switch command {
	case keymap.MergeQuit:
		return m.quit()
	case keymap.MergeClaude, keymap.MergeCodex:
		if assistant, ok := m.assistantFor(command); ok {
			return m.askAssistant(assistant)
		}
	case keymap.MergeNext:
		m.gotoChange(m.changeAfterCursor())
	case keymap.MergePrev:
		m.gotoChange(m.changeBeforeCursor())
	case keymap.MergeAllOurs:
		m.resolveAll(ChoiceOurs)
	case keymap.MergeAllTheirs:
		m.resolveAll(ChoiceTheirs)
	case keymap.MergePickOurs:
		m.resolve(ChoiceOurs)
	case keymap.MergePickTheirs:
		m.resolve(ChoiceTheirs)
	case keymap.MergeOurs:
		m.toggleSide(paneOurs)
	case keymap.MergeTheirs:
		m.toggleSide(paneTheirs)
	case keymap.MergeCancel:
		m.cancelChange()
	case keymap.MergeIgnore:
		m.ignoreChange()
	case keymap.MergeBoth:
		m.resolve(ChoiceBoth)
	case keymap.MergeBase:
		m.resolve(ChoiceBase)
	case keymap.MergeAuto:
		m.autoResolve()
	case keymap.MergeEdit:
		m.startEdit(false)
	case keymap.MergeDelete:
		m.deleteLine()
	case keymap.MergeInsert:
		m.startEdit(true)
	case keymap.MergeUndo:
		m.undo()
	case keymap.MergePane:
		m.cycleView()
	case keymap.MergeWrap:
		m.noWrap = !m.noWrap
		m.offset = m.firstRowFor(m.offset)
		wrap := keyLabel(keymap.MergeWrap)
		m.notice(map[bool]string{true: "Long lines are cut (" + wrap + " wraps them)", false: "Long lines are wrapped (" + wrap + " cuts them)"}[m.noWrap], false)
	case keymap.MergeSave:
		m.saveResult()
	}
	m.clamp()
	return m, nil
}

// quit leaves the viewer; unsaved work needs a second press.
func (m MergeModel) quit() (tea.Model, tea.Cmd) {
	if (!m.dirty || m.quitArmed) && m.aiCancel != nil {
		m.aiCancel() // do not leave an assistant process running
	}
	if !m.dirty || m.quitArmed {
		if m.embedded {
			m.done = true
			return m, nil
		}
		return m, tea.Quit
	}
	m.quitArmed = true
	m.notice("Unsaved result - press "+quitKeys()+" to discard it, or "+keyLabel(keymap.MergeSave)+" to save", true)
	return m, nil
}

func (m *MergeModel) gotoChange(index int) {
	count := len(m.changed)
	if count == 0 {
		return
	}
	m.current = (index%count + count) % count
	m.offset = max(m.starts[m.current]-m.bodyHeight()/3, 0)
	m.cursor = m.starts[m.current]
}

// resolve applies a choice to the selected change and moves on to the next unresolved conflict.
func (m *MergeModel) resolve(choice Choice) {
	if m.current < 0 {
		m.notice("Select a change first (n/p)", true)
		return
	}
	m.applyChoice(choice, true)
}

// applyChoice sets the choice of the selected change; advance moves on to the next unresolved conflict.
func (m *MergeModel) applyChoice(choice Choice, advance bool) {
	chunk := m.changed[m.current]
	m.setChoice(chunk, choice, nil)
	m.rebuild()
	m.notice(m.resolvedNotice(chunk, choice), false)
	if next := m.nextUnresolved(); advance && next >= 0 {
		m.gotoChange(next)
	} else {
		m.keepVisible()
	}
}

// resolvedNotice reports what a resolution did, including what is left to do.
func (m MergeModel) resolvedNotice(chunk int, choice Choice) string {
	what := "Change"
	if number := m.conflictNumber(chunk); number > 0 {
		what = fmt.Sprintf("Conflict %d", number)
	}
	text := fmt.Sprintf("✓ %s: used %s", what, choiceName(choice))
	if left := m.Unresolved(); left > 0 {
		return fmt.Sprintf("%s · %d conflict(s) left", text, left)
	}
	return text + " · all conflicts resolved, press " + keyLabel(keymap.MergeSave) + " to save"
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
		if !m.rowFullyVisible(start) {
			m.offset = max(start-m.bodyHeight()/3, 0)
		}
	}
}

func (m *MergeModel) undo() {
	if len(m.history) == 0 {
		m.notice("Nothing to undo", true)
		return
	}
	var last historyEntry
	m.choices = slices.Clone(m.choices)
	for {
		last = m.history[len(m.history)-1]
		m.history = m.history[: len(m.history)-1 : len(m.history)-1]
		m.choices[last.chunk] = last.prev
		m.setCustom(last.chunk, last.prevCustom, last.hadCustom)
		if !last.joined {
			break
		}
	}
	m.markDirty()
	m.rebuild()
	m.setCursor(m.chunkRow[last.chunk])
	m.keepVisible()
	m.notice(m.undoneNotice(last.chunk), false)
}

// undoneNotice reports the state of the chunk an undo step touched.
func (m MergeModel) undoneNotice(chunk int) string {
	if number := m.conflictNumber(chunk); number > 0 && m.stateOf(chunk) == stateUnresolved {
		return fmt.Sprintf("Undone · conflict %d is unresolved again", number)
	}
	return "Undone · " + fmt.Sprintf("%d conflict(s) left", m.Unresolved())
}

// markDirty records that the result changed after the last save; the outcome then counts as not saved.
func (m *MergeModel) markDirty() {
	m.dirty = true
	m.saveFailed = false
	m.outcome.Saved = false
}

func (m *MergeModel) saveResult() {
	if left := m.Unresolved(); left > 0 {
		m.notice(fmt.Sprintf("Cannot save: %d conflict(s) unresolved (n jumps to the next one)", left), true)
		return
	}
	if err := m.save(m.Result()); err != nil {
		m.saveFailed = true
		m.notice("Save failed: "+err.Error()+" - fix the problem and press "+keyLabel(keymap.MergeSave)+" to retry", true)
		return
	}
	m.outcome.Saved = true
	m.dirty = false
	m.saveFailed = false
	m.notice("✓ Saved - the result was written", false)
}

func (m MergeModel) bodyHeight() int { return max(m.height-m.chrome(), 1) }

func (m *MergeModel) clamp() {
	m.offset = min(max(m.offset, 0), m.lastOffset())
}

// resultCell draws the result pane cell of row i: the cursor line or a normal cell.
func (m MergeModel) resultCell(i int, row mergeRow, numWidth, width int) string {
	cell := row.cells[paneResult]
	if i == m.cursor && cell.Kind != CellFiller {
		cell.Kind = CellCursor
	}
	return m.theme.renderCell(cell, numWidth, width)
}

// gutter draws the strip between two panes: an arrow on the first row of a change that the
// neighbouring side contributes to, a bar on the other rows of a change.
func (m MergeModel) gutter(row mergeRow, side int, continuation bool) string {
	if m.chunks[row.chunk].Kind == merge.Unchanged {
		return m.theme.Gutter.Render("  │  ")
	}
	style := m.theme.Gutter
	if m.current >= 0 && m.changed[m.current] == row.chunk {
		style = m.theme.GutterCur
	}
	apply, discard := m.buttonShown(row, side, true), m.buttonShown(row, side, false)
	if continuation || !apply && !discard {
		return style.Render("  ┃  ")
	}
	glyph := func(shown bool, symbol string) string {
		if shown {
			return symbol
		}
		return " "
	}
	if side == paneTheirs {
		return style.Render(" " + glyph(apply, "«") + " " + glyph(discard, "×") + " ")
	}
	return style.Render(" " + glyph(discard, "×") + " " + glyph(apply, "»") + " ")
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

// arrowShown tells whether the gutter of a row draws the » (side ours) or « (side theirs) arrow: on the first row
// of a change that the neighbouring side contributes to. The arrow is also the click target that applies it.
func (m MergeModel) arrowShown(row mergeRow, side int) bool {
	chunk := m.chunks[row.chunk]
	if chunk.Kind == merge.Unchanged || !row.first {
		return false
	}
	if side == paneTheirs {
		return !equalLines(chunk.Theirs, chunk.Base)
	}
	return !equalLines(chunk.Ours, chunk.Base)
}

// buttonShown tells whether the apply (» or «) or the discard (×) button of a side is drawn and clickable on a
// row: only on the first row of a change that the side contributes to, and only while the click would change the
// result (a button disappears once its action has been done).
func (m MergeModel) buttonShown(row mergeRow, side int, apply bool) bool {
	return m.arrowShown(row, side) && m.sideChoice(row.chunk, side, apply) != m.choices[row.chunk]
}

// WithEmbedded returns the viewer for use inside another model: leaving it (after the unsaved-work check) does
// not end the program but makes Done report true.
func (m MergeModel) WithEmbedded() MergeModel {
	m.embedded = true
	return m
}

// Done reports whether an embedded viewer was left.
func (m MergeModel) Done() bool { return m.done }
