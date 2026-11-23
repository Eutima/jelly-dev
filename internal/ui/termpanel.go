package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eutima/jelly-dev/internal/keymap"

	"github.com/eutima/jelly-dev/internal/terminal"
)

const (
	minTermPanel    = 6           // header and five rows of the shell
	minEditorRows   = 4           // the editor keeps at least this many lines when the panel is open
	actionCloseTab  = "closeterm" // the buttons and tabs of the panel header
	actionNewTab    = "newterm"
	actionSelectTab = "tab:"
	leaveTermKey    = "ctrl+]"
)

// termUpdateMsg reports that the screen of a shell changed (or that it ended).
type termUpdateMsg struct{ session *terminal.Session }

// waitForTerminal waits for the next change of the shell's screen.
func waitForTerminal(session *terminal.Session) tea.Cmd {
	return func() tea.Msg {
		<-session.Updates()
		return termUpdateMsg{session: session}
	}
}

// activeTerm is the shell of the shown tab, nil when there is none.
func (m ProjectModel) activeTerm() *terminal.Session {
	if m.termActive < 0 || m.termActive >= len(m.terms) {
		return nil
	}
	return m.terms[m.termActive]
}

// termPanelHeight is the height of the terminal panel including its header line; 0 when it is hidden or does not fit.
func (m ProjectModel) termPanelHeight() int {
	if len(m.terms) == 0 || !m.termShown || m.dbg != nil { // the debug panel takes the place while a run is shown
		return 0
	}
	return m.panelSlot()
}

// panelSlot is the height of the panel under the editor, whichever one it holds.
func (m ProjectModel) panelSlot() int {
	height := min(max(m.bodyHeight()/3, minTermPanel), m.bodyHeight()-minEditorRows)
	if height < 2 {
		return 0
	}
	return height
}

// editorHeight is the height of the editor: the panes without the terminal panel.
func (m ProjectModel) editorHeight() int {
	return max(m.bodyHeight()-m.termPanelHeight()-m.debugPanelHeight(), 1)
}

// openTerminal is the terminal button and ctrl+t: it shows the panel (starting the first shell in the project when none
// runs) and gives it the focus; when the shell already has the focus it hides the panel again (the shells keep running).
func (m ProjectModel) openTerminal() (tea.Model, tea.Cmd) {
	if m.dbg != nil { // one panel under the editor: the debug panel holds the place until the run is stopped
		m.say("the debug panel is shown: stop debugging first ("+keys.Label(keymap.DebugStop)+")", true)
		return m, nil
	}
	if len(m.terms) > 0 && m.termShown && m.focus == focusTerminal {
		m.termShown, m.focus = false, focusProjectEdit
		return m.resizeEditor(), nil
	}
	if len(m.terms) > 0 {
		m.termShown, m.focus = true, focusTerminal
		return m.resizeEditor(), nil
	}
	return m.newTerminal()
}

// newTerminal starts another shell in the project in a new tab and shows it.
func (m ProjectModel) newTerminal() (tea.Model, tea.Cmd) {
	command := defaultTerminal
	if m.hooks.Terminal != nil {
		command = m.hooks.Terminal
	}
	cmd := command(m.root)
	if cmd == nil {
		m.say("E1: Cannot start the terminal", true)
		return m, nil
	}
	wasShown := m.termShown
	m.termShown = true
	panel := m.termPanelHeight()
	if len(m.terms) == 0 {
		panel = min(max(m.bodyHeight()/3, minTermPanel), m.bodyHeight()-minEditorRows) // the panel is not counted yet
	}
	m.termShown = wasShown
	session, err := terminal.Start(cmd, m.root, max(m.editorWidth(), 1), max(panel-1, 1))
	if err != nil {
		if errors.Is(err, terminal.ErrUnsupported) { // no pseudo terminal here: run the shell in place of the window instead
			return m.runExternal("terminal", command(m.root))
		}
		m.say("E1: "+err.Error(), true)
		return m, nil
	}
	m.terms = append(m.terms, session)
	m.termActive, m.termShown, m.focus = len(m.terms)-1, true, focusTerminal
	return m.resizeEditor(), waitForTerminal(session)
}

// closeTab ends the shell of tab i; with the last one the panel goes away.
func (m *ProjectModel) closeTermTab(i int) {
	if i < 0 || i >= len(m.terms) {
		return
	}
	m.terms[i].Close()
	m.terms = append(m.terms[:i:i], m.terms[i+1:]...)
	switch {
	case len(m.terms) == 0:
		m.termActive, m.termShown = 0, false
		if m.focus == focusTerminal {
			m.focus = focusProjectEdit
		}
	case m.termActive > i || m.termActive >= len(m.terms):
		m.termActive = max(min(m.termActive-1, len(m.terms)-1), 0)
	}
}

