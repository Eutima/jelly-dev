package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/diff"
)

const (
	gutterWidth = 3
	chromeLines = 2 // header + footer
	wheelLines  = 3
	signWidth   = 2 // "+ " / "- " / "  " in the unified view
)

// DiffModel is the side-by-side diff viewer.
type DiffModel struct {
	leftTitle   string
	rightTitle  string
	rows        []Row
	hunkStarts  []int
	unified     []UnifiedRow
	unifiedAt   []int // first unified row of each change
	showUnified bool
	theme       Theme

	width, height int
	offset        int // first visible row
	current       int // current hunk, -1 when none selected
}

// NewDiffModel builds a viewer for two texts.
func NewDiffModel(leftTitle, rightTitle string, oldLines, newLines []string) DiffModel {
	blocks := diff.Lines(oldLines, newLines)
	rows, hunkStarts := BuildRows(blocks)
	unified := BuildUnified(blocks)
	return DiffModel{
		leftTitle: leftTitle, rightTitle: rightTitle,
		rows: rows, hunkStarts: hunkStarts, unified: unified, unifiedAt: unifiedStarts(unified),
		theme: DarkTheme(), current: -1,
	}
}

// unifiedStarts returns the index of the first row of each change.
func unifiedStarts(rows []UnifiedRow) []int {
	var starts []int
	for i, row := range rows {
		if row.Hunk >= 0 && (i == 0 || rows[i-1].Hunk != row.Hunk) {
			starts = append(starts, i)
		}
	}
	return starts
}

// rowCount returns the number of rows of the active view.
func (m DiffModel) rowCount() int {
	if m.showUnified {
		return len(m.unified)
	}
	return len(m.rows)
}

// starts returns the first row of each change in the active view.
func (m DiffModel) starts() []int {
	if m.showUnified {
		return m.unifiedAt
	}
	return m.hunkStarts
}

// HunkCount returns the number of changes.
func (m DiffModel) HunkCount() int { return len(m.hunkStarts) }

// Init implements tea.Model.
func (m DiffModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m DiffModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m DiffModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := max(m.bodyHeight()-1, 1)
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
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
		m.offset = m.rowCount()
	case "t":
		m.showUnified = !m.showUnified
		if m.current >= 0 {
			m.gotoHunk(m.current)
		}
	case "n", "tab":
		m.gotoHunk(m.current + 1)
	case "p", "shift+tab":
		if m.current < 0 {
			m.gotoHunk(m.HunkCount() - 1)
		} else {
			m.gotoHunk(m.current - 1)
		}
	}
	m.clamp()
	return m, nil
}

// gotoHunk selects a change (wrapping around) and scrolls it into view.
func (m *DiffModel) gotoHunk(index int) {
	starts := m.starts()
	count := len(starts)
	if count == 0 {
		return
	}
	m.current = (index%count + count) % count
	m.offset = max(starts[m.current]-m.bodyHeight()/3, 0)
}

func (m DiffModel) bodyHeight() int { return max(m.height-chromeLines, 1) }

func (m *DiffModel) clamp() {
	maxOffset := max(m.rowCount()-m.bodyHeight(), 0)
	m.offset = min(max(m.offset, 0), maxOffset)
}

// View implements tea.Model.
func (m DiffModel) View() string {
	if m.width == 0 {
		return ""
	}
	paneWidth := max((m.width-gutterWidth)/2, 0)
	rightWidth := max(m.width-paneWidth-gutterWidth, 0)
	numWidth := len(fmt.Sprint(max(len(m.rows), len(m.unified), 1))) + 1

	var out strings.Builder
	out.WriteString(m.header(paneWidth, rightWidth))
	out.WriteByte('\n')
	end := min(m.offset+m.bodyHeight(), m.rowCount())
	for i := m.offset; i < end; i++ {
		if m.showUnified {
			out.WriteString(m.renderUnifiedRow(m.unified[i], numWidth))
		} else {
			row := m.rows[i]
			out.WriteString(m.renderCell(row.Left, numWidth, paneWidth))
			out.WriteString(m.renderGutter(row))
			out.WriteString(m.renderCell(row.Right, numWidth, rightWidth))
		}
		out.WriteByte('\n')
	}
	for i := end - m.offset; i < m.bodyHeight(); i++ {
		out.WriteByte('\n')
	}
	out.WriteString(m.footer())
	return out.String()
}

