package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/source"
)

const (
	mergedMark      = "✓"
	onlyInLeftNote  = "  (only in a)"
	onlyInRightNote = "  (only in b)"
)

// MergeOpener builds the merge viewer of one listed file. The returned outcome tells afterwards whether the
// result was saved.
type MergeOpener func(file source.ChangedFile) (MergeModel, *MergeOutcome, error)

// DirMergeModel lists the files that differ between two directories; enter opens one in the merge viewer, the
// result of which is saved into the left directory.
type DirMergeModel struct {
	leftLabel, rightLabel string
	files                 []source.ChangedFile
	open                  MergeOpener
	merged                []bool // per file: its result was saved
	selected, listTop     int
	status                string
	theme                 Theme

	child      MergeModel // the open merge viewer
	hasChild   bool
	childIndex int
	outcome    *MergeOutcome

	width, height int
}

// NewDirMergeModel builds the file list; open creates the merge viewer when a file is opened.
func NewDirMergeModel(leftLabel, rightLabel string, files []source.ChangedFile, open MergeOpener) DirMergeModel {
	return DirMergeModel{
		leftLabel: leftLabel, rightLabel: rightLabel, files: files, open: open,
		merged: make([]bool, len(files)), theme: DarkTheme(),
	}
}

// MergedCount returns the number of files whose result was saved.
func (m DirMergeModel) MergedCount() int {
	count := 0
	for _, done := range m.merged {
		if done {
			count++
		}
	}
	return count
}

// Init implements tea.Model.
func (m DirMergeModel) Init() tea.Cmd { return nil }

func (m DirMergeModel) bodyHeight() int { return max(m.height-headerLines-len(m.footerLines()), 1) }

// Update implements tea.Model.
func (m DirMergeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.ensureVisible()
	}
	if m.hasChild {
		return m.updateChild(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		return m.handleKey(key)
	}
	if mouse, ok := msg.(tea.MouseMsg); ok && leftPress(mouse) && m.status == "" {
		if keys := rowKeysAt(m.footerRows(), mouse.X, mouse.Y, m.width, m.height); keys != nil {
			return replay(m, keys)
		}
	}
	return m, nil
}

// updateChild passes a message to the open merge viewer and returns to the list when it was left.
func (m DirMergeModel) updateChild(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.child.Update(msg)
	m.child = updated.(MergeModel)
	if !m.child.Done() {
		return m, cmd
	}
	m.hasChild = false
	if m.outcome != nil && m.outcome.Saved {
		m.merged = append([]bool(nil), m.merged...) // fresh slice: older copies of the model must not change
		m.merged[m.childIndex] = true
		m.status = fmt.Sprintf("%s %s saved", mergedMark, m.files[m.childIndex].Path)
	} else {
		m.status = ""
	}
	m.ensureVisible() // a message of several lines makes the list shorter
	return m, nil
}

func (m DirMergeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	if keys.Is(keymap.DiffQuit, msg.String()) || msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch msg.String() {
	case "j", "down":
		m.selected++
	case "k", "up":
		m.selected--
	case "pgdown":
		m.selected += m.bodyHeight()
	case "pgup":
		m.selected -= m.bodyHeight()
	case "g", "home":
		m.selected = 0
	case "G", "end":
		m.selected = len(m.files) - 1
	case "enter", "right", "l":
		m.openSelected()
	}
	m.selected = min(max(m.selected, 0), max(len(m.files)-1, 0))
	m.ensureVisible()
	return m, nil
}

func (m *DirMergeModel) openSelected() {
	if len(m.files) == 0 {
		return
	}
	file := m.files[m.selected]
	model, outcome, err := m.open(file)
	if err != nil {
		m.status = fmt.Sprintf("Cannot open %s: %v", file.Path, err)
		return
	}
	updated, _ := model.WithEmbedded().WithTheme(m.theme).Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	m.child, m.hasChild, m.childIndex, m.outcome = updated.(MergeModel), true, m.selected, outcome
}

func (m *DirMergeModel) ensureVisible() {
	m.listTop = scrollToShow(m.selected, m.listTop, m.bodyHeight(), len(m.files), func(row int) int { return len(m.fileRowLines(row)) })
}

// View implements tea.Model.
func (m DirMergeModel) View() string {
	if m.hasChild {
		return m.child.View()
	}
	if m.width == 0 {
		return ""
	}
	title := fmt.Sprintf(" Merge %s ← %s   %d files, %d merged", sanitize(m.leftLabel), sanitize(m.rightLabel),
		len(m.files), m.MergedCount())
	lines := []string{m.theme.Header.Render(padRight(title, m.width))}
	body := m.bodyHeight()
	for index := m.listTop; index < len(m.files) && len(lines) <= body; index++ {
		lines = append(lines, m.fileRowLines(index)...)
	}
	lines = lines[:min(len(lines), body+1)]
	for len(lines) <= body {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	lines = append(lines, m.footerLines()...)
	return strings.Join(lines, "\n")
}

// fileRowLines draws the row of a file: a long name continues on further lines.
func (m DirMergeModel) fileRowLines(index int) []string {
	file := m.files[index]
	style := m.theme.Equal
	if index == m.selected {
		style = m.theme.Selected
	}
	label := sanitize(file.Path)
	switch file.Status {
	case source.StatusDeleted:
		label += onlyInLeftNote
	case source.StatusAdded:
		label += onlyInRightNote
	}
	mark := "  "
	if m.merged[index] {
		mark = mergedMark + " "
	}
	status := m.theme.statusStyle(file.Status).Inherit(style).Render(" " + string(rune(file.Status)) + " ")
	const statusWidth, markWidth = 3, 2
	parts := splitRowLines(label, max(m.width-statusWidth-markWidth, 1))
	lines := make([]string, len(parts))
	for i, part := range parts {
		text := style.Render(padRight(part, max(m.width-statusWidth-markWidth, 0)))
		switch {
		case i == 0:
			lines[i] = status + text + style.Render(mark)
		default:
			lines[i] = style.Render(strings.Repeat(" ", statusWidth)) + text + style.Render(strings.Repeat(" ", markWidth))
		}
	}
	return lines
}

// footerLines are the lines at the bottom: the message of the last action, or the keys; both wrap instead of being cut off.
func (m DirMergeModel) footerLines() []string {
	if m.status != "" {
		// the status can contain file names and error texts: sanitize strips control characters
		return messageLines(sanitize(m.status), m.width, messageCap(m.height), m.theme.Footer)
	}
	return renderRowLines(m.footerRows(), m.width, m.theme.Footer)
}

// footerRows are the keys as wrapped bars.
func (m DirMergeModel) footerRows() []*bar { return m.footerBar().wrap(m.width, footerMax) }

// footerBar builds the clickable key hints of the file list.
func (m DirMergeModel) footerBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	result.command("↑↓", "files", true, m.theme)
	result.command("enter", "merge into "+sanitize(m.leftLabel), true, m.theme)
	result.binding(keymap.DiffQuit, "quit", true, m.theme)
	return result
}