// closeTerminal ends all shells and removes the panel.
func (m *ProjectModel) closeTerminal() {
	for len(m.terms) > 0 {
		m.closeTermTab(len(m.terms) - 1)
	}
}

func (m ProjectModel) termUpdated(msg termUpdateMsg) (tea.Model, tea.Cmd) {
	for i, session := range m.terms {
		if session != msg.session {
			continue
		}
		if session.Done() {
			m.closeTermTab(i)
			return m.resizeEditor(), nil
		}
		return m, waitForTerminal(session)
	}
	return m, nil // an answer of a shell that was closed
}

// termPage is how many lines ctrl+pgup and ctrl+pgdn scroll: the rows of the screen of the shell (the panel without its header), less two lines
// that stay in view to keep the place.
func (m ProjectModel) termPage() int { return max(m.termPanelHeight()-1-2, 1) }

// terminalKey sends a key to the shell. The keys of the window itself: leaveTermKey gives the focus back to the editor,
// ctrl+t opens the tab list, alt+t hides the panel, ctrl+d closes the shown tab and alt+1 ... alt+9 choose one.
func (m ProjectModel) terminalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch {
	case keys.Is(keymap.Tabs, key) && key != "shift+tab": // shift+tab belongs to the shell (completion menus), the other keys open the list
		return m.openTabSwitcher()
	case keys.Is(keymap.Terminal, key):
		return m.openTerminal() // hides the panel
	case keys.Is(keymap.LeaveTerminal, key):
		m.focus = focusProjectEdit
		return m.resizeEditor(), nil
	case keys.Is(keymap.CloseTerminal, key):
		m.closeTermTab(m.termActive)
		return m.resizeEditor(), nil
	case len(key) == 5 && strings.HasPrefix(key, "alt+") && key[4] >= '1' && key[4] <= '9':
		if i := int(key[4] - '1'); i < len(m.terms) {
			m.termActive = i
		}
		return m, nil
	}
	session := m.activeTerm()
	if session == nil {
		return m, nil
	}
	m.termSel = nil
	switch key { // the history of the shell: a page back and forth (the wheel of the mouse scrolls as well)
	case "ctrl+pgup":
		session.ScrollBy(m.termPage())
		return m, nil
	case "ctrl+pgdown":
		session.ScrollBy(-m.termPage())
		return m, nil
	}
	session.ScrollToLive()
	if msg.Paste {
		session.Paste(string(msg.Runes))
		return m, nil
	}
	session.Write(keyBytes(msg))
	return m, nil
}

// clickTerminal handles a click in the panel: a tab, the buttons of the header, anywhere else gives it the focus.
func (m ProjectModel) clickTerminal(msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if updated, used := m.mouseInTerminal(msg, x, y); used {
		return updated, nil
	}
	if !leftPress(msg) {
		return m, nil
	}
	if y == 0 {
		if keys := m.termHeader().visibleKeysAt(x, m.editorWidth()); len(keys) > 0 {
			m.focus = focusTerminal
			switch {
			case keys[0] == actionNewTab:
				return m.newTerminal()
			case keys[0] == actionCloseTab:
				m.closeTermTab(m.termActive)
				return m.resizeEditor(), nil
			case strings.HasPrefix(keys[0], actionSelectTab):
				if i, err := strconv.Atoi(strings.TrimPrefix(keys[0], actionSelectTab)); err == nil && i < len(m.terms) {
					m.termActive = i
				}
			}
			return m, nil
		}
	}
	m.focus = focusTerminal
	return m, nil
}

func (m ProjectModel) termHeader() *bar {
	result := &bar{}
	style := m.theme.Header
	if m.focus == focusTerminal {
		style = m.theme.Selected.Bold(true)
	}
	result.add(barPart{focusMark(m.focus == focusTerminal) + "TERMINAL ", style})
	for i, session := range m.terms {
		tab := m.theme.Footer
		if i == m.termActive {
			tab = m.theme.Header
		}
		result.click([]string{actionSelectTab + strconv.Itoa(i)}, barPart{fmt.Sprintf(" %d:%s ", i+1, sanitize(session.Name())), tab})
	}
	if session := m.activeTerm(); session != nil {
		if offset, _ := session.Scrolled(); offset > 0 { // the view is in the history: say how far and how to get back
			result.add(barPart{fmt.Sprintf(" ↑ %d lines back (wheel or ctrl+pgdn, any key: live) ", offset), m.theme.Warning})
		}
	}
	result.add(barPart{" ", m.theme.Footer})
	result.click([]string{actionNewTab}, barPart{"[+]", m.theme.Footer})
	result.add(barPart{" ", m.theme.Footer})
	result.click([]string{actionCloseTab}, barPart{"[×]", m.theme.Footer})
	var hints []string
	for _, h := range []struct {
		command keymap.Command
		what    string
	}{{keymap.LeaveTerminal, "editor"}, {keymap.Tabs, "tabs"}, {keymap.Terminal, "hide"}, {keymap.CloseTerminal, "close"}} {
		if label := panelLabel(h.command); label != "" {
			hints = append(hints, label+" "+h.what)
		}
	}
	result.hint(barPart{"  " + strings.Join(hints, "  "), m.theme.Footer})
	return result
}

