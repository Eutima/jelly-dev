package ui

import (
	"context"
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/ai"
	"github.com/eutima/jelly-dev/internal/merge"
)

// aiDoneMsg is the answer of an assistant request.
type aiDoneMsg struct {
	name   string
	result map[int][]string // resolved lines per chunk index
	err    error
}

// WithAssistants returns the model with assistants that can resolve conflicts; each starts with its own key.
func (m MergeModel) WithAssistants(assistants ...ai.Assistant) MergeModel {
	m.assistants = assistants
	return m
}

// assistantFor returns the assistant started by key.
func (m MergeModel) assistantFor(key string) (ai.Assistant, bool) {
	for _, assistant := range m.assistants {
		if assistant.Key == key {
			return assistant, true
		}
	}
	return ai.Assistant{}, false
}

// askAssistant sends all unresolved conflicts to the assistant; the answer arrives as aiDoneMsg.
func (m MergeModel) askAssistant(assistant ai.Assistant) (tea.Model, tea.Cmd) {
	if m.aiBusy {
		m.notice("Still waiting for the assistant", true)
		return m, nil
	}
	request := ai.Request{File: m.titles.Name}
	for _, chunk := range m.changed {
		if m.chunks[chunk].Kind == merge.Conflict && m.choices[chunk] == ChoiceAuto {
			c := m.chunks[chunk]
			request.Conflicts = append(request.Conflicts, ai.Conflict{ID: chunk, Base: c.Base, Ours: c.Ours, Theirs: c.Theirs})
		}
	}
	if len(request.Conflicts) == 0 {
		m.notice("No unresolved conflicts left", true)
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.aiBusy, m.aiCancel = true, cancel
	m.notice(m.waitingNotice(assistant.Name, len(request.Conflicts)), false)
	return m, func() tea.Msg {
		result, err := assistant.Resolve(ctx, request)
		return aiDoneMsg{name: assistant.Name, result: result, err: err}
	}
}

func (m MergeModel) waitingNotice(name string, count int) string {
	return fmt.Sprintf("Asking %s to resolve %d conflict(s) - the file content is sent to it (q quits)", name, count)
}

// applyAssistant takes over the lines of an answer for the conflicts that are still unresolved. One undo reverts it all.
func (m MergeModel) applyAssistant(msg aiDoneMsg) MergeModel {
	m.aiBusy, m.aiCancel = false, nil
	if msg.err != nil {
		m.notice(fmt.Sprintf("%v", msg.err), true)
		return m
	}
	applied := 0
	for _, chunk := range m.changed {
		lines, ok := msg.result[chunk]
		if !ok || m.chunks[chunk].Kind != merge.Conflict || m.choices[chunk] != ChoiceAuto {
			continue
		}
		m.setChoice(chunk, ChoiceCustom, slices.Clone(lines))
		m.history[len(m.history)-1].joined = applied > 0
		applied++
	}
	if applied == 0 {
		m.notice(msg.name+" resolved nothing that is still open", true)
		return m
	}
	m.rebuild()
	m.notice(fmt.Sprintf("✓ %s resolved %d conflict(s), %d left - review the result (u undoes)", msg.name, applied, m.Unresolved()), false)
	if next := m.nextUnresolved(); next >= 0 {
		m.gotoChange(next)
	} else {
		m.keepVisible()
	}
	return m
}

// assistantHints are the shortcut hints of the installed assistants.
func (m MergeModel) assistantHints() []hint {
	hints := make([]hint, 0, len(m.assistants))
	for _, assistant := range m.assistants {
		hints = append(hints, hint{assistant.Key, assistant.Name, m.Unresolved() > 0 && !m.aiBusy, 1})
	}
	return hints
}
