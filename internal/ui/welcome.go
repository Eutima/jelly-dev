package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-dev/internal/project"
)

// WelcomeOutcome tells what the welcome screen was left with.
type WelcomeOutcome struct {
	Path      string // the project to open; empty when the user just quit
	NewWindow bool   // the user wants it in a new window (only set when the screen is embedded; the screen opens the window itself otherwise)
}

// welcomeQuestion is a yes or no question about the folder that is being opened: whether to create it, or whether it opens in a
// new window (yes) or in this one (no).
type welcomeQuestion struct {
	create bool   // the folder does not exist: yes makes it
	direct bool   // the folder is meant for a new window (ctrl+w): after it exists there is nothing left to ask
	path   string // the folder
}

// WelcomeModel is the start screen: the recent projects, and a prompt to open another folder.
type WelcomeModel struct {
	theme         Theme
	width, height int
	entries       []project.Entry
	selected      int
	top           int
	question      *welcomeQuestion // the open question, nil when none
	typing        bool             // the path prompt is open
	input         []rune
	matches       []string // folders that complete the typed path
	matchSel      int      // the match that was chosen with tab or the arrows, -1 when none
	status        string
	warn          bool
	newWindow     func(dir string) error
	remove        func(path string)
	outcome       *WelcomeOutcome
	title         string // replaces "Recent projects" above the list
	embedded      bool   // shown inside another model: leaving it sets done instead of ending the program
	done          bool
}

// NewWelcomeModel builds the screen. newWindow opens a directory in a new terminal window and remove forgets a project; both may be nil.
func NewWelcomeModel(entries []project.Entry, newWindow func(string) error, remove func(string)) (WelcomeModel, *WelcomeOutcome) {
	outcome := &WelcomeOutcome{}
	return WelcomeModel{theme: DarkTheme(), entries: slices.Clone(entries), newWindow: newWindow, remove: remove, outcome: outcome}, outcome
}

// WithTheme returns the model with another theme.
func (m WelcomeModel) WithTheme(theme Theme) WelcomeModel {
	m.theme = theme
	return m
}

// WithEmbedded returns the screen for use inside another model: leaving it (with or without a choice) does not end
// the program but makes Done report true.
func (m WelcomeModel) WithEmbedded() WelcomeModel {
	m.embedded = true
	return m
}

// WithChoice returns the screen for choosing a project for something other than opening it (the notes): it has its own title, never asks
// where the project opens and does not start windows.
func (m WelcomeModel) WithChoice(title string) WelcomeModel {
	m.title = title
	m.newWindow = nil
	return m
}

// Done reports whether an embedded screen was left; the outcome tells whether a project was chosen.
func (m WelcomeModel) Done() bool { return m.done }

// finish leaves the screen.
func (m WelcomeModel) finish() (tea.Model, tea.Cmd) {
	if m.embedded {
		m.done = true
		return m, nil
	}
	return m, tea.Quit
}

// Init implements tea.Model.
func (m WelcomeModel) Init() tea.Cmd { return nil }

func (m WelcomeModel) bodyHeight() int {
	return max(m.height-welcomeTop-len(m.messageRows())-len(m.footerRows()), 1)
}

const welcomeTop = 3 // title, blank line, list header

// messageRows are the lines under the list: the prompt of the folder being typed, or the last message (it wraps instead of being
// cut off); there is always at least one.
func (m WelcomeModel) messageRows() []*bar {
	switch {
	case m.question != nil:
		return textRows(m.questionText(), m.width, footerMax, m.theme.Equal.Bold(true))
	case m.typing:
		text := "Open folder: " + sanitize(string(m.input)) + "█"
		if m.status != "" {
			text += "   ! " + sanitize(m.status)
		}
		return textRows(text, m.width, footerMax, m.theme.Equal)
	case m.status != "" && m.warn:
		return textRows(sanitize(m.status), m.width, footerMax, m.theme.Warning)
	case m.status != "":
		return textRows(sanitize(m.status), m.width, footerMax, m.theme.Equal)
	}
	return []*bar{{}}
}

// footerRows are the hints as wrapped bars.
func (m WelcomeModel) footerRows() []*bar {
	if m.question != nil {
		return m.questionHintBar().wrap(m.width, footerMax)
	}
	if m.typing {
		return m.promptHintBar().wrap(m.width, footerMax)
	}
	return m.hintBar().wrap(m.width, footerMax)
}

