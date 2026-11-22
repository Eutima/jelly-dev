package ui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/nanokeys"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// The editor style in force: every editor made from now on has it. It is vim until main installs the choice of the user, so the editors
// of the tests are the vim ones.
var editorStyleHolder = struct {
	mu    sync.RWMutex
	style config.EditorStyle
}{style: config.EditorVim}

// UseEditorStyle installs the editor style (nano or vim) that the editors of the project window and of the merge and diff views get.
func UseEditorStyle(style config.EditorStyle) {
	editorStyleHolder.mu.Lock()
	editorStyleHolder.style = style
	editorStyleHolder.mu.Unlock()
}

// CurrentEditorStyle returns the editor style in force.
func CurrentEditorStyle() config.EditorStyle {
	editorStyleHolder.mu.RLock()
	defer editorStyleHolder.mu.RUnlock()
	return editorStyleHolder.style
}

// nanoSession is a session of the nano-style key handler that follows the key bindings in force.
func nanoSession() *nanokeys.Session {
	return nanokeys.New(func(key string) keymap.Command {
		command, _ := keys.Lookup(keymap.ScopeNano, key)
		return command
	})
}

// nanoHintBar appends the footer hints of the nano-style editor: the keys that are bound now, each one clickable.
func nanoHintBar(result *bar, theme Theme) {
	result.binding(keymap.Save, "Save", true, theme)
	for _, h := range nanokeys.HintCommands {
		result.binding(h.Command, h.Text, true, theme)
	}
}

// nanoPromptRow is the line that shows the question of the nano handler: "Search: text█".
func nanoPromptRow(session *nanokeys.Session) (string, bool) {
	label, input, open := session.Prompt()
	if !open {
		return "", false
	}
	if input == "" && strings.HasSuffix(label, ")") {
		return " " + label, true // a question that is answered with a key: no input line
	}
	return " " + label + ": " + input + "█", true
}

// useNano gives the buffer to the nano handler.
func useNano(buffer *vimbuf.Buffer) *nanokeys.Session {
	buffer.EnterModeless()
	return nanoSession()
}

// handleNanoKey handles a key press of the nano-style editor: the popup of the completion, the keys of the project window, then the nano keys.
func (m EditorModel) handleNanoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	_, _, asking := m.nano.Prompt()
	if asking && msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) > 1 { // pasted text goes into the question
		m.nano.Paste(string(msg.Runes))
		return m, nil
	}
	if !asking {
		if m.completion != nil {
			if model, used := m.completionKey(key); used {
				return model, nil
			}
		}
		if isCompletionKey(key) {
			return m.startCompletion()
		}
		if model, cmd, used := m.navigationKey(key); used {
			return model, cmd
		}
		if key == "ctrl+d" && m.forwardEx { // in the project window the window asks before it closes the tab
			return m, func() tea.Msg { return CloseRequestMsg{} }
		}
		if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) > 1 { // text that arrived at once
			m.quitArmed = false
			m.vim.TypeText(string(msg.Runes))
			m.scrollToCursor()
			return m, nil
		}
	}
	m.quitArmed = false
	switch m.nano.Handle(m.vim, key).Result {
	case vimbuf.Write:
		m.save("")
	case vimbuf.WriteQuit:
		if m.save("") {
			m.markQuit()
			return m, tea.Quit
		}
	case vimbuf.Quit, vimbuf.ForceQuit:
		m.markQuit()
		return m, tea.Quit
	}
	if text := m.nano.Status(); text != "" {
		m.notice(text, false)
	} else if text := m.vim.Message(); text != "" {
		m.notice(text, strings.HasPrefix(text, "E") || strings.HasPrefix(text, "search hit"))
	}
	if m.completion != nil {
		m.refilterCompletion()
	}
	m.scrollToCursor()
	return m, nil
}

// nanoNavigationKey is navigationKey for the nano style: the keys of the project window that open its search, project list and tab list, and the
// way back after a jump.
func (m EditorModel) nanoNavigationKey(key string) (EditorModel, tea.Cmd, bool) {
	switch {
	case m.forwardEx && forwardedCommand(key) != "":
		command := forwardedCommand(key)
		return m, func() tea.Msg { return ExCommandMsg{Command: command} }, true
	case keys.Is(keymap.Back, key):
		model, cmd := m.jumpBack()
		return model, cmd, true
	}
	return m, nil, false
}

// editorCommand handles :editor [nano|vim]: it shows the style, or changes it for every open file and for the next start.
func (m ProjectModel) editorCommand(argument string) (tea.Model, tea.Cmd) {
	if argument == "" {
		m.say("Editor: "+string(CurrentEditorStyle())+" style (:editor nano or :editor vim changes it)", false)
		return m, nil
	}
	style, err := config.ParseEditorStyle(argument)
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	UseEditorStyle(style)
	all := m.openEditors()
	for i := range all {
		all[i] = all[i].WithStyle(style)
	}
	m.files = all
	if m.tab >= 0 && m.tab < len(all) {
		m.editor = all[m.tab]
	}
	if m.hooks.SetEditor != nil {
		m.hooks.SetEditor(style)
	}
	m.say("Editor: "+string(style)+" style", false)
	return m, nil
}
