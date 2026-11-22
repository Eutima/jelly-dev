package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
)

// EditorChoiceOutcome tells what the editor question was left with.
type EditorChoiceOutcome struct {
	Style    config.EditorStyle
	Answered bool // an entry was chosen
	Aborted  bool // esc, q, ctrl+c or ctrl+d: "not now": nothing is saved, the question comes again
}

// editorChoices are the entries of the question, in the order of config.EditorStyles.
var editorChoices = map[config.EditorStyle][2]string{
	config.EditorNano: {"Nano-style", "beginner-friendly terminal text editor: just type, the keys are shown at the bottom"},
	config.EditorVim:  {"Vim-style", "more advanced editor with different modes and keyboard commands"},
}

// EditorChoiceModel asks which editor style to use: nano-style or vim-style. It is shown once, in the first-start setup, and again by
// jd setup.
type EditorChoiceModel struct {
	theme         Theme
	styles        []config.EditorStyle
	sel           int
	outcome       *EditorChoiceOutcome
	width, height int
	done          bool
}

// NewEditorChoiceModel builds the question; current is the entry that starts selected.
func NewEditorChoiceModel(current config.EditorStyle) (EditorChoiceModel, *EditorChoiceOutcome) {
	outcome := &EditorChoiceOutcome{}
	m := EditorChoiceModel{theme: DarkTheme(), styles: config.EditorStyles(), outcome: outcome}
	for i, style := range m.styles {
		if style == current {
			m.sel = i
		}
	}
	return m, outcome
}

// WithTheme returns the model with another theme.
func (m EditorChoiceModel) WithTheme(theme Theme) EditorChoiceModel {
	m.theme = theme
	return m
}

// Init implements tea.Model.
func (m EditorChoiceModel) Init() tea.Cmd { return nil }

const editorChoiceTop = 3 // title, explanation and an empty line come before the first entry

// Update implements tea.Model.
func (m EditorChoiceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		if leftPress(msg) {
			if keys := m.hintBar().visibleKeysAt(msg.X, m.width); msg.Y == m.height-1 && keys != nil {
				return replay(m, keys)
			}
			if row := msg.Y - editorChoiceTop; row >= 0 && row < len(m.styles) {
				m.sel = row
				return m.finish()
			}
		}
	}
	return m, nil
}

func (m EditorChoiceModel) finish() (tea.Model, tea.Cmd) {
	m.outcome.Style, m.outcome.Answered = m.styles[m.sel], true
	m.done = true
	return m, tea.Quit
}

func (m EditorChoiceModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k", "ctrl+p":
		m.sel = max(m.sel-1, 0)
	case "down", "j", "ctrl+n":
		m.sel = min(m.sel+1, len(m.styles)-1)
	case "1", "n":
		m.sel = 0
	case "2", "v":
		m.sel = 1
	case "enter":
		return m.finish()
	case "esc", "q", "ctrl+c", "ctrl+d": // not now: nothing is saved, the question comes again
		m.outcome.Aborted = true
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m EditorChoiceModel) View() string {
	if m.width == 0 || m.done {
		return ""
	}
	lines := []string{
		m.theme.Header.Render(padRight(" Jelly Dev: which editor do you want?", m.width)),
		m.theme.Equal.Render(padRight(" The text editor of the project window and of the merge and diff views. You can change it later with :editor.", m.width)),
		strings.Repeat(" ", m.width),
	}
	for i, style := range m.styles {
		mark := "( )"
		if i == m.sel {
			mark = "(*)"
		}
		text := " " + mark + " " + editorChoices[style][0] + "  " + editorChoices[style][1]
		line := m.theme.Equal
		if i == m.sel {
			line = m.theme.Selected
		}
		lines = append(lines, line.Render(padRight(text, m.width)))
	}
	for len(lines) < m.height-1 {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	lines = append(lines, m.hintBar().render(m.width, m.theme.Footer))
	return strings.Join(lines[:min(len(lines), max(m.height, 1))], "\n")
}

func (m EditorChoiceModel) hintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range [][2]string{{"enter", "choose"}, {"↑↓", "select"}, {"esc", "not now"}} {
		result.command(h[0], h[1], true, m.theme)
	}
	return result
}