// promptHintBar is the hint line while a folder is typed (its keys go to the prompt, so it is not clickable).
func (m WelcomeModel) promptHintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	for _, h := range []string{"[tab] complete", "[↑↓] choose", "[enter] open", "[ctrl+w] new window", "[esc] back"} {
		result.hint(barPart{h + "  ", m.theme.Footer})
	}
	return result
}

// Update implements tea.Model.
func (m WelcomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepVisible()
	case tea.KeyMsg:
		if m.question != nil {
			return m.handleQuestionKey(msg)
		}
		if m.typing {
			return m.handlePromptKey(msg)
		}
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m *WelcomeModel) keepVisible() {
	m.selected = min(max(m.selected, 0), max(len(m.entries)-1, 0))
	body := m.bodyHeight()
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+body {
		m.top = m.selected - body + 1
	}
}

func (m WelcomeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status, m.warn = "", false
	switch msg.String() {
	case "q", "esc", "ctrl+c", "ctrl+d":
		return m.finish()
	case "j", "down":
		m.selected++
	case "k", "up":
		m.selected--
	case "g", "home":
		m.selected = 0
	case "G", "end":
		m.selected = len(m.entries) - 1
	case "enter":
		return m.choose()
	case "w":
		if m.title != "" { // choosing a project for something else: nothing opens here
			return m, nil
		}
		return m.openInWindow()
	case "d", "delete":
		if m.selected < len(m.entries) {
			if m.remove != nil {
				m.remove(m.entries[m.selected].Path)
			}
			m.entries = slices.Delete(slices.Clone(m.entries), m.selected, m.selected+1)
			m.status = "Removed from the list (the folder is untouched)"
		}
	case "o", "n", ":":
		m.typing, m.input, m.matchSel = true, nil, -1
		m.matches = folderMatches("")
	}
	m.keepVisible()
	return m, nil
}

func (m WelcomeModel) choose() (tea.Model, tea.Cmd) {
	if m.selected >= len(m.entries) {
		return m, nil
	}
	return m.open(m.entries[m.selected].Path)
}

// open asks where a project opens: in a new window or in this one. Without a way to start windows there is nothing to ask.
func (m WelcomeModel) open(path string) (tea.Model, tea.Cmd) {
	if m.newWindow == nil {
		m.outcome.Path = path
		return m.finish()
	}
	m.question = &welcomeQuestion{path: path}
	return m, nil
}

// openIn carries out the answer: the project opens in a new window or in this one.
func (m WelcomeModel) openIn(path string, inNewWindow bool) (tea.Model, tea.Cmd) {
	switch {
	case !inNewWindow:
		m.outcome.Path = path
		return m.finish()
	case m.embedded: // the project window starts the new window
		m.outcome.Path, m.outcome.NewWindow = path, true
		return m.finish()
	}
	return m.launch(path)
}

func (m WelcomeModel) questionText() string {
	name := sanitize(m.question.path)
	if m.question.create {
		return "Create this folder?  " + name + " does not exist"
	}
	return "Open " + sanitize(filepath.Base(m.question.path)) + " in a new window? (no: in this window)"
}

// questionHintBar is the hint line while a question is open.
func (m WelcomeModel) questionHintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	yes, no := "new window", "this window"
	if m.question.create {
		yes, no = "create", "no"
	}
	result.command("y", yes, true, m.theme)
	result.command("n", no, true, m.theme)
	result.command("esc", "cancel", true, m.theme)
	return result
}

// handleQuestionKey answers the open question: y or enter say yes, n says no, esc cancels (back to the prompt or the list).
func (m WelcomeModel) handleQuestionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	question := *m.question
	if msg.String() == "enter" && question.create {
		return m, nil // creating a folder takes an explicit y: an enter that is pressed twice must not make a folder of a typo
	}
	m.question = nil
	switch msg.String() {
	case "y", "Y", "enter":
		if question.create {
			if err := os.MkdirAll(question.path, 0o755); err != nil { // #nosec G301 -- a project folder of the user's own
				m.status, m.warn = "Cannot create "+question.path+": "+err.Error(), true
				return m, nil
			}
			if question.direct {
				m.typing = false
				return m.openIn(question.path, true)
			}
			return m.open(question.path)
		}
		m.typing = false
		return m.openIn(question.path, true)
	case "n", "N":
		if question.create {
			return m, nil // keep typing
		}
		m.typing = false
		return m.openIn(question.path, false)
	}
	return m, nil // esc and every other key: back
}

