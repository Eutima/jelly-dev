package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-diff/internal/diff"
)

const (
	tabWidth    = 4
	gutterWidth = 3
	chromeLines = 2 // header + footer
	wheelLines  = 3
	replacement = '·'
)

// DiffModel is the side-by-side diff viewer.
type DiffModel struct {
	leftTitle  string
	rightTitle string
	rows       []Row
	hunkStarts []int
	theme      Theme

	width, height int
	offset        int // first visible row
	current       int // current hunk, -1 when none selected
}

// NewDiffModel builds a viewer for two texts.
func NewDiffModel(leftTitle, rightTitle string, oldLines, newLines []string) DiffModel {
	rows, hunkStarts := BuildRows(diff.Lines(oldLines, newLines))
	return DiffModel{
		leftTitle: leftTitle, rightTitle: rightTitle,
		rows: rows, hunkStarts: hunkStarts, theme: DarkTheme(), current: -1,
	}
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
		m.offset = len(m.rows)
	case "n", "tab":
		m.gotoHunk(m.current + 1)
	case "p", "shift+tab":
		if m.current < 0 {
			m.gotoHunk(len(m.hunkStarts) - 1)
		} else {
			m.gotoHunk(m.current - 1)
		}
	}
	m.clamp()
	return m, nil
}

// gotoHunk selects a change (wrapping around) and scrolls it into view.
func (m *DiffModel) gotoHunk(index int) {
	count := len(m.hunkStarts)
	if count == 0 {
		return
	}
	m.current = (index%count + count) % count
	m.offset = max(m.hunkStarts[m.current]-m.bodyHeight()/3, 0)
}

func (m DiffModel) bodyHeight() int { return max(m.height-chromeLines, 1) }

func (m *DiffModel) clamp() {
	maxOffset := max(len(m.rows)-m.bodyHeight(), 0)
	m.offset = min(max(m.offset, 0), maxOffset)
}

// View implements tea.Model.
func (m DiffModel) View() string {
	if m.width == 0 {
		return ""
	}
	paneWidth := max((m.width-gutterWidth)/2, 0)
	rightWidth := max(m.width-paneWidth-gutterWidth, 0)
	numWidth := len(fmt.Sprint(max(len(m.rows), 1))) + 1

	var out strings.Builder
	out.WriteString(m.header(paneWidth, rightWidth))
	out.WriteByte('\n')
	end := min(m.offset+m.bodyHeight(), len(m.rows))
	for i := m.offset; i < end; i++ {
		row := m.rows[i]
		out.WriteString(m.renderCell(row.Left, numWidth, paneWidth))
		out.WriteString(m.renderGutter(row))
		out.WriteString(m.renderCell(row.Right, numWidth, rightWidth))
		out.WriteByte('\n')
	}
	for i := end - m.offset; i < m.bodyHeight(); i++ {
		out.WriteByte('\n')
	}
	out.WriteString(m.footer())
	return out.String()
}

func (m DiffModel) header(paneWidth, rightWidth int) string {
	left := padRight(" "+sanitize(m.leftTitle), paneWidth)
	right := padRight(" "+sanitize(m.rightTitle), rightWidth)
	return m.theme.Header.Render(left + strings.Repeat(" ", gutterWidth) + right)
}

func (m DiffModel) footer() string {
	position := "no changes"
	if n := len(m.hunkStarts); n > 0 {
		position = fmt.Sprintf("%d changes", n)
		if m.current >= 0 {
			position = fmt.Sprintf("change %d/%d", m.current+1, n)
		}
	}
	text := fmt.Sprintf(" %s   n/p next/prev change   j/k scroll   g/G top/bottom   q quit", position)
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
	text := padRight(truncate(sanitize(cell.Text), textWidth), textWidth)
	return m.theme.LineNumber.Background(style.GetBackground()).Render(number) + style.Render(text)
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

// sanitize expands tabs to tab stops and replaces control characters, so file content can never
// inject terminal escape sequences.
func sanitize(text string) string {
	var out strings.Builder
	column := 0
	for _, r := range text {
		switch {
		case r == '\t':
			spaces := tabWidth - column%tabWidth
			out.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) ||
			r == '\u2028' || r == '\u2029':
			out.WriteRune(replacement)
			column++
		default:
			out.WriteRune(r)
			column += runewidth.RuneWidth(r)
		}
	}
	return out.String()
}

func truncate(text string, width int) string { return runewidth.Truncate(text, width, "…") }

func padRight(text string, width int) string {
	if gap := width - runewidth.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return runewidth.Truncate(text, width, "")
}