func (m DiffModel) header(paneWidth, rightWidth int) string {
	if m.showUnified {
		title := " " + sanitize(m.leftTitle) + " → " + sanitize(m.rightTitle)
		return m.theme.Header.Render(padRight(title, m.width))
	}
	left := padRight(" "+sanitize(m.leftTitle), paneWidth)
	right := padRight(" "+sanitize(m.rightTitle), rightWidth)
	return m.theme.Header.Render(left + strings.Repeat(" ", gutterWidth) + right)
}

func (m DiffModel) footer() string {
	position := "no changes"
	if n := m.HunkCount(); n > 0 {
		position = fmt.Sprintf("%d changes", n)
		if m.current >= 0 {
			position = fmt.Sprintf("change %d/%d", m.current+1, n)
		}
	}
	text := fmt.Sprintf(" %s   n/p next/prev change   j/k scroll   t split/unified   q quit", position)
	return m.theme.Footer.Render(padRight(text, m.width))
}

func (m DiffModel) renderGutter(row Row) string {
	if row.Hunk < 0 {
		return m.theme.Gutter.Render(" │ ")
	}
	style := m.theme.Gutter
	if row.Hunk == m.current {
		style = m.theme.GutterCur
	}
	switch {
	case row.Left.Kind == CellFiller:
		return style.Render(" « ")
	case row.Right.Kind == CellFiller:
		return style.Render(" » ")
	default:
		return style.Render(" ┃ ")
	}
}

// renderCell draws one pane cell exactly cellWidth columns wide.
func (m DiffModel) renderCell(cell Cell, numWidth, cellWidth int) string {
	style := m.styleFor(cell.Kind)
	if cell.Kind == CellFiller {
		return style.Render(strings.Repeat(" ", cellWidth))
	}
	// A pane narrower than the number column drops the number instead of showing a cut-off one.
	num := numWidth
	if cellWidth < numWidth {
		num = 0
	}
	textWidth := cellWidth - num
	number := ""
	if num > 0 {
		number = fmt.Sprintf("%*d ", num-1, cell.No)
	}
	text := renderText(cell.Text, cell.Spans, textWidth, style, m.wordStyleFor(cell.Kind))
	return m.theme.LineNumber.Background(style.GetBackground()).Render(number) + text
}

// renderUnifiedRow draws one full-width line of the unified view.
func (m DiffModel) renderUnifiedRow(row UnifiedRow, numWidth int) string {
	style := m.styleFor(row.Kind)
	sign := "  "
	switch row.Kind {
	case CellAdded:
		sign = "+ "
	case CellDeleted:
		sign = "- "
	}
	prefix := number(row.OldNo, numWidth) + number(row.NewNo, numWidth) + sign
	prefixStyle := m.theme.LineNumber
	if row.Hunk >= 0 && row.Hunk == m.current {
		prefixStyle = m.theme.GutterCur
	}
	prefixWidth := lipgloss.Width(prefix)
	if prefixWidth >= m.width {
		return style.Render(strings.Repeat(" ", m.width))
	}
	text := renderText(row.Text, row.Spans, m.width-prefixWidth, style, m.wordStyleFor(row.Kind))
	return prefixStyle.Background(style.GetBackground()).Render(prefix) + text
}

// number formats a one-based line number into numWidth columns (blank for 0).
func number(no, numWidth int) string {
	if no == 0 {
		return strings.Repeat(" ", numWidth)
	}
	return fmt.Sprintf("%*d ", numWidth-1, no)
}

func (m DiffModel) wordStyleFor(kind CellKind) lipgloss.Style {
	switch kind {
	case CellAdded:
		return m.theme.AddedWord
	case CellDeleted:
		return m.theme.DeletedWord
	case CellModified:
		return m.theme.ModifiedWord
	default:
		return m.styleFor(kind)
	}
}

func (m DiffModel) styleFor(kind CellKind) lipgloss.Style {
	switch kind {
	case CellAdded:
		return m.theme.Added
	case CellDeleted:
		return m.theme.Deleted
	case CellModified:
		return m.theme.Modified
	case CellFiller:
		return m.theme.Filler
	default:
		return m.theme.Equal
	}
}
