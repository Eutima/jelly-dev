package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/eutima/jelly-dev/internal/source"
)

const (
	stateFile = "tools.json"
	// RetryAfter is how long jd waits before it tries to install a tool again that failed.
	RetryAfter = 24 * time.Hour
)

type failure struct {
	At    time.Time `json:"at"`
	Error string    `json:"error"`
}

// Installer installs the missing tools. A tool that failed is not tried again at every start (that would make every start
// slow when, say, there is no network): the failure is remembered for RetryAfter, and the command :tools forces a new try.
type Installer struct {
	env       Env
	tools     []Tool
	configDir string // where languages.json and the failures are kept; empty for an installer built by hand
	restrict  bool   // false: every tool counts (an installer built by hand); true: only those of languages
	mu        sync.Mutex
	languages []string // the languages whose tools are installed automatically (only when restricted); guarded by mu: the IDE
	// asks from its own goroutine while a language is being added in another
	path string
	now  func() time.Time
}

// NewInstaller returns an installer for the default tools; configDir is where the failures are remembered.
// The languages the user chose (languages.json) decide which tools are installed; without a choice the default languages do.
func NewInstaller(configDir string) *Installer {
	languages, ok := LoadLanguages(configDir)
	if !ok {
		languages = DefaultLanguages()
	}
	return &Installer{env: DefaultEnv(), tools: Defaults(), configDir: configDir, languages: languages, restrict: true, path: filepath.Join(configDir, stateFile), now: time.Now}
}

// wanted reports whether the tool belongs to a chosen language.
func (i *Installer) wanted(name string) bool {
	if !i.restrict { // an installer built by hand: everything
		return true
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return slices.Contains(ToolsFor(i.languages), name)
}

func (i *Installer) load() map[string]failure {
	failures := map[string]failure{}
	data, err := os.ReadFile(i.path)
	if err != nil {
		return failures
	}
	var state struct {
		Failed map[string]failure `json:"failed"`
	}
	if json.Unmarshal(data, &state) == nil && state.Failed != nil {
		failures = state.Failed
	}
	return failures
}

func (i *Installer) save(failures map[string]failure) error {
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"failed": failures}, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(i.path, append(data, '\n'))
}

// Pending returns the names of the tools that are missing and should be installed now: those that did not fail recently,
// or all of them when force is set.
func (i *Installer) Pending(force bool) []string {
	failures := i.load()
	var names []string
	for _, t := range i.env.Missing(i.tools) {
		if !i.wanted(t.Name) {
			continue
		}
		if f, failed := failures[t.Name]; failed && !force && i.now().Sub(f.At) < RetryAfter {
			continue
		}
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

// IsInstalled reports whether the named tool is installed.
func (i *Installer) IsInstalled(name string) bool {
	for _, t := range i.tools {
		if t.Name == name {
			return i.env.Installed(t)
		}
	}
	return false
}

// Purpose says what a tool is for.
func (i *Installer) Purpose(name string) string {
	for _, t := range i.tools {
		if t.Name == name {
			return t.Purpose
		}
	}
	return ""
}

// Install installs a tool by name and remembers a failure (or forgets an earlier one).
func (i *Installer) Install(ctx context.Context, name string) error {
	for _, t := range i.tools {
		if t.Name != name {
			continue
		}
		err := i.env.Install(ctx, t)
		failures := i.load()
		if err != nil {
			failures[name] = failure{At: i.now(), Error: err.Error()}
		} else {
			delete(failures, name)
		}
		if saveErr := i.save(failures); saveErr != nil && err == nil {
			return fmt.Errorf("remember the result: %w", saveErr)
		}
		return err
	}
	return errors.New("unknown tool " + name)
}

// Status lists every tool with whether it is installed and, when it failed, why; for the :tools command.
func (i *Installer) Status() []string {
	failures := i.load()
	var lines []string
	for _, t := range i.tools {
		if !i.wanted(t.Name) && !i.env.Installed(t) { // tools of languages that were not chosen are not listed as missing
			continue
		}
		switch f, failed := failures[t.Name]; {
		case i.env.Installed(t):
			lines = append(lines, fmt.Sprintf("ok       %s (%s)", t.Name, t.Purpose))
		case failed:
			lines = append(lines, fmt.Sprintf("missing  %s: %s", t.Name, f.Error))
		default:
			lines = append(lines, fmt.Sprintf("missing  %s (%s)", t.Name, t.Purpose))
		}
	}
	return lines
}
