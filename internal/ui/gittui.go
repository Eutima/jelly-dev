package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/diff"
	"github.com/eutima/jelly-diff/internal/source"
)

const (
	minListWidth = 20
	maxListWidth = 44
	listDivisor  = 3 // the file list takes at most 1/3 of the terminal
	separator    = "│"
	separatorW   = 1
)

// ChangeSource provides the texts of a changed file (implemented by source.GitChanges).
type ChangeSource interface {
	Labels() (oldLabel, newLabel string)
	Load(file source.ChangedFile) (oldText, newText string, err error)
}

type focus int

const (
	focusList focus = iota
	focusDiff
)

// GitModel shows a list of changed files next to the diff of the selected file.
type GitModel struct {
	files    []source.ChangedFile
	source   ChangeSource
	selected int
	listTop  int // first visible list row
	focus    focus
	diff     DiffModel
	message  string // shown instead of the diff when the file cannot be displayed
	theme    Theme

	width, height int
}

// NewGitModel builds the viewer and loads the diff of the first file.
func NewGitModel(files []source.ChangedFile, src ChangeSource) GitModel {
	model := GitModel{files: files, source: src, theme: DarkTheme()}
	model.selectFile(0)
	return model
}

// FileCount returns the number of changed files.
func (m GitModel) FileCount() int { return len(m.files) }

// Init implements tea.Model.
func (m GitModel) Init() tea.Cmd { return nil }

func (m GitModel) listWidth() int {
	return min(max(m.width/listDivisor, minListWidth), maxListWidth, max(m.width/2, 0))
}

func (m GitModel) diffWidth() int { return max(m.width-m.listWidth()-separatorW, 0) }

// selectFile loads the diff of file index i.
func (m *GitModel) selectFile(i int) {
	if len(m.files) == 0 {
		return
	}
	m.selected = min(max(i, 0), len(m.files)-1)
	m.ensureVisible()
	file := m.files[m.selected]
	oldLabel, newLabel := m.source.Labels()
	oldPath := file.Path
	if file.OldPath != "" {
		oldPath = file.OldPath
	}
	oldText, newText, err := m.source.Load(file)
	if err != nil {
		m.message = describeLoadError(err)
		m.diff = NewDiffModel(oldLabel+": "+oldPath, newLabel+": "+file.Path, nil, nil)
	} else {
		m.message = ""
		m.diff = NewDiffModel(oldLabel+": "+oldPath, newLabel+": "+file.Path,
			diff.SplitLines(oldText), diff.SplitLines(newText))
	}
	m.diff = m.diff.WithTheme(m.theme).withSize(m.diffWidth(), m.height)
}

func describeLoadError(err error) string {
	switch {
	case errors.Is(err, source.ErrBinary):
		return "Binary file - not shown"
	case errors.Is(err, source.ErrTooLarge):
		return "File too large - not shown"
	case errors.Is(err, source.ErrNotRegular):
		return "Not a regular file (directory, submodule or special file) - not shown"
	case errors.Is(err, source.ErrOutsideRepository):
		return "Path leaves the repository through a symbolic link - not shown"
	case errors.Is(err, source.ErrUnmerged):
		return "Unmerged file (conflict) - resolve it with the merge tool first"
	case errors.Is(err, source.ErrSubmodule):
		return "Submodule - commit change not shown"
	default:
		firstLine, _, _ := strings.Cut(err.Error(), "\n")
		return "Cannot show this file: " + firstLine
	}
}

func (m GitModel) listBodyHeight() int { return max(m.height-chromeLines, 1) }

func (m *GitModel) ensureVisible() {
	body := m.listBodyHeight()
	if m.selected < m.listTop {
		m.listTop = m.selected
	}
	if m.selected >= m.listTop+body {
		m.listTop = m.selected - body + 1
	}
	m.listTop = max(m.listTop, 0)
}

// Update implements tea.Model.
func (m GitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.diff = m.diff.withSize(m.diffWidth(), m.height)
		m.ensureVisible()
		return m, nil
	case tea.MouseMsg:
		return m.forwardToDiff(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m GitModel) forwardToDiff(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.diff.Update(msg)
	m.diff = updated.(DiffModel)
	return m, cmd
}

func (m GitModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		m.focus = 1 - m.focus
		return m, nil
	case "n", "p", "t":
		return m.forwardToDiff(msg)
	}
	if m.focus == focusDiff {
		switch key {
		case "esc", "left", "h":
			m.focus = focusList
			return m, nil
		}
		return m.forwardToDiff(msg)
	}
	switch key {
	case "esc":
		return m, tea.Quit
	case "enter", "right", "l":
		m.focus = focusDiff
	case "j", "down":
		m.selectFile(m.selected + 1)
	case "k", "up":
		m.selectFile(m.selected - 1)
	case "pgdown":
		m.selectFile(m.selected + m.listBodyHeight())
	case "pgup":
		m.selectFile(m.selected - m.listBodyHeight())
	case "g", "home":
		m.selectFile(0)
	case "G", "end":
		m.selectFile(len(m.files) - 1)
	}
	return m, nil
}

// View implements tea.Model.
func (m GitModel) View() string {
	if m.width == 0 {
		return ""
	}
	list := m.listView()
	right := m.diff.View()
	if m.message != "" {
		right = m.messageView()
	}
	divider := strings.TrimSuffix(strings.Repeat(m.theme.Gutter.Render(separator)+"\n", m.height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, divider, right)
}

func (m GitModel) listView() string {
	width := m.listWidth()
	lines := make([]string, 0, m.height)
	lines = append(lines, m.theme.Header.Render(padRight(fmt.Sprintf(" Changes (%d)", len(m.files)), width)))
	body := m.listBodyHeight()
	for row := 0; row < body; row++ {
		index := m.listTop + row
		if index >= len(m.files) {
			lines = append(lines, strings.Repeat(" ", width))
			continue
		}
		lines = append(lines, m.fileRow(index, width))
	}
	hint := " ↑↓ files  enter open  tab focus  q quit"
	lines = append(lines, m.theme.Footer.Render(padRight(hint, width)))
	return strings.Join(lines, "\n")
}

func (m GitModel) fileRow(index, width int) string {
	file := m.files[index]
	label := sanitize(file.Path)
	if file.OldPath != "" {
		label = sanitize(file.OldPath) + " → " + label
	}
	style := m.theme.Equal
	if index == m.selected {
		style = m.theme.SelectedDim
		if m.focus == focusList {
			style = m.theme.Selected
		}
	}
	status := m.theme.statusStyle(file.Status).Inherit(style).Render(" " + string(rune(file.Status)) + " ")
	return status + renderText(label, nil, max(width-3, 0), style, style)
}

// messageView draws the right pane for files that cannot be shown.
func (m GitModel) messageView() string {
	width := m.diffWidth()
	lines := make([]string, 0, m.height)
	lines = append(lines, m.theme.Header.Render(padRight(" "+sanitize(m.files[m.selected].Path), width)))
	plain := m.theme.Equal.UnsetBackground()
	for row := 0; row < m.listBodyHeight(); row++ {
		text := ""
		if row == 1 {
			text = "  " + m.message // renderText sanitizes control characters and cuts to the width
		}
		lines = append(lines, renderText(text, nil, width, plain, plain))
	}
	lines = append(lines, m.theme.Footer.Render(padRight("", width)))
	return strings.Join(lines, "\n")
}

// withSize returns the model resized to the given terminal area.
func (m DiffModel) withSize(width, height int) DiffModel {
	m.width, m.height = width, height
	m.clamp()
	return m
}
