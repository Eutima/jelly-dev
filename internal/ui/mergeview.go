package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/merge"
)

const (
	mergeGutterWidth   = 5  // between two panes: room for an apply arrow and a discard cross
	markerWidth        = 2  // edge column: current-change bar and state symbol
	fullChromeHeight   = 10 // from this terminal height on the header has three lines and the footer two
	fullHeaderLines    = 3
	discardCells       = 2   // cells of a gutter that belong to the discard cross
	compactChromeLines = 2   // pane labels + one status line
	threePaneWidth     = 72  // narrower terminals show one pane at a time
	resultPercent      = 40  // the result pane gets this share of the pane width, ours and theirs split the rest
	progressBarWidth   = 10  // cells of the resolved-conflicts bar
	minBarWidth        = 96  // narrower terminals leave out the progress bar
	percentFull        = 100 // used for the progress ratio
)

// Symbols of the chunk state; every one of them is also distinguishable from the others without color.
const (
	symbolCurrent    = "▌"
	symbolUnresolved = "!"
	symbolResolved   = "✓"
	symbolAuto       = "~"
	symbolContinue   = "│"
)

// chunkState tells how far a changed chunk is settled.
type chunkState int

const (
	stateUntouched  chunkState = iota // unchanged lines
	stateAuto                         // changed by one side only, merged automatically
	stateUnresolved                   // conflict that still waits for a decision
	stateResolved                     // the user decided (a conflict, or an automatic change that was overridden)
)

func (m MergeModel) stateOf(chunk int) chunkState {
	switch {
	case m.chunks[chunk].Kind == merge.Conflict && m.choices[chunk] == ChoiceAuto:
		return stateUnresolved
	case m.chunks[chunk].Kind == merge.Unchanged && m.choices[chunk] == ChoiceAuto:
		return stateUntouched
	case m.choices[chunk] == ChoiceAuto:
		return stateAuto
	default:
		return stateResolved
	}
}

// chrome returns the number of lines taken by header and footer.
func (m MergeModel) chrome() int {
	if m.height >= fullChromeHeight {
		return fullHeaderLines + len(m.footerBars())
	}
	return compactChromeLines
}

// ConflictCount returns the number of conflicts, resolved or not.
func (m MergeModel) ConflictCount() int {
	count := 0
	for _, chunk := range m.chunks {
		if chunk.Kind == merge.Conflict {
			count++
		}
	}
	return count
}

// conflictNumber returns the one-based position of chunk among the conflicts, 0 when it is none.
func (m MergeModel) conflictNumber(chunk int) int {
	if m.chunks[chunk].Kind != merge.Conflict {
		return 0
	}
	number := 0
	for i := 0; i <= chunk; i++ {
		if m.chunks[i].Kind == merge.Conflict {
			number++
		}
	}
	return number
}

func choiceName(choice Choice) string {
	switch choice {
	case ChoiceOurs:
		return "ours"
	case ChoiceTheirs:
		return "theirs"
	case ChoiceBoth:
		return "both"
	case ChoiceBase:
		return "base"
	case ChoiceCustom:
		return "manual edit"
	}
	return "automatic merge"
}

// layout returns the panes that are shown (left to right) and their widths.
func (m MergeModel) layout() (panes []int, widths [mergePanes]int) {
	if m.width < threePaneWidth || m.editing {
		pane := m.viewPane
		if m.editing {
			pane = paneResult
		}
		widths[pane] = max(m.width-markerWidth, 0)
		return []int{pane}, widths
	}
	avail := max(m.width-markerWidth-2*mergeGutterWidth, 0)
	result := avail * resultPercent / percentFull
	side := (avail - result) / 2
	widths = [mergePanes]int{side, result, avail - result - side}
	return []int{paneOurs, paneResult, paneTheirs}, widths
}

// cycleView switches the pane shown in the narrow single pane layout.
func (m *MergeModel) cycleView() {
	if m.width >= threePaneWidth {
		m.notice("All three panes are visible", false)
		return
	}
	m.viewPane = (m.viewPane + 1) % mergePanes
}

// notice sets the message line; a warning is drawn in the warning style and with a "!" prefix.
func (m *MergeModel) notice(text string, warn bool) {
	m.status, m.warn = text, warn && text != ""
}

