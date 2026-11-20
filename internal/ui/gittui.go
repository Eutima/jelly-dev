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

	loading     bool // a file is being loaded in the background
	loadingFile int  // index of the file being loaded
	loadedFile  int  // index of the file whose diff is shown, -1 when none

	width, height int
}

// loadedMsg carries the result of a background file load.
type loadedMsg struct {
	index   int
	diff    DiffModel
	message string // set instead of a meaningful diff when the file cannot be shown
}

// NewGitModel builds the viewer; the diff of the first file is loaded in the background (see Init).
func NewGitModel(files []source.ChangedFile, src ChangeSource) GitModel {
	model := GitModel{files: files, source: src, theme: DarkTheme(), loadedFile: -1}
	if len(files) > 0 {
		model.loading, model.loadingFile = true, 0
	}
	return model
}

// FileCount returns the number of changed files.
func (m GitModel) FileCount() int { return len(m.files) }

// Init implements tea.Model.
func (m GitModel) Init() tea.Cmd {
	if !m.loading {
		return nil
	}
	return m.loadCmd(m.loadingFile)
}

func (m GitModel) listWidth() int {
	return min(max(m.width/listDivisor, minListWidth), maxListWidth, max(m.width/2, 0))
}

func (m GitModel) diffWidth() int { return max(m.width-m.listWidth()-separatorW, 0) }

// selectFile selects file i and starts loading its diff unless it is already shown or a load is running
// (the running load hands over to the latest selection when it finishes).
func (m *GitModel) selectFile(i int) tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	m.selected = min(max(i, 0), len(m.files)-1)
	m.ensureVisible()
	if m.loading || m.selected == m.loadedFile {
		return nil
	}
	m.loading, m.loadingFile = true, m.selected
	return m.loadCmd(m.selected)
}

// loadCmd loads file index and computes its diff off the UI goroutine.
func (m GitModel) loadCmd(index int) tea.Cmd {
	file := m.files[index]
	src, theme := m.source, m.theme
	oldLabel, newLabel := src.Labels()
	oldPath := file.Path
	if file.OldPath != "" {
		oldPath = file.OldPath
	}
	leftTitle, rightTitle := oldLabel+": "+oldPath, newLabel+": "+file.Path
	return func() tea.Msg {
		oldText, newText, err := src.Load(file)
		if err != nil {
			return loadedMsg{
				index: index, message: describeLoadError(err),
				diff: NewDiffModel(leftTitle, rightTitle, nil, nil).WithTheme(theme),
			}
		}
		return loadedMsg{
			index: index,
			diff: NewDiffModel(leftTitle, rightTitle, diff.SplitLines(oldText), diff.SplitLines(newText)).
				WithTheme(theme),
		}
	}
}

// applyLoaded shows a finished load, or starts loading the file selected in the meantime.
func (m *GitModel) applyLoaded(msg loadedMsg) tea.Cmd {
	m.loading = false
	if msg.index != m.selected {
		if m.selected == m.loadedFile {
			return nil // the user came back to the file that is already shown
		}
		m.loading, m.loadingFile = true, m.selected
		return m.loadCmd(m.selected)
	}
	m.loadedFile = msg.index
	m.message = msg.message
	m.diff = msg.diff.withSize(m.diffWidth(), m.height)
	return nil
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
	case loadedMsg:
		cmd := m.applyLoaded(msg)
		return m, cmd
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
	var cmd tea.Cmd
	switch key {
	case "esc":
		return m, tea.Quit
	case "enter", "right", "l":
		m.focus = focusDiff
	case "j", "down":
		cmd = m.selectFile(m.selected + 1)
	case "k", "up":
		cmd = m.selectFile(m.selected - 1)
	case "pgdown":
		cmd = m.selectFile(m.selected + m.listBodyHeight())
	case "pgup":
		cmd = m.selectFile(m.selected - m.listBodyHeight())
	case "g", "home":
		cmd = m.selectFile(0)
	case "G", "end":
		cmd = m.selectFile(len(m.files) - 1)
	}
	return m, cmd
}

// View implements tea.Model.
func (m GitModel) View() string {
	if m.width == 0 {
		return ""
	}
	list := m.listView()
	right := m.diff.View()
	if message := m.paneMessage(); message != "" {
		right = m.messageView(message)
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

// paneMessage returns the text that replaces the diff pane: a loading hint or the reason a file is not shown.
func (m GitModel) paneMessage() string {
	if len(m.files) == 0 {
		return ""
	}
	if m.loadedFile != m.selected {
		return "Loading " + m.files[m.selected].Path + " ..."
	}
	return m.message
}

// messageView draws the right pane for files that cannot be shown.
func (m GitModel) messageView(message string) string {
	width := m.diffWidth()
	lines := make([]string, 0, m.height)
	lines = append(lines, m.theme.Header.Render(padRight(" "+sanitize(m.files[m.selected].Path), width)))
	plain := m.theme.Equal.UnsetBackground()
	for row := 0; row < m.listBodyHeight(); row++ {
		text := ""
		if row == 1 {
			text = "  " + message // renderText sanitizes control characters and cuts to the width
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
