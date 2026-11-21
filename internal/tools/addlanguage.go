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
	if _, saved := LoadLanguages(i.configDir); slices.Contains(i.languages, id) && (saved || i.configDir == "") {
		return nil // chosen already; the defaults that nobody saved are written down now, so a later change of them keeps the choice
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

// UpdateLanguage adds the language to the chosen ones and brings its language server and formatter to their newest versions;
// a tool that is missing is installed. It returns one result per tool.
func (i *Installer) UpdateLanguage(ctx context.Context, id string) ([]InstallResult, error) {
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
	return i.UpdateLanguages(ctx, ids, nil)
}

// UpdateLanguages updates the tools of the languages (each tool once, also when languages share it) and installs the ones that
// are missing; progress is told about every tool before and after (err is nil before). The result is remembered like the one of an
// installation: a failure is recorded, a success clears an earlier failure.
func (i *Installer) UpdateLanguages(ctx context.Context, languages []string, progress func(tool string, done bool, err error)) ([]InstallResult, error) {
	if len(languages) == 0 {
		return nil, ErrNoLanguages
	}
	var results []InstallResult
	rustupUpdated := false // rustup update brings the whole toolchain up to date: once is enough for rust-analyzer and rustfmt
	for _, name := range ToolsFor(languages) {
		tool, err := i.tool(name)
		if err != nil {
			return results, err
		}
		installed := i.env.Installed(tool)
		if progress != nil {
			progress(name, false, nil)
		}
		switch {
		case installed && tool.Kind == Rustup && rustupUpdated:
			i.forget(name) // updated with the toolchain: an earlier failure is over
		case installed:
			err = i.update(ctx, tool)
			rustupUpdated = rustupUpdated || (tool.Kind == Rustup && err == nil)
		default:
			err = i.Install(ctx, name)
		}
		if progress != nil {
			progress(name, true, err)
		}
		results = append(results, InstallResult{Tool: name, Updated: installed && err == nil, Err: err})
	}
	return results, nil
}

// tool returns the tool of a name.
func (i *Installer) tool(name string) (Tool, error) {
	for _, t := range i.tools {
		if t.Name == name {
			return t, nil
		}
	}
	return Tool{}, errors.New("unknown tool " + name)
}

// update updates one installed tool and remembers the result (see Install).
func (i *Installer) update(ctx context.Context, tool Tool) error {
	err := i.env.Update(ctx, tool)
	failures := i.load()
	if err != nil {
		failures[tool.Name] = failure{At: i.now(), Error: "update: " + err.Error()}
	} else {
		delete(failures, tool.Name)
	}
	if saveErr := i.save(failures); saveErr != nil && err == nil {
		return fmt.Errorf("remember the result: %w", saveErr)
	}
	return err
}

// forget removes the recorded failure of a tool.
func (i *Installer) forget(name string) {
	failures := i.load()
	if _, ok := failures[name]; ok {
		delete(failures, name)
		_ = i.save(failures)
	}
}
