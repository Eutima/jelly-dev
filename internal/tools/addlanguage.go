package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// LanguageState is a language with how far its tools are installed, for the list of languages in the IDE.
type LanguageState struct {
	ID, Name string
	Tools    []string // the language server and the formatter
	Missing  []string // those of Tools that are not installed
	Chosen   bool     // the user chose the language (its tools are kept installed)
}

// LanguageStates lists every language with its missing tools and whether it is chosen.
func (i *Installer) LanguageStates() []LanguageState {
	var states []LanguageState
	for _, l := range Languages() {
		state := LanguageState{ID: l.ID, Name: l.Name, Chosen: i.chosen(l.ID)}
		for _, name := range l.Tools {
			state.Tools = append(state.Tools, name)
			if !i.IsInstalled(name) {
				state.Missing = append(state.Missing, name)
			}
		}
		states = append(states, state)
	}
	return states
}

// AddLanguage adds a language to the chosen ones (so the IDE keeps its tools installed) and installs the language server and
// the formatter that are missing. It returns one result per tool; an unknown language is an error.
func (i *Installer) AddLanguage(ctx context.Context, id string) ([]InstallResult, error) {
	ids, err := ParseLanguages(id)
	if err != nil {
		return nil, err
	}
	if len(ids) != 1 || strings.Contains(id, ",") {
		return nil, fmt.Errorf("name one language, not %q", id)
	}
	if err := i.choose(ids[0]); err != nil {
		return nil, err
	}
	results, err := i.InstallLanguages(ctx, ids, nil)
	if errors.Is(err, ErrNoLanguages) {
		return nil, err
	}
	return results, err
}

// chosen reports whether the user chose the language.
func (i *Installer) chosen(id string) bool {
	if !i.restrict {
		return true
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return slices.Contains(i.languages, id)
}

// choose adds a language to the chosen ones: saved first, so a failure to save leaves everything as it was.
func (i *Installer) choose(id string) error {
	if !i.restrict {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if slices.Contains(i.languages, id) {
		return nil
	}
	merged, _ := ParseLanguages(strings.Join(append(slices.Clone(i.languages), id), ","))
	if i.configDir != "" {
		if err := SaveLanguages(i.configDir, merged); err != nil {
			return fmt.Errorf("remember the language: %w", err)
		}
	}
	i.languages = merged
	return nil
}
