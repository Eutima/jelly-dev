package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
)

// languagePick is the open list of languages (:addlang).
type languagePick struct {
	picker PickerModel
	states []tools.LanguageState
}

// languageAddedMsg reports that adding a language ended.
type languageAddedMsg struct {
	name    string
	results []tools.InstallResult
	err     error
}

// openLanguagePicker shows the searchable list of all languages with their language server and formatter.
func (m ProjectModel) openLanguagePicker() (tea.Model, tea.Cmd) {
	switch {
	case m.hooks.Tools == nil:
		m.say("E1: Tool installation is not available here", true)
		return m, nil
	case m.toolRun != nil || m.toolStatus != "":
		m.say("Tools are being installed already: "+m.toolStatus, false)
		return m, nil
	}
	states := m.hooks.Tools.LanguageStates()
	items := make([]PickItem, len(states))
	for i, s := range states {
		detail := strings.Join(s.Tools, ", ")
		switch {
		case len(s.Missing) == 0:
			detail += "  (installed)"
		case len(s.Missing) < len(s.Tools):
			detail += "  (missing: " + strings.Join(s.Missing, ", ") + ")"
		}
		items[i] = PickItem{Label: s.Name, Detail: detail, Dim: len(s.Missing) == 0}
	}
	m.langs = &languagePick{picker: newPicker(m.theme, "Add a language: its language server and formatter are installed", items), states: states}
	return m, nil
}

// updateLanguagePicker passes a key or click to the open list; a chosen language is added and its tools installed.
func (m ProjectModel) updateLanguagePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	picker := m.langs.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	switch outcome {
	case pickNone:
		pick := *m.langs
		pick.picker = picker
		m.langs = &pick
		return m, nil
	case pickCancelled:
		m.langs = nil
		return m, nil
	}
	index := picker.Chosen()
	if index < 0 || index >= len(m.langs.states) {
		m.langs = nil
		return m, nil
	}
	state := m.langs.states[index]
	m.langs = nil
	installer := m.hooks.Tools
	if len(state.Missing) == 0 {
		m.toolStatus = fmt.Sprintf("Adding %s...", state.Name)
	} else {
		m.toolStatus = fmt.Sprintf("Installing %s for %s...", strings.Join(state.Missing, " and "), state.Name)
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), toolInstallTimeout)
		defer cancel()
		results, err := installer.AddLanguage(ctx, state.ID)
		return languageAddedMsg{name: state.Name, results: results, err: err}
	}
}

// languageAdded tells what came of adding a language.
func (m ProjectModel) languageAdded(msg languageAddedMsg) (tea.Model, tea.Cmd) {
	m.toolStatus = ""
	if msg.err != nil {
		m.say(fmt.Sprintf("%s: %v", msg.name, msg.err), true)
		return m, nil
	}
	var installed, failed []string
	runtimeMissing := false
	for _, r := range msg.results {
		var missing *tools.MissingRuntimeError
		switch {
		case r.Err != nil:
			failed = append(failed, r.Tool+" ("+shorten(r.Err.Error(), 60)+")")
			if errors.As(r.Err, &missing) {
				runtimeMissing = true
			}
		case !r.Already:
			installed = append(installed, r.Tool)
		}
	}
	switch {
	case len(failed) > 0:
		text := fmt.Sprintf("%s: could not install %s", msg.name, strings.Join(failed, "; "))
		if runtimeMissing {
			text += " - run jd setup to install the runtimes"
		}
		m.say(text, true)
	case len(installed) > 0:
		m.say(fmt.Sprintf("%s added: installed %s", msg.name, strings.Join(installed, " and ")), false)
	default:
		m.say(fmt.Sprintf("%s added: its language server and formatter are installed already", msg.name), false)
	}
	return m, nil
}
