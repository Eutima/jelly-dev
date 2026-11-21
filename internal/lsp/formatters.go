package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FormatterChoice is a formatter the user can pick when :format finds none for a file type.
type FormatterChoice struct {
	Name      string
	Command   []string // reads the text on stdin and writes the formatted text to stdout; "{file}" is the file name
	Detail    string   // what it formats
	Installed bool     // whether the command is on the PATH
	// Extensions are the file types it is made for (with the dot); such formatters are listed first for those files.
	Extensions []string
}

// KnownFormatters are the formatters offered in the picker, in the order they are listed otherwise.
func KnownFormatters() []FormatterChoice {
	return []FormatterChoice{
		{Name: "gofmt", Command: []string{"gofmt"}, Detail: "Go", Extensions: []string{".go"}},
		{Name: "goimports", Command: []string{"goimports"}, Detail: "Go, also sorts the imports", Extensions: []string{".go"}},
		{Name: "prettier", Command: []string{"prettier", "--stdin-filepath", "{file}"}, Detail: "JavaScript, TypeScript, HTML, CSS, JSON, YAML, Markdown",
			Extensions: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".html", ".htm", ".css", ".scss", ".less", ".json", ".jsonc", ".yaml", ".yml", ".md", ".vue"}},
		{Name: "black", Command: []string{"black", "-q", "-"}, Detail: "Python", Extensions: []string{".py", ".pyi"}},
		{Name: "ruff format", Command: []string{"ruff", "format", "-"}, Detail: "Python (fast)", Extensions: []string{".py", ".pyi"}},
		{Name: "shfmt", Command: []string{"shfmt"}, Detail: "Shell scripts", Extensions: []string{".sh", ".bash", ".zsh"}},
		{Name: "clang-format", Command: []string{"clang-format", "--assume-filename={file}"}, Detail: "C, C++, Objective-C, Java, C#",
			Extensions: []string{".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".m", ".mm", ".cs", ".java"}},
		{Name: "rustfmt", Command: []string{"rustfmt", "--emit=stdout"}, Detail: "Rust", Extensions: []string{".rs"}},
		{Name: "google-java-format", Command: []string{"google-java-format", "-"}, Detail: "Java", Extensions: []string{".java"}},
		{Name: "jq", Command: []string{"jq", "."}, Detail: "JSON", Extensions: []string{".json", ".jsonc"}},
		{Name: "xmllint", Command: []string{"xmllint", "--format", "-"}, Detail: "XML, HTML", Extensions: []string{".xml", ".xsd", ".xsl", ".svg", ".html"}},
		{Name: "yamlfmt", Command: []string{"yamlfmt", "-in"}, Detail: "YAML", Extensions: []string{".yaml", ".yml"}},
		{Name: "taplo", Command: []string{"taplo", "format", "-"}, Detail: "TOML", Extensions: []string{".toml"}},
		{Name: "terraform fmt", Command: []string{"terraform", "fmt", "-"}, Detail: "Terraform", Extensions: []string{".tf", ".tfvars"}},
		{Name: "sql-formatter", Command: []string{"sql-formatter"}, Detail: "SQL", Extensions: []string{".sql"}},
	}
}

// fileType is the key under which the choice for a file is remembered: its extension in lower case.
func fileType(path string) string { return strings.ToLower(filepath.Ext(path)) }

// FormatterChoices lists the known formatters for a file: those made for its file type first, installed ones before missing
// ones, otherwise in the order of KnownFormatters. Installed tells whether the command is on the PATH.
func (m *Manager) FormatterChoices(path string) []FormatterChoice {
	choices := KnownFormatters()
	ext := fileType(path)
	made := func(c FormatterChoice) bool {
		for _, e := range c.Extensions {
			if e == ext {
				return true
			}
		}
		return false
	}
	for i := range choices {
		_, err := m.lookPath(choices[i].Command[0])
		choices[i].Installed = err == nil
	}
	sort.SliceStable(choices, func(i, j int) bool {
		if mi, mj := made(choices[i]), made(choices[j]); mi != mj {
			return mi
		}
		return choices[i].Installed && !choices[j].Installed
	})
	return choices
}

// FormatWith formats a text with the chosen formatter.
func (m *Manager) FormatWith(ctx context.Context, path, text string, choice FormatterChoice) (string, error) {
	if len(choice.Command) == 0 {
		return "", errors.New("no formatter chosen")
	}
	if _, err := m.lookPath(choice.Command[0]); err != nil {
		return "", fmt.Errorf("%s is not installed: %q is not on the PATH", choice.Name, choice.Command[0])
	}
	return runFormatterChecked(ctx, choice.Command, path, text, m.root)
}

// UseFormatterFile makes the manager remember the formatters the user chose in file (a JSON object of file type to command,
// for example {".sh": ["shfmt"]}) and applies the ones stored there already. A missing file is not an error.
func (m *Manager) UseFormatterFile(file string) error {
	m.formatterFile = file
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	chosen := map[string][]string{}
	if err := json.Unmarshal(data, &chosen); err != nil {
		// keep what the user wrote: the next choice would overwrite the file
		backup := file + ".bak"
		if renameErr := os.Rename(file, backup); renameErr != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		return fmt.Errorf("read %s: %w (kept as %s)", file, err, backup)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for ext, command := range chosen {
		if len(command) > 0 {
			m.chosen[strings.ToLower(ext)] = command
		}
	}
	return nil
}

// RememberFormatter makes :format use the formatter for all files of this type from now on, also in later sessions.
func (m *Manager) RememberFormatter(path string, choice FormatterChoice) error {
	ext := fileType(path)
	if ext == "" || len(choice.Command) == 0 {
		return errors.New("only files with an extension can have a formatter of their own")
	}
	m.mu.Lock()
	defer m.mu.Unlock() // the snapshot and the write belong together: two choices must not interleave
	m.chosen[ext] = choice.Command
	if m.formatterFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(m.chosen, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.formatterFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(m.formatterFile, append(data, '\n'), 0o600)
}

// chosenFormatter returns the formatter the user picked for this file type, when there is one.
func (m *Manager) chosenFormatter(path string) ([]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	command, ok := m.chosen[fileType(path)]
	return command, ok
}