// barPart is a piece of a status line with its own style.
type barPart struct {
	text  string
	style lipgloss.Style
}

func segmentsWidth(segments []barPart) int {
	width := 0
	for _, s := range segments {
		width += runewidth.StringWidth(s.text)
	}
	return width
}

// renderSegments draws the segments in exactly width columns: too long text is cut, the rest is filled.
func renderSegments(segments []barPart, width int, fill lipgloss.Style) string {
	var out strings.Builder
	used := 0
	for _, s := range segments {
		text := runewidth.Truncate(s.text, width-used, "")
		if text == "" {
			continue
		}
		out.WriteString(s.style.Render(text))
		used += runewidth.StringWidth(text)
	}
	if used < width {
		out.WriteString(fill.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

// leftRight puts left and right segments on one line; the right side is dropped when it does not fit.
func leftRight(left, right []barPart, width int, fill lipgloss.Style) string {
	rightWidth := segmentsWidth(right)
	gap := width - segmentsWidth(left) - rightWidth
	if gap < 1 {
		if width-rightWidth-1 < width/2 { // no room for both: the left side wins
			return renderSegments(left, width, fill)
		}
		gap = 1
	}
	line := append(append([]barPart(nil), left...), barPart{strings.Repeat(" ", gap), fill})
	return renderSegments(append(line, right...), width, fill)
}

// View implements tea.Model.
func (m MergeModel) View() string {
	if m.width == 0 {
		return ""
	}
	panes, widths := m.layout()
	numWidth := len(fmt.Sprint(max(len(m.rows), 1))) + 1
	body := m.bodyHeight() // the footer wraps: its height is worked out once

	var out strings.Builder
	out.WriteString(m.header(panes, widths))
	out.WriteByte('\n')
	if m.editing {
		out.WriteString(m.editorBody(widths[paneResult]))
		out.WriteString(m.footer())
		return out.String()
	}
	drawn := 0
	for i := m.offset; i < len(m.rows) && drawn < body; i++ {
		row := m.rows[i]
		height := m.rowHeight(i, panes, widths, numWidth)
		var cells [mergePanes][]string
		for _, pane := range panes {
			cell := row.cells[pane]
			if pane == paneResult && i == m.cursor && cell.Kind != CellFiller {
				cell.Kind = CellCursor
			}
			cells[pane] = m.theme.renderCellLines(cell, numWidth, widths[pane], height, !m.noWrap)
		}
		for line := 0; line < height && drawn < body; line++ {
			out.WriteString(m.marker(i, row, line > 0))
			for index, pane := range panes {
				if index > 0 {
					side := paneOurs // the gutter before the result pane belongs to ours, the one before theirs to theirs
					if pane == paneTheirs {
						side = paneTheirs
					}
					out.WriteString(m.gutter(row, side, line > 0))
				}
				out.WriteString(cells[pane][line])
			}
			out.WriteByte('\n')
			drawn++
		}
	}
	for ; drawn < body; drawn++ {
		out.WriteByte('\n')
	}
	out.WriteString(m.footer())
	if m.palette != nil {
		return m.palette.picker.overlay(out.String(), m.width, m.height)
	}
	return out.String()
}

// numWidth is the width of the line number column.
func (m MergeModel) numWidth() int { return len(fmt.Sprint(max(len(m.rows), 1))) + 1 }

// rowHeight is the number of screen lines a row takes: one, or with wrapping the most lines any shown pane needs.
func (m MergeModel) rowHeight(i int, panes []int, widths [mergePanes]int, numWidth int) int {
	if m.noWrap {
		return 1
	}
	height := 1
	for _, pane := range panes {
		cell := m.rows[i].cells[pane]
		if cell.Kind == CellFiller {
			continue
		}
		num := numWidth
		if widths[pane] < numWidth {
			num = 0
		}
		height = max(height, wrappedHeight(cell.Text, widths[pane]-num))
	}
	return height
}

// rowFullyVisible tells whether row is completely shown when the view starts at offset.
func (m MergeModel) rowFullyVisible(row int) bool {
	if row < m.offset || row >= len(m.rows) {
		return false
	}
	panes, widths := m.layout()
	numWidth, used := m.numWidth(), 0
	for i := m.offset; i <= row; i++ {
		if used += m.rowHeight(i, panes, widths, numWidth); used > m.bodyHeight() {
			return i == m.offset && i == row // a row taller than the screen counts as visible when it starts the view
		}
	}
	return true
}

// firstRowFor returns the lowest offset at which row is completely visible (the row itself when it is taller
// than the screen).
func (m MergeModel) firstRowFor(row int) int {
	if len(m.rows) == 0 {
		return 0
	}
	row = min(max(row, 0), len(m.rows)-1)
	panes, widths := m.layout()
	numWidth, used, first := m.numWidth(), 0, row
	for i := row; i >= 0; i-- {
		if used += m.rowHeight(i, panes, widths, numWidth); used > m.bodyHeight() && i < row {
			break
		}
		first = i
	}
	return first
}

// lastOffset is the highest useful offset: the last rows fill the screen from the bottom.
func (m MergeModel) lastOffset() int { return m.firstRowFor(len(m.rows) - 1) }

// marker draws the edge column of a row: a bar for the selected change and the state of its chunk. A
// continuation line is a further screen line of a wrapped row.
func (m MergeModel) marker(rowIndex int, row mergeRow, continuation bool) string {
	if m.width < markerWidth {
		return ""
	}
	current := m.current >= 0 && m.changed[m.current] == row.chunk
	bar := m.theme.Gutter.Render(" ")
	if current {
		bar = m.theme.GutterCur.Render(symbolCurrent)
	}
	state := m.stateOf(row.chunk)
	if state == stateUntouched {
		return bar + m.theme.Gutter.Render(" ")
	}
	if !row.first || continuation {
		return bar + m.theme.Gutter.Render(symbolContinue)
	}
	switch state {
	case stateUnresolved:
		return bar + m.theme.ConflictMark.Render(symbolUnresolved)
	case stateResolved:
		return bar + m.theme.StatusAdded.Render(symbolResolved)
	default:
		return bar + m.theme.StatusModified.Render(symbolAuto)
	}
}

func (m MergeModel) saveBadge() barPart {
	switch {
	case m.saveFailed:
		return barPart{"✗ Save failed", m.theme.Header}
	case m.dirty:
		return barPart{"● Modified", m.theme.Header}
	default:
		return barPart{"✓ Saved", m.theme.Header}
	}
}

func (m MergeModel) conflictSummary() barPart {
	switch left := m.Unresolved(); {
	case left == 1:
		return barPart{"1 conflict remaining", m.theme.Header}
	case left > 1:
		return barPart{fmt.Sprintf("%d conflicts remaining", left), m.theme.Header}
	case m.ConflictCount() == 0:
		return barPart{"no conflicts", m.theme.Header}
	default:
		return barPart{"✓ all conflicts resolved", m.theme.Header}
	}
}

// header draws the title and progress lines (full layout) and always the pane labels.
func (m MergeModel) header(panes []int, widths [mergePanes]int) string {
	if m.height < fullChromeHeight {
		return m.paneLabels(panes, widths)
	}
	title := []barPart{
		{" Jelly Dev", m.theme.Header},
		{"  " + sanitize(m.titles.Name), m.theme.Header},
	}
	status := []barPart{m.conflictSummary(), {"   ", m.theme.Header}, m.saveBadge(), {" ", m.theme.Header}}
	lines := []string{
		leftRight(title, status, m.width, m.theme.Header),
		m.paneLabels(panes, widths),
		m.progressLine(),
	}
	return strings.Join(lines, "\n")
}

// paneLabels draws OURS / RESULT / THEIRS above their panes; the result pane is the highlighted one.
func (m MergeModel) paneLabels(panes []int, widths [mergePanes]int) string {
	names := [mergePanes]string{"OURS", "RESULT", "THEIRS"}
	files := [mergePanes]string{m.titles.Ours, m.titles.Result, m.titles.Theirs}
	out := m.theme.Header.Render(strings.Repeat(" ", min(markerWidth, m.width)))
	for index, pane := range panes {
		if index > 0 {
			out += m.theme.Header.Render(strings.Repeat(" ", mergeGutterWidth))
		}
		label := " " + names[pane]
		if file := sanitize(files[pane]); file != "" {
			label += " · " + file
		}
		switch {
		case m.editing:
			label += "   (editing)"
		case len(panes) == 1:
			label += "   (v: switch pane)"
		}
		style := m.theme.Header
		if pane == paneResult {
			style = m.theme.Selected.Bold(true)
		}
		out += style.Render(padRight(label, widths[pane]))
	}
	return out
}

// position describes the selected change: which one, and whether it is settled.
func (m MergeModel) position() string {
	if m.current < 0 {
		if m.Unresolved() > 0 {
			return "No change selected · n jumps to the next one"
		}
		return "No change selected"
	}
	chunk := m.changed[m.current]
	var what string
	if number := m.conflictNumber(chunk); number > 0 {
		what = fmt.Sprintf("Conflict %d of %d", number, m.ConflictCount())
	} else {
		what = fmt.Sprintf("Change %d of %d", m.current+1, len(m.changed))
	}
	switch m.stateOf(chunk) {
	case stateUnresolved:
		return what + " · unresolved"
	case stateResolved:
		return what + " · resolved (" + choiceName(m.choices[chunk]) + ")"
	default:
		return what + " · merged automatically"
	}
}

func (m MergeModel) progressLine() string {
	left := []barPart{{" " + m.position(), m.theme.Header}}
	var right []barPart
	if total := m.ConflictCount(); total > 0 {
		resolved := total - m.Unresolved()
		text := fmt.Sprintf("%d/%d resolved ", resolved, total)
		if m.width >= minBarWidth {
			filled := resolved * progressBarWidth / total
			bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)
			text = bar + " " + text
		}
		right = []barPart{{text, m.theme.Header}}
	}
	return leftRight(left, right, m.width, m.theme.Header)
}

// footerMaxLines is the number of lines each of the two footer bars may take: a second one only when the terminal is high
// enough to spare it.
func (m MergeModel) footerMaxLines() int {
	if m.height >= fullChromeHeight+4 {
		return 2
	}
	return 1
}

// actionRows is the footer: a message, the editor status or the one line of main actions; every other action is in the
// command palette.
func (m MergeModel) actionRows() []*bar {
	footer := m.theme.Footer
	maxLines := m.footerMaxLines()
	text := func(text string, style lipgloss.Style) []*bar { return textRows(text, m.width, maxLines, style) }
	switch {
	case m.editing && m.status == "":
		return text(m.editingStatus(), m.theme.Header)
	case m.status != "" && m.warn:
		return text("! "+m.status, m.theme.Header.Background(footer.GetBackground()))
	case m.status != "":
		return text(m.status, m.theme.Header)
	}
	return m.actionBar().wrap(m.width, maxLines)
}

// actionBar is the single footer line: the pick actions and the way to the command palette.
func (m MergeModel) actionBar() *bar {
	result := &bar{}
	selected := m.current >= 0
	result.add(barPart{" ", m.theme.Footer})
	chunk := 0
	if selected {
		chunk = m.changed[m.current]
	}
	mark := func(side int) string {
		if selected && m.sideIncluded(chunk, side) {
			return " ✓"
		}
		return ""
	}
	result.binding(keymap.MergeOurs, "ours"+mark(paneOurs), selected, m.theme)
	result.binding(keymap.MergeTheirs, "theirs"+mark(paneTheirs), selected, m.theme)
	result.binding(keymap.MergeEdit, "edit", len(m.rows) > 0, m.theme)
	result.binding(keymap.MergePalette, "actions", true, m.theme)
	return result
}

// footerBars are the bars of the full footer from top to bottom; it is not used by the compact footer of small terminals.
func (m MergeModel) footerBars() []*bar {
	if m.editing {
		return append(m.actionRows(), m.editorBar().wrap(m.width, m.footerMaxLines())...)
	}
	return m.actionRows()
}

func (m MergeModel) footer() string {
	if m.height < fullChromeHeight {
		return m.compactFooter()
	}
	return strings.Join(renderRowLines(m.footerBars(), m.width, m.theme.Footer), "\n")
}

// compactFooter is the single footer line of small terminals: a message, or counters and the main keys.
func (m MergeModel) compactFooter() string {
	if m.editing || m.status != "" {
		return renderRows(m.actionRows(), m.width, m.theme.Footer)
	}
	badge := m.saveBadge()
	badge.style = m.theme.Footer
	summary := fmt.Sprintf(" %d left · ", m.Unresolved())
	var main strings.Builder
	for _, command := range []keymap.Command{keymap.MergeOurs, keymap.MergeTheirs, keymap.MergeEdit, keymap.MergePalette} {
		if label := keys.Label(command); label != "" {
			fmt.Fprintf(&main, " [%s]", label)
		}
	}
	return renderSegments([]barPart{
		{summary, m.theme.Footer}, badge, {"  " + main.String(), m.theme.Footer},
	}, m.width, m.theme.Footer)
}

// headerLines is the number of lines above the panes.
func (m MergeModel) headerLines() int {
	if m.height >= fullChromeHeight {
		return fullHeaderLines
	}
	return 1
}

// rowAtLine returns the row drawn on screen line y of the body and the line of that row (-1 for none).
func (m MergeModel) rowAtLine(y int, panes []int, widths [mergePanes]int) (row, line int) {
	if y < 0 || y >= m.bodyHeight() {
		return -1, 0
	}
	numWidth, top := m.numWidth(), 0
	for i := m.offset; i < len(m.rows); i++ {
		height := m.rowHeight(i, panes, widths, numWidth)
		if y < top+height {
			return i, y - top
		}
		top += height
	}
	return -1, 0
}

// click handles a left click on a gutter button (», « or ×) of a change: it selects the change and applies or
// discards that side.
func (m *MergeModel) click(x, y int) {
	if m.editing || m.width < threePaneWidth {
		return
	}
	panes, widths := m.layout()
	index, line := m.rowAtLine(y-m.headerLines(), panes, widths)
	if y < m.headerLines() || index < 0 || line > 0 { // the buttons are drawn on the first screen line of a row only
		return
	}
	left := markerWidth + widths[paneOurs]
	right := left + mergeGutterWidth + widths[paneResult]
	side, apply := -1, false
	switch {
	case x >= left && x < left+mergeGutterWidth:
		side, apply = paneOurs, x-left >= discardCells // left gutter: cross first, arrow towards the result
	case x >= right && x < right+mergeGutterWidth:
		side, apply = paneTheirs, x-right < mergeGutterWidth-discardCells
	}
	if side < 0 || !m.buttonShown(m.rows[index], side, apply) {
		return
	}
	m.setCursor(index)
	chunk := m.rows[index].chunk
	if choice := m.sideChoice(chunk, side, apply); choice != m.choices[chunk] {
		m.applyChoice(choice, false) // the user points at a change: stay on it instead of jumping on
	}
	m.clamp()
}

// sideIncluded tells whether a side's lines are part of the choice of a chunk.
func (m MergeModel) sideIncluded(chunk, side int) bool {
	choice := m.choices[chunk]
	if side == paneTheirs {
		return choice == ChoiceTheirs || choice == ChoiceBoth
	}
	return choice == ChoiceOurs || choice == ChoiceBoth
}

// unresolvedSelected tells whether the selected change is a conflict that still waits for a decision.
func (m MergeModel) unresolvedSelected() bool {
	return m.current >= 0 && m.stateOf(m.changed[m.current]) == stateUnresolved
}

// choiceMade tells whether the selected change has a decision that can be cancelled.
func (m MergeModel) choiceMade() bool {
	return m.current >= 0 && m.choices[m.changed[m.current]] != ChoiceAuto
}

// toggleSide is the keyboard version of the » / « / × buttons: it adds the side to the selected change, or
// removes it when it is part of the result already. The change stays selected, so the other side can still be added.
func (m *MergeModel) toggleSide(side int) {
	if m.current < 0 {
		m.notice("Select a change first (n/p)", true)
		return
	}
	chunk := m.changed[m.current]
	name, other, otherName := "ours", m.chunks[chunk].Ours, "theirs"
	arrow, otherArrow := "→", "←"
	if side == paneTheirs {
		name, other, otherName = "theirs", m.chunks[chunk].Theirs, "ours"
		arrow, otherArrow = "←", "→"
	}
	if equalLines(other, m.chunks[chunk].Base) {
		m.notice(fmt.Sprintf("%s has no change here", name), true)
		return
	}
	apply := !m.sideIncluded(chunk, side)
	choice := m.sideChoice(chunk, side, apply)
	if !apply && choice == ChoiceBase && m.chunks[chunk].Kind == merge.Conflict {
		choice = ChoiceAuto // removing the last side takes the conflict back to its initial, highlighted, unresolved state
	}
	m.applyChoice(choice, false)
	what := "Change"
	if number := m.conflictNumber(chunk); number > 0 {
		what = fmt.Sprintf("Conflict %d", number)
	}
	hint := fmt.Sprintf("%s adds %s · ⌫ cancels · n next", otherArrow, otherName)
	text := fmt.Sprintf("✓ %s: %s applied · %s removes it · %s", what, name, arrow, hint)
	if !apply {
		text = fmt.Sprintf("%s: %s removed · %s adds it again · ⌫ cancels · n next", what, name, arrow)
		if choice == ChoiceAuto {
			text = fmt.Sprintf("%s: %s removed · unresolved again · %s adds it again", what, name, arrow)
		}
	}
	if left := m.Unresolved(); left > 0 {
		text += fmt.Sprintf(" (%d conflict(s) left)", left)
	} else if apply {
		text = fmt.Sprintf("✓ %s: %s applied", what, name) // short: the completion message follows it
	}
	m.notice(text, false)
}

// ignoreChange resolves the selected conflict by keeping the lines the result pane shows now and taking neither
// side. The change stays selected, like after the arrows and clicks.
func (m *MergeModel) ignoreChange() {
	if m.current < 0 {
		m.notice("Select a change first (n/p)", true)
		return
	}
	chunk := m.changed[m.current]
	switch {
	case m.chunks[chunk].Kind != merge.Conflict:
		m.notice("Nothing to ignore: this change merged automatically (r reverts it)", true)
		return
	case m.choices[chunk] != ChoiceAuto:
		m.notice("Already decided - ⌫ cancels the decision, then x ignores", true)
		return
	}
	m.applyChoice(ChoiceBase, false)
	text := fmt.Sprintf("✓ Conflict %d: ignored - the current lines are kept", m.conflictNumber(chunk))
	if left := m.Unresolved(); left > 0 {
		text += fmt.Sprintf(" · ⌫ cancels · n next (%d conflict(s) left)", left)
	}
	m.notice(text, false)
}

// cancelChange throws the decision for the selected change away: it is unresolved again (automatic for a change
// that merged by itself).
func (m *MergeModel) cancelChange() {
	if m.current < 0 {
		m.notice("Select a change first (n/p)", true)
		return
	}
	chunk := m.changed[m.current]
	if m.choices[chunk] == ChoiceAuto {
		m.notice("Nothing to cancel: no decision made for this change", true)
		return
	}
	m.setChoice(chunk, ChoiceAuto, nil)
	m.rebuild()
	what := "Change"
	if number := m.conflictNumber(chunk); number > 0 {
		what = fmt.Sprintf("Conflict %d", number)
	}
	m.notice(fmt.Sprintf("%s: decision cancelled · %d conflict(s) left (u undoes)", what, m.Unresolved()), false)
	m.keepVisible()
}

// sideChoice returns the choice of a chunk after applying (») or discarding (×) the change of one side. Applying
// the second side after the first one gives both; discarding a side keeps the other one, or the base when no
// other side is wanted.
func (m MergeModel) sideChoice(chunk, side int, apply bool) Choice {
	current, c := m.choices[chunk], m.chunks[chunk]
	ours := current == ChoiceOurs || current == ChoiceBoth
	theirs := current == ChoiceTheirs || current == ChoiceBoth
	mine, other, myChoice, otherChoice, otherLines := ours, theirs, ChoiceOurs, ChoiceTheirs, c.Theirs
	if side == paneTheirs {
		mine, other, myChoice, otherChoice, otherLines = theirs, ours, ChoiceTheirs, ChoiceOurs, c.Ours
	}
	switch {
	case apply && other:
		return ChoiceBoth
	case apply:
		return myChoice
	case other:
		return otherChoice
	case mine, equalLines(otherLines, c.Base):
		return ChoiceBase
	default:
		return otherChoice
	}
}