func (m WelcomeModel) openInWindow() (tea.Model, tea.Cmd) {
	if m.selected >= len(m.entries) {
		return m, nil
	}
	return m.launch(m.entries[m.selected].Path)
}

func (m WelcomeModel) launch(dir string) (tea.Model, tea.Cmd) {
	switch {
	case m.newWindow == nil:
		m.status, m.warn = "Opening windows is not available here", true
	default:
		if err := m.newWindow(dir); err != nil {
			m.status, m.warn = err.Error(), true
		} else {
			m.status = "Opened " + dir + " in a new window"
		}
	}
	return m, nil
}

// handlePromptKey edits the path of the folder to open: enter opens it (after asking where), ctrl+w in a new window.
func (m WelcomeModel) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status, m.warn = "", false
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.typing = false
	case tea.KeyTab, tea.KeyShiftTab:
		m.complete(msg.Type == tea.KeyShiftTab)
	case tea.KeyDown, tea.KeyUp:
		m.moveMatch(map[tea.KeyType]int{tea.KeyDown: 1, tea.KeyUp: -1}[msg.Type])
	case tea.KeyEnter, tea.KeyCtrlW:
		path := expandHome(strings.TrimSpace(string(m.input)))
		abs, err := filepath.Abs(path)
		if path == "" || err != nil {
			m.status, m.warn = fmt.Sprintf("E344: Can't find directory \"%s\"", path), true
			return m, nil
		}
		_, statErr := os.Stat(abs)
		switch {
		case os.IsNotExist(statErr): // a folder that does not exist yet can be made, when it can be opened afterwards
			if msg.Type == tea.KeyCtrlW && m.newWindow == nil {
				m.status, m.warn = "Opening windows is not available here", true
				return m, nil
			}
			m.question = &welcomeQuestion{create: true, direct: msg.Type == tea.KeyCtrlW, path: abs}
			return m, nil
		case statErr != nil:
			m.status, m.warn = fmt.Sprintf("E344: %v", statErr), true
			return m, nil
		case !isDir(abs):
			m.status, m.warn = fmt.Sprintf("E344: \"%s\" is not a directory", path), true
			return m, nil
		}
		if msg.Type == tea.KeyCtrlW {
			m.typing = false
			return m.launch(abs)
		}
		return m.open(abs) // the prompt stays until the question is answered: esc comes back to it
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			m.retype()
		}
	case tea.KeyCtrlU:
		m.input = nil
		m.retype()
	case tea.KeySpace:
		m.input = append(m.input, ' ')
		m.retype()
	case tea.KeyRunes:
		m.input = append(m.input, []rune(sanitizeEditable(string(msg.Runes)))...)
		m.retype()
	}
	return m, nil
}

// retype recomputes the matches after the user changed the typed path.
func (m *WelcomeModel) retype() {
	m.matches, m.matchSel = folderMatches(string(m.input)), -1
	m.top = 0
}

// complete is the tab key: the first press completes the path as far as all matches agree (the whole folder when
// there is only one), the next presses walk through the matches; shift+tab walks backwards.
func (m *WelcomeModel) complete(backwards bool) {
	switch {
	case len(m.matches) == 0:
		m.status, m.warn = "No folder matches", true
	case m.matchSel < 0 && !backwards:
		if common := commonPrefix(m.matches); len([]rune(common)) > len(m.input) {
			m.input = []rune(common)
			if len(m.matches) == 1 { // done: continue inside the folder
				m.retype()
			}
			return
		}
		m.moveMatch(1)
	default:
		m.moveMatch(map[bool]int{false: 1, true: -1}[backwards])
	}
}

// moveMatch selects another match and writes it into the prompt; the list stays as it is while walking through it.
func (m *WelcomeModel) moveMatch(step int) {
	if len(m.matches) == 0 {
		return
	}
	if m.matchSel < 0 && step < 0 {
		m.matchSel = 0
	}
	m.matchSel = (m.matchSel + step + len(m.matches)) % len(m.matches)
	m.input = []rune(m.matches[m.matchSel])
	body := m.bodyHeight()
	if m.matchSel < m.top {
		m.top = m.matchSel
	}
	if m.matchSel >= m.top+body {
		m.top = m.matchSel - body + 1
	}
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func (m WelcomeModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.top = max(m.top-wheelLines, 0)
	case msg.Button == tea.MouseButtonWheelDown:
		m.top = min(m.top+wheelLines, max(len(m.entries)-m.bodyHeight(), 0))
	case leftPress(msg):
		if msg.Y >= m.height-len(m.footerRows()) {
			if keys := rowKeysAt(m.footerRows(), msg.X, msg.Y, m.width, m.height); keys != nil {
				return replayKeys(m, keys)
			}
			return m, nil
		}
		if m.question != nil {
			return m, nil
		}
		if row := m.top + msg.Y - welcomeTop; msg.Y >= welcomeTop && msg.Y < welcomeTop+m.bodyHeight() && row < len(m.entries) {
			m.selected = row
			return m.choose()
		}
	}
	return m, nil
}

