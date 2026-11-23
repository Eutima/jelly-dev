package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eutima/jelly-dev/internal/source"
)

// languagesFile keeps which languages the user wants servers and formatters for.
const languagesFile = "languages.json"

// Language is something the user writes code in: its name for the user and the tools (language server and formatter) it needs.
type Language struct {
	ID    string // what the user types: python, html, ...
	Name  string
	Tools []string // names of Defaults()
	// OptIn languages are only installed when they are named: "all", the installer without a choice and the first-start question leave
	// them out, because their language servers need a big runtime (.NET, PowerShell) that most machines do not have.
	OptIn bool
}

// Languages are all the languages jd knows tools for, in the order they are shown.
func Languages() []Language {
	return []Language{
		{ID: "python", Name: "Python", Tools: []string{"pyright", "black", "debugpy"}},
		{ID: "html", Name: "HTML", Tools: []string{"vscode-langservers-extracted", "prettier"}},
		{ID: "css", Name: "CSS, SCSS, Less", Tools: []string{"vscode-langservers-extracted", "prettier"}},
		{ID: "json", Name: "JSON", Tools: []string{"vscode-langservers-extracted", "prettier"}},
		{ID: "yaml", Name: "YAML", Tools: []string{"yaml-language-server", "prettier"}},
		{ID: "docker", Name: "Dockerfile", Tools: []string{"dockerfile-language-server"}},
		{ID: "compose", Name: "Docker Compose", Tools: []string{"compose-language-service"}},
		{ID: "javascript", Name: "JavaScript and TypeScript", Tools: []string{"typescript-language-server", "prettier", "js-debug"}},
		{ID: "angular", Name: "Angular", Tools: []string{"angular-language-server", "typescript-language-server", "prettier"}},
		{ID: "go", Name: "Go", Tools: []string{"gopls", "delve"}},
		{ID: "rust", Name: "Rust", Tools: []string{"rust-analyzer", "rustfmt"}},
		{ID: "c", Name: "C", Tools: []string{"clangd", "clang-format"}},
		{ID: "cpp", Name: "C++", Tools: []string{"clangd", "clang-format"}},
		{ID: "java", Name: "Java", Tools: []string{"jdtls"}},
		{ID: "bash", Name: "Shell (Bash, sh)", Tools: []string{"bash-language-server"}},
		{ID: "csharp", Name: "C# (.NET)", Tools: []string{"csharp-ls"}, OptIn: true},
		{ID: "powershell", Name: "PowerShell", Tools: []string{"powershell-editor-services"}, OptIn: true},
	}
}

// DefaultLanguages are chosen when the user has not chosen yet.
func DefaultLanguages() []string {
	return []string{"python", "html", "css", "json", "yaml", "docker", "compose"}
}

// ParseLanguages turns "python,html css" (or "all") into language ids, in the order of Languages, without doubles. "all" means every
// language except the opt-in ones, which have to be named. An unknown name is an error that lists the known ones.
func ParseLanguages(list string) ([]string, error) {
	fields := strings.FieldsFunc(strings.ToLower(list), func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	known := Languages()
	want := map[string]bool{}
	for _, field := range fields {
		if field == "none" {
			continue // names no language: an empty list
		}
		if field == "all" {
			for _, l := range known {
				if !l.OptIn {
					want[l.ID] = true
				}
			}
			continue
		}
		if !slices.ContainsFunc(known, func(l Language) bool { return l.ID == field }) {
			ids := make([]string, len(known))
			for i, l := range known {
				ids[i] = l.ID
			}
			return nil, fmt.Errorf("unknown language %q (known: %s, or all)", field, strings.Join(ids, ", "))
		}
		want[field] = true
	}
	ids := []string{} // never nil: an empty choice must not look like "no choice"
	for _, l := range known {
		if want[l.ID] {
			ids = append(ids, l.ID)
		}
	}
	return ids, nil
}

// AllLanguages returns the ids of every language that "all" stands for (not the opt-in ones).
func AllLanguages() []string {
	ids := make([]string, 0, len(Languages()))
	for _, l := range Languages() {
		if !l.OptIn {
			ids = append(ids, l.ID)
		}
	}
	return ids
}

// ToolsFor returns the names of the tools the languages need, each once, in the order of Defaults.
func ToolsFor(languages []string) []string {
	need := map[string]bool{}
	for _, l := range Languages() {
		if slices.Contains(languages, l.ID) {
			for _, t := range l.Tools {
				need[t] = true
			}
		}
	}
	var names []string
	for _, t := range Defaults() {
		if need[t.Name] {
			names = append(names, t.Name)
		}
	}
	return names
}

// retiredLanguages were offered once and are gone; a saved choice that still names one keeps its other languages.
var retiredLanguages = []string{"groovy", "debug"} // "debug" was the opt-in group of the debuggers, which now come with their language

// LoadLanguages returns the languages the user chose; ok is false when nothing was chosen yet (or the file is unreadable).
func LoadLanguages(configDir string) (languages []string, ok bool) {
	data, err := os.ReadFile(filepath.Join(configDir, languagesFile))
	if err != nil {
		return nil, false
	}
	var state struct {
		Languages []string `json:"languages"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil, false
	}
	kept := slices.DeleteFunc(slices.Clone(state.Languages), func(id string) bool { return slices.Contains(retiredLanguages, id) })
	if len(kept) == 0 && len(state.Languages) > 0 {
		return nil, false // only retired languages were chosen: ask again
	}
	ids, err := ParseLanguages(strings.Join(kept, ","))
	if err != nil {
		return nil, false
	}
	return ids, true
}

// SaveLanguages remembers the languages the user chose (an empty list is a choice too: no tools).
func SaveLanguages(configDir string, languages []string) error {
	ids, err := ParseLanguages(strings.Join(languages, ","))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]any{"languages": ids}, "", "  ")
	if err != nil {
		return err
	}
	return source.WriteAtomic(filepath.Join(configDir, languagesFile), append(data, '\n'))
}

// ErrNoLanguages is returned when installing is asked for without any language chosen.
var ErrNoLanguages = errors.New("no language chosen")

// InstallResult is what came of installing one tool.
type InstallResult struct {
	Tool    string
	Already bool  // it was installed before (and nothing was done to it)
	Updated bool  // it was installed and has been updated now
	Err     error // nil when it is installed (and up to date) now
}

// InstallLanguages installs the tools of the languages that are missing; progress is told about every tool before and after
// it is installed (err is nil before). It returns one result per tool.
func (i *Installer) InstallLanguages(ctx context.Context, languages []string, progress func(tool string, done bool, err error)) ([]InstallResult, error) {
	if len(languages) == 0 {
		return nil, ErrNoLanguages
	}
	var results []InstallResult
	for _, name := range ToolsFor(languages) {
		var tool Tool
		for _, t := range i.tools {
			if t.Name == name {
				tool = t
			}
		}
		if i.env.Installed(tool) {
			results = append(results, InstallResult{Tool: name, Already: true})
			continue
		}
		if progress != nil {
			progress(name, false, nil)
		}
		err := i.Install(ctx, name)
		if progress != nil {
			progress(name, true, err)
		}
		results = append(results, InstallResult{Tool: name, Err: err})
	}
	return results, nil
}
