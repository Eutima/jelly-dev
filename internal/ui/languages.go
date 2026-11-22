package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
)

// LanguagesOutcome tells what the language question was left with.
type LanguagesOutcome struct {
	Chosen   []string // language ids, in the order of the list
	Skipped  bool     // the question was dismissed: nothing is installed now
	Answered bool     // the user answered (or dismissed) the question; false when the program ended without it
	Aborted  bool     // ctrl+c, ctrl+d or q: "not now": nothing is chosen, installed or remembered, the question comes again
}

// LanguagesModel asks which languages to install language servers and formatters for: a list of checkboxes, the defaults
// checked. It is shown once, when jd starts for the first time.
type LanguagesModel struct {
	theme         Theme
	languages     []tools.Language
	checked       map[string]bool
	sel           int
	outcome       *LanguagesOutcome
	width, height int
	done          bool
}

// NewLanguagesModel builds the question; checked are the ids that start checked.
func NewLanguagesModel(languages []tools.Language, checked []string) (LanguagesModel, *LanguagesOutcome) {
	outcome := &LanguagesOutcome{}
	m := LanguagesModel{theme: DarkTheme(), languages: languages, checked: map[string]bool{}, outcome: outcome}
	for _, id := range checked {
		m.checked[id] = true
	}
	return m, outcome
}

// WithTheme returns the model with another theme.
func (m LanguagesModel) WithTheme(theme Theme) LanguagesModel {
	m.theme = theme
	return m
}

// Init implements tea.Model.
func (m LanguagesModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m LanguagesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if row := msg.Y - languagesTop; row >= 0 && row < len(m.languages) {
				m.sel = row
				m.toggle()
			}
		}
	}
	return m, nil
}

const languagesTop = 3 // title, explanation and an empty line come before the first entry

func (m *LanguagesModel) toggle() {
	id := m.languages[m.sel].ID
	m.checked[id] = !m.checked[id]
}

func (m LanguagesModel) finish(skipped bool) (tea.Model, tea.Cmd) {
	m.outcome.Answered, m.outcome.Skipped = true, skipped
	m.outcome.Chosen = nil
	if !skipped {
		m.outcome.Chosen = m.checkedIDs()
	}
	m.done = true
	return m, tea.Quit
}

func (m LanguagesModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k", "ctrl+p":
		m.sel = max(m.sel-1, 0)
	case "down", "j", "ctrl+n":
		m.sel = min(m.sel+1, len(m.languages)-1)
	case "home", "g":
		m.sel = 0
	case "end", "G":
		m.sel = len(m.languages) - 1
	case " ", "x":
		m.toggle()
	case "a":
		for _, l := range m.languages {
			if !l.OptIn { // like "all": the opt-in languages (C#, PowerShell) are left as they are, they are checked one by one
				m.checked[l.ID] = true
			}
		}
	case "n":
		clear(m.checked)
	case "enter":
		return m.finish(false)
	case "esc":
		return m.finish(true)
	case "q", "ctrl+c", "ctrl+d": // not now: nothing is saved, the question comes again at the next start
		m.outcome.Aborted = true
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m LanguagesModel) View() string {
	if m.width == 0 || m.done {
		return ""
	}
	lines := []string{
		m.theme.Header.Render(padRight(" Jelly Dev: which languages do you work with?", m.width)),
		m.theme.Equal.Render(padRight(" Check the languages: jd installs the language server and the formatter of each.", m.width)),
		strings.Repeat(" ", m.width),
	}
	for i, l := range m.languages {
		mark := "[ ]"
		if m.checked[l.ID] {
			mark = "[x]"
		}
		text := fmt.Sprintf(" %s %-26s %s", mark, l.Name, strings.Join(l.Tools, ", "))
		style := m.theme.Equal
		if i == m.sel {
			style = m.theme.Selected
		}
		lines = append(lines, style.Render(padRight(text, m.width)))
	}
	for len(lines) < m.height-2 {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	lines = append(lines, m.theme.Equal.Render(padRight(" Later: jd install-lsp <language>... installs more.", m.width)), m.hintBar().render(m.width, m.theme.Footer))
	return strings.Join(lines[:min(len(lines), max(m.height, 1))], "\n")
}

func (m LanguagesModel) hintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	enter := "install"
	if len(m.checkedIDs()) == 0 {
		enter = "continue without tools" // nothing is checked
	}
	for _, h := range [][2]string{{"enter", enter}, {"space", "toggle"}, {"a", "all"}, {"n", "none"}, {"esc", "no languages"}, {"ctrl+c", "not now"}} {
		result.command(h[0], h[1], true, m.theme)
	}
	return result
}

// checkedIDs returns the ids that are checked now.
func (m LanguagesModel) checkedIDs() []string {
	var ids []string
	for _, l := range m.languages {
		if m.checked[l.ID] {
			ids = append(ids, l.ID)
		}
	}
	return ids
}
