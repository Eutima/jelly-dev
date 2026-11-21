package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-diff/internal/merge"
)

const (
	mergeGutterWidth   = 5  // between two panes: room for an apply arrow and a discard cross
	markerWidth        = 2  // edge column: current-change bar and state symbol
	fullChromeHeight   = 10 // from this terminal height on the header has three lines and the footer two
	fullHeaderLines    = 3
	discardCells       = 2   // cells of a gutter that belong to the discard cross
	fullChromeLines    = 5   // header (title, pane labels, progress) + footer (actions, shortcuts)
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
		return fullChromeLines
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
	if m.width < threePaneWidth {
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

	var out strings.Builder
	out.WriteString(m.header(panes, widths))
	out.WriteByte('\n')
	end := min(m.offset+m.bodyHeight(), len(m.rows))
	for i := m.offset; i < end; i++ {
		row := m.rows[i]
		out.WriteString(m.marker(i, row))
		for index, pane := range panes {
			if index > 0 {
				side := paneOurs // the gutter before the result pane belongs to ours, the one before theirs to theirs
				if pane == paneTheirs {
					side = paneTheirs
				}
				out.WriteString(m.gutter(row, side))
			}
			if pane == paneResult {
				out.WriteString(m.resultCell(i, row, numWidth, widths[pane]))
			} else {
				out.WriteString(m.theme.renderCell(row.cells[pane], numWidth, widths[pane]))
			}
		}
		out.WriteByte('\n')
	}
	for i := end - m.offset; i < m.bodyHeight(); i++ {
		out.WriteByte('\n')
	}
	out.WriteString(m.footer())
	return out.String()
}

// marker draws the edge column of a row: a bar for the selected change and the state of its chunk.
func (m MergeModel) marker(rowIndex int, row mergeRow) string {
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
	if !row.first {
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
	bg := m.theme.Header.GetBackground()
	switch {
	case m.saveFailed:
		return barPart{"✗ Save failed", m.theme.Warning.Background(bg)}
	case m.dirty:
		return barPart{"● Modified", m.theme.Warning.Background(bg)}
	default:
		return barPart{"✓ Saved", m.theme.StatusAdded.Background(bg)}
	}
}

func (m MergeModel) conflictSummary() barPart {
	bg := m.theme.Header.GetBackground()
	switch left := m.Unresolved(); {
	case left == 1:
		return barPart{"1 conflict remaining", m.theme.ConflictMark.Background(bg)}
	case left > 1:
		return barPart{fmt.Sprintf("%d conflicts remaining", left), m.theme.ConflictMark.Background(bg)}
	case m.ConflictCount() == 0:
		return barPart{"no conflicts", m.theme.Header}
	default:
		return barPart{"✓ all conflicts resolved", m.theme.StatusAdded.Background(bg)}
	}
}

// header draws the title and progress lines (full layout) and always the pane labels.
func (m MergeModel) header(panes []int, widths [mergePanes]int) string {
	if m.height < fullChromeHeight {
		return m.paneLabels(panes, widths)
	}
	bg := m.theme.Header.GetBackground()
	title := []barPart{
		{" Jelly-", m.theme.StatusModified.Background(bg)}, {"Merge", m.theme.Jelly.Bold(true).Background(bg)},
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
		if len(panes) == 1 {
			label += "   (v: switch pane)"
		}
		style := m.theme.Header.Faint(true)
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

// hint is one shortcut of the footer.
type hint struct {
	key, label string
	enabled    bool
	drop       int // 0 is always kept; higher numbers are dropped first when the terminal is too narrow
}

func (m MergeModel) hints() [][]hint {
	_, _, onLine := m.resultLineAt(m.cursor)
	selected := m.current >= 0
	save := hint{"s", "save", m.Unresolved() == 0, 0}
	if left := m.Unresolved(); left > 0 {
		save.label = fmt.Sprintf("save (%d left)", left)
	}
	view := []hint{{"↑↓", "move", true, 2}, {"n/p", "change", true, 0}}
	if m.width < threePaneWidth {
		view = append(view, hint{"v", "pane", true, 0})
	}
	return [][]hint{
		view,
		{
			{"o", "ours", selected, 0}, {"t", "theirs", selected, 0}, {"b", "both", selected, 0},
			{"r", "base", selected, 1}, {"m", "auto-resolve", true, 0}, {"e", "edit", onLine, 0},
		},
		{{"d", "del", onLine, 2}, {"i", "ins", len(m.rows) > 0, 2}},
		{{"u", "undo", len(m.history) > 0, 0}, save, {"q", "quit", true, 0}},
	}
}

// shortcutLine draws the shortcut groups, dropping the less important hints when the width is short.
func (m MergeModel) shortcutLine() string {
	groups := m.hints()
	build := func(maxDrop int) []barPart {
		var segments []barPart
		for _, group := range groups {
			var parts []barPart
			for _, h := range group {
				if h.drop > maxDrop {
					continue
				}
				keyStyle, labelStyle := m.theme.Header, m.theme.Footer
				if !h.enabled {
					keyStyle, labelStyle = m.theme.Footer.Faint(true), m.theme.Footer.Faint(true)
				}
				parts = append(parts, barPart{h.key, keyStyle}, barPart{" " + h.label + "  ", labelStyle})
			}
			if len(parts) > 0 {
				if len(segments) > 0 {
					segments = append(segments, barPart{"│ ", m.theme.Footer.Faint(true)})
				}
				segments = append(segments, parts...)
			}
		}
		return segments
	}
	var segments []barPart
	for maxDrop := 2; maxDrop >= 0; maxDrop-- {
		segments = append([]barPart{{" ", m.theme.Footer}}, build(maxDrop)...)
		if segmentsWidth(segments) <= m.width {
			break
		}
	}
	return renderSegments(segments, m.width, m.theme.Footer)
}

// actionLine is the first footer line: a message, the editor hints or the actions for the selected change.
func (m MergeModel) actionLine() string {
	footer := m.theme.Footer
	switch {
	case m.editing:
		return renderSegments([]barPart{{" " + m.editingStatus(), m.theme.Header}}, m.width, footer)
	case m.status != "" && m.warn:
		return renderSegments([]barPart{{" ! " + m.status, m.theme.Warning.Background(footer.GetBackground())}}, m.width, footer)
	case m.status != "":
		return renderSegments([]barPart{{" " + m.status, m.theme.Header}}, m.width, footer)
	}
	key := func(k, label string) []barPart {
		return []barPart{{"[" + k + "]", m.theme.Header}, {" " + label + "  ", footer}}
	}
	var segments []barPart
	switch {
	case m.current >= 0:
		title := "Resolve change"
		if number := m.conflictNumber(m.changed[m.current]); number > 0 {
			title = fmt.Sprintf("Resolve conflict %d/%d", number, m.ConflictCount())
		}
		segments = append(segments, barPart{" " + title + "   ", m.theme.Header})
		for _, k := range [][2]string{{"o", "Use ours"}, {"t", "Use theirs"}, {"b", "Use both"}, {"e", "Edit"}} {
			segments = append(segments, key(k[0], k[1])...)
		}
		segments = append(segments, barPart{" r base  m auto-resolve", footer.Faint(true)})
	case m.Unresolved() > 0:
		segments = []barPart{{" Press n to jump to the next conflict, m to auto-resolve the simple ones", footer}}
	case m.dirty:
		segments = []barPart{{" ✓ All conflicts resolved · press s to save, or n to review the changes", m.theme.StatusAdded.Background(footer.GetBackground())}}
	default:
		segments = []barPart{{" ✓ Result saved · press q to quit", m.theme.StatusAdded.Background(footer.GetBackground())}}
	}
	return renderSegments(segments, m.width, footer)
}

func (m MergeModel) footer() string {
	if m.height < fullChromeHeight {
		return m.compactFooter()
	}
	return m.actionLine() + "\n" + m.shortcutLine()
}

// compactFooter is the single footer line of small terminals: a message, or counters and the main keys.
func (m MergeModel) compactFooter() string {
	if m.editing || m.status != "" {
		return m.actionLine()
	}
	badge := m.saveBadge()
	badge.style = m.theme.Footer
	summary := fmt.Sprintf(" %d left · ", m.Unresolved())
	return renderSegments([]barPart{
		{summary, m.theme.Footer}, badge, {"   n/p o t b e u s q", m.theme.Footer},
	}, m.width, m.theme.Footer)
}

// headerLines is the number of lines above the panes.
func (m MergeModel) headerLines() int {
	if m.height >= fullChromeHeight {
		return fullHeaderLines
	}
	return 1
}

// click handles a left click: on a » or « arrow of a change it selects the change and applies ours or theirs.
func (m *MergeModel) click(x, y int) {
	if m.editing || m.width < threePaneWidth {
		return
	}
	index := m.offset + y - m.headerLines()
	if y < m.headerLines() || index < 0 || index >= len(m.rows) || y-m.headerLines() >= m.bodyHeight() {
		return
	}
	_, widths := m.layout()
	left := markerWidth + widths[paneOurs]
	right := left + mergeGutterWidth + widths[paneResult]
	side, apply := -1, false
	switch {
	case x >= left && x < left+mergeGutterWidth:
		side, apply = paneOurs, x-left >= discardCells // left gutter: cross first, arrow towards the result
	case x >= right && x < right+mergeGutterWidth:
		side, apply = paneTheirs, x-right < mergeGutterWidth-discardCells
	}
	if side < 0 || !m.arrowShown(m.rows[index], side) {
		return
	}
	m.setCursor(index)
	chunk := m.rows[index].chunk
	if choice := m.sideChoice(chunk, side, apply); choice != m.choices[chunk] {
		m.applyChoice(choice, false) // the user points at a change: stay on it instead of jumping on
	}
	m.clamp()
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