// replayKeys sends keys to the welcome model one after the other (a click on a hint runs its key).
func replayKeys(m WelcomeModel, keys []string) (tea.Model, tea.Cmd) {
	return replay(m, keys)
}

func (m WelcomeModel) hintBar() *bar {
	result := &bar{}
	result.add(barPart{" ", m.theme.Footer})
	hints := [][2]string{{"enter", "open"}, {"w", "new window"}, {"o", "open folder"}, {"d", "forget"}, {"q", "quit"}}
	if m.embedded {
		hints = [][2]string{{"enter", "open"}, {"o", "open folder"}, {"d", "forget"}, {"q", "close"}}
	}
	if m.title != "" {
		hints[0][1] = "choose"
	}
	for _, h := range hints {
		result.command(h[0], h[1], len(m.entries) > 0 || h[0] == "o" || h[0] == "q", m.theme)
	}
	return result
}

// View implements tea.Model.
func (m WelcomeModel) View() string {
	if m.width == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, m.theme.Header.Render(padRight(" Jelly Dev", m.width)), strings.Repeat(" ", m.width))
	if m.typing {
		return m.promptView(lines)
	}
	lines = append(lines, m.theme.Header.Render(padRight(m.heading(), m.width)))
	for y := 0; y < m.bodyHeight(); y++ {
		i := m.top + y
		switch {
		case i < len(m.entries):
			lines = append(lines, m.entryRow(i))
		case i == 0 && len(m.entries) == 0:
			lines = append(lines, m.theme.Equal.Faint(true).Render(padRight(" No projects yet: press o and type a folder, or run jd <folder>", m.width)))
		default:
			lines = append(lines, strings.Repeat(" ", m.width))
		}
	}
	return m.withBottom(lines)
}

// promptView is the screen while a folder is typed: the matching folders replace the list of projects.
func (m WelcomeModel) promptView(lines []string) string {
	lines = append(lines, m.theme.Header.Render(padRight(" Folders ("+fmt.Sprint(len(m.matches))+")", m.width)))
	for y := 0; y < m.bodyHeight(); y++ {
		i := m.top + y
		switch {
		case i < len(m.matches):
			style := m.theme.Equal
			if i == m.matchSel {
				style = m.theme.Selected
			}
			lines = append(lines, style.Render(padRight(" "+sanitize(m.matches[i]), m.width)))
		case i == 0:
			lines = append(lines, m.theme.Equal.Faint(true).Render(padRight(" No folder matches", m.width)))
		default:
			lines = append(lines, strings.Repeat(" ", m.width))
		}
	}
	return m.withBottom(lines)
}

// withBottom adds the message lines and the hints under the list.
func (m WelcomeModel) withBottom(lines []string) string {
	lines = append(lines, renderRowLines(m.messageRows(), m.width, m.theme.Equal)...)
	lines = append(lines, renderRowLines(m.footerRows(), m.width, m.theme.Footer)...)
	return strings.Join(lines, "\n")
}

func (m WelcomeModel) entryRow(i int) string {
	entry := m.entries[i]
	style := m.theme.Equal
	if i == m.selected {
		style = m.theme.Selected
	}
	name := sanitize(filepath.Base(entry.Path))
	age := relativeTime(entry.LastOpened)
	room := max(m.width-runewidth.StringWidth(name)-runewidth.StringWidth(age)-6, 4)
	path := runewidth.Truncate(sanitize(entry.Path), room, "…")
	left := fmt.Sprintf(" %s  %s", name, path)
	gap := max(m.width-runewidth.StringWidth(left)-runewidth.StringWidth(age)-1, 1)
	return style.Render(padRight(left+strings.Repeat(" ", gap)+age+" ", m.width))
}

// relativeTime says how long ago a project was opened.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%d min ago", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(age.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(age.Hours()/24))
}

// heading is the title above the list of projects.
func (m WelcomeModel) heading() string {
	if m.title != "" {
		return " " + m.title
	}
	return " Recent projects"
}