// editorColumn is the editor with the terminal panel under it.
func (m ProjectModel) editorColumn() string {
	if m.debugPanelHeight() > 0 {
		return m.debugColumn()
	}
	panel := m.termPanelHeight()
	session := m.activeTerm()
	if panel == 0 || session == nil {
		return m.withTabBar(m.dimEditor(m.editor.View()))
	}
	width := max(m.editorWidth(), 1)
	rows := session.Lines(m.focus == focusTerminal)
	if m.termSel != nil && m.termSel.moved {
		rows = m.highlightSelection(rows, session.Cells())
	}
	lines := []string{m.termHeader().render(width, m.theme.Footer)}
	for y := 0; y < panel-1; y++ {
		line := strings.Repeat(" ", width)
		if y < len(rows) {
			line = rows[y]
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(width).Render(line))
	}
	if m.focus != focusTerminal {
		dimLines(lines[1:]) // the rows of the shell, not the title
	}
	return m.withTabBar(m.dimEditor(m.editor.View())) + "\n" + strings.Join(lines, "\n")
}

// dimEditor draws the text of the editor faint when it does not have the focus; its first line (replaced by the tab bar) and its
// footer stay as they are.
func (m ProjectModel) dimEditor(view string) string {
	if m.focus == focusProjectEdit {
		return view
	}
	lines := strings.Split(view, "\n")
	if last := len(lines) - len(m.editor.footerRows()); last > 1 {
		dimLines(lines[1:last])
	}
	return strings.Join(lines, "\n")
}

// keyBytes is what a terminal sends to the shell for a key.
func keyBytes(msg tea.KeyMsg) []byte {
	var out []byte
	switch msg.Type {
	case tea.KeyRunes:
		out = []byte(string(msg.Runes))
	case tea.KeySpace:
		out = []byte{' '}
	case tea.KeyBackspace:
		out = []byte{0x7f}
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
	case tea.KeyUp:
		out = []byte("\x1b[A")
	case tea.KeyDown:
		out = []byte("\x1b[B")
	case tea.KeyRight:
		out = []byte("\x1b[C")
	case tea.KeyLeft:
		out = []byte("\x1b[D")
	case tea.KeyHome:
		out = []byte("\x1b[H")
	case tea.KeyEnd:
		out = []byte("\x1b[F")
	case tea.KeyPgUp:
		out = []byte("\x1b[5~")
	case tea.KeyPgDown:
		out = []byte("\x1b[6~")
	case tea.KeyDelete:
		out = []byte("\x1b[3~")
	case tea.KeyInsert:
		out = []byte("\x1b[2~")
	default:
		if msg.Type >= 0 && msg.Type < 32 || msg.Type == tea.KeyEscape { // control keys, tab, enter, esc
			out = []byte{byte(msg.Type)}
			if msg.Type == tea.KeyEnter {
				out = []byte{'\r'}
			}
		}
	}
	if msg.Alt && len(out) > 0 {
		out = append([]byte{0x1b}, out...)
	}
	return out
}

// panelLabel is the key of a command as the terminal panel can use it: shift+tab belongs to the shell there, so the first other key is shown.
func panelLabel(command keymap.Command) string {
	for _, key := range keys.Keys(command) {
		if key != "shift+tab" {
			return keymap.Pretty(key)
		}
	}
	return ""
}

// highlightSelection draws the rows the selection touches in plain text with the selected cells marked; the other rows keep
// their colours.
func (m ProjectModel) highlightSelection(rows []string, cells [][]string) []string {
	out := append([]string(nil), rows...)
	_, y0, _, y1 := m.termSel.bounds()
	for y := max(y0, 0); y <= y1 && y < len(out) && y < len(cells); y++ {
		var line strings.Builder
		for x, cell := range cells[y] {
			if m.termSel.covers(x, y) {
				line.WriteString(m.theme.Selected.Render(cell))
			} else {
				line.WriteString(cell)
			}
		}
		out[y] = line.String()
	}
	return out
}
