package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
)

// toolInstallTimeout is how long installing one tool may take.
const toolInstallTimeout = 12 * time.Minute

// ToolInstaller installs the language servers and formatters. Pending returns the names of the missing tools that should
// be installed now (all missing ones with force), Install installs one, Status describes all of them.
type ToolInstaller interface {
	Pending(force bool) []string
	Install(ctx context.Context, name string) error
	Status() []string
	// LanguageStates lists the languages with their missing tools (for :addlang), AddLanguage chooses a language and installs
	// the language server and the formatter that are missing.
	LanguageStates() []tools.LanguageState
	AddLanguage(ctx context.Context, id string) ([]tools.InstallResult, error)
	// UpdateLanguage brings the language server and the formatter of a language to their newest versions.
	UpdateLanguage(ctx context.Context, id string) ([]tools.InstallResult, error)
}

// toolsPendingMsg is the answer of the check for missing tools.
type toolsPendingMsg struct {
	names []string
	force bool
}

// toolDoneMsg reports that installing one tool ended.
type toolDoneMsg struct {
	name string
	err  error
}

// toolRun is the installation that is running: the queue and what came of it so far.
type toolRun struct {
	queue     []string
	index     int
	installed []string
	failed    map[string]error // by name
}

// checkTools asks the installer which tools are missing.
func (m ProjectModel) checkTools(force bool) tea.Cmd {
	installer := m.hooks.Tools
	return func() tea.Msg { return toolsPendingMsg{names: installer.Pending(force), force: force} }
}

// startTools begins installing the missing tools, one after the other, without blocking the window.
func (m ProjectModel) startTools(msg toolsPendingMsg) (tea.Model, tea.Cmd) {
	if m.toolRun != nil {
		m.say("Tools are being installed already", false)
		return m, nil
	}
	if len(msg.names) == 0 {
		if msg.force {
			m.say("All language servers and formatters are installed", false)
		}
		return m, nil
	}
	m.toolRun = &toolRun{queue: msg.names, failed: map[string]error{}}
	return m.installNextTool()
}

func (m ProjectModel) installNextTool() (tea.Model, tea.Cmd) {
	run := m.toolRun
	name := run.queue[run.index]
	m.toolStatus = fmt.Sprintf("Installing %s (%d/%d)...", name, run.index+1, len(run.queue))
	installer := m.hooks.Tools
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), toolInstallTimeout)
		defer cancel()
		return toolDoneMsg{name: name, err: installer.Install(ctx, name)}
	}
}

// toolDone records one result and goes on with the next tool or reports the end.
func (m ProjectModel) toolDone(msg toolDoneMsg) (tea.Model, tea.Cmd) {
	run := m.toolRun
	if run == nil {
		return m, nil
	}
	if msg.err != nil {
		run.failed[msg.name] = msg.err
	} else {
		run.installed = append(run.installed, msg.name)
	}
	run.index++
	if run.index < len(run.queue) {
		return m.installNextTool()
	}
	m.toolRun, m.toolStatus = nil, ""
	text, warn := toolSummary(run)
	m.say(text, warn)
	return m, nil
}

// toolSummary describes the result of an installation run in one line.
func toolSummary(run *toolRun) (string, bool) {
	var parts []string
	if n := len(run.installed); n > 0 {
		parts = append(parts, fmt.Sprintf("installed %s", strings.Join(run.installed, ", ")))
	}
	runtimes := map[string]bool{}
	var other []string
	for name, err := range run.failed {
		var missing *tools.MissingRuntimeError
		if errors.As(err, &missing) {
			runtimes[missing.Needs] = true
			continue
		}
		other = append(other, name+": "+oneLine(err.Error()))
	}
	sort.Strings(other)
	if len(runtimes) > 0 {
		var needs []string
		for need := range runtimes {
			needs = append(needs, need)
		}
		sort.Strings(needs)
		parts = append(parts, fmt.Sprintf("%d need %s - run the jd installer", countMissing(run, runtimes), strings.Join(needs, ", ")))
	}
	if len(other) > 0 {
		parts = append(parts, "failed: "+strings.Join(other, "; ")+" (:tools retries)")
	}
	return "Tools: " + strings.Join(parts, " · "), len(run.failed) > 0
}

func countMissing(run *toolRun, runtimes map[string]bool) int {
	count := 0
	for _, err := range run.failed {
		var missing *tools.MissingRuntimeError
		if errors.As(err, &missing) && runtimes[missing.Needs] {
			count++
		}
	}
	return count
}

// oneLine puts a text on one line: line breaks and runs of spaces become single spaces. Nothing is cut off; a long text wraps where it is shown.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// toolsCommand is the :tools command: without an argument it installs what is missing (also what failed before), with
// "status" it only says what is installed.
func (m ProjectModel) toolsCommand(argument string) (tea.Model, tea.Cmd) {
	if m.hooks.Tools == nil {
		m.say("E1: Tool installation is not available here", true)
		return m, nil
	}
	if argument == "status" {
		var missing []string
		status := m.hooks.Tools.Status()
		for _, line := range status {
			if strings.HasPrefix(line, "missing") {
				missing = append(missing, strings.TrimSuffix(strings.Fields(line)[1], ":"))
			}
		}
		text := fmt.Sprintf("Tools: %d of %d installed", len(status)-len(missing), len(status))
		if len(missing) > 0 {
			text += " · missing: " + strings.Join(missing, ", ") + " (:tools installs them)"
		}
		m.say(text, len(missing) > 0)
		return m, nil
	}
	if m.toolRun != nil {
		m.say("Tools are being installed already: "+m.toolStatus, false)
		return m, nil
	}
	m.say("Checking the language servers and formatters...", false)
	return m, m.checkTools(true)
}
