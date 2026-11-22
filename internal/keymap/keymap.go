package keymap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the name of the file with the key mapping, in the home folder and in a project.
const FileName = ".jdrc"

// EnvConfigDir names the folder of jd's own files; a file called jdrc in it is read after the one in the home folder.
const EnvConfigDir = "JD_CONFIG_DIR"

// defaultSource is the source shown for a command nothing has changed.
const defaultSource = "default"

// binding is the keys of a command and where they come from.
type binding struct {
	keys   []string
	source string // defaultSource, or "file:line"
}

// Map is the mapping of commands to keys in force: the defaults with the files laid over them.
type Map struct {
	bindings map[Command]binding
	index    map[Scope]map[string]Command // key -> command, per scope
}

// Default returns the mapping jd has when no file changes anything.
func Default() Map {
	m := Map{bindings: map[Command]binding{}}
	for _, d := range definitions {
		m.bindings[d.command] = binding{keys: append([]string(nil), d.keys...), source: defaultSource}
	}
	m.reindex()
	return m
}

func (m *Map) reindex() {
	m.index = map[Scope]map[string]Command{}
	for _, d := range definitions {
		for _, key := range m.bindings[d.command].keys {
			if m.index[d.scope] == nil {
				m.index[d.scope] = map[string]Command{}
			}
			m.index[d.scope][key] = d.command
		}
	}
}

// Apply lays the assignments of one file over the mapping and returns the lines it could not use. A command that is assigned loses its
// previous keys. A key that another command has from the defaults moves to the assigned command; a key another command was given by a file
// is refused (that line is skipped), so two commands never share a key in one scope.
func (m Map) Apply(assignments []Assignment, source string) (Map, []Problem) {
	next := Map{bindings: make(map[Command]binding, len(m.bindings))}
	for command, b := range m.bindings {
		next.bindings[command] = binding{keys: append([]string(nil), b.keys...), source: b.source}
	}
	var problems []Problem
	for _, a := range assignments {
		scope := ScopeOf(a.Command)
		if owner, key, clash := next.clash(a, scope); clash {
			problems = append(problems, Problem{Source: source, Line: a.Line, Text: fmt.Sprintf("%s: %s is used by %s (set that to another key or none first)", a.Command, Written(key), owner)})
			continue
		}
		for _, key := range a.Keys { // take the key from a command that only has it by default
			for _, d := range definitions {
				if d.scope != scope || d.command == a.Command {
					continue
				}
				b := next.bindings[d.command]
				if b.source != defaultSource {
					continue
				}
				b.keys = without(b.keys, key)
				next.bindings[d.command] = b
			}
		}
		next.bindings[a.Command] = binding{keys: a.Keys, source: fmt.Sprintf("%s:%d", source, a.Line)}
	}
	next.reindex()
	return next, problems
}

// clash finds a key of the assignment that another command got from a file.
func (m Map) clash(a Assignment, scope Scope) (owner Command, key string, found bool) {
	for _, key := range a.Keys {
		for _, d := range definitions {
			if d.scope != scope || d.command == a.Command {
				continue
			}
			b := m.bindings[d.command]
			if b.source == defaultSource {
				continue
			}
			for _, other := range b.keys {
				if other == key {
					return d.command, key, true
				}
			}
		}
	}
	return "", "", false
}

func without(keys []string, key string) []string {
	out := keys[:0:0]
	for _, k := range keys {
		if k != key {
			out = append(out, k)
		}
	}
	return out
}

// Lookup returns the command a key runs in a scope.
func (m Map) Lookup(scope Scope, key string) (Command, bool) {
	command, ok := m.index[scope][key]
	return command, ok
}

// Is tells whether a key runs a command.
func (m Map) Is(command Command, key string) bool {
	for _, k := range m.bindings[command].keys {
		if k == key {
			return true
		}
	}
	return false
}

// Keys returns the keys of a command (none when it is unbound).
func (m Map) Keys(command Command) []string { return m.bindings[command].keys }

// Label returns how the first key of a command is shown in the UI; "" for an unbound command.
func (m Map) Label(command Command) string {
	keys := m.bindings[command].keys
	if len(keys) == 0 {
		return ""
	}
	return Pretty(keys[0])
}

// Key returns the first key of a command, the one a click on its footer hint sends; "" for an unbound command.
func (m Map) Key(command Command) string {
	keys := m.bindings[command].keys
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// Source tells where the keys of a command come from: "default" or "file:line".
func (m Map) Source(command Command) string { return m.bindings[command].source }

// Load reads the key mapping: the defaults, then ~/.jdrc, then jdrc in the jd config folder, then .jdrc in projectDir (each one only when it
// exists). The problems are the lines that were skipped and the files that could not be read.
func Load(getenv func(string) string, home, projectDir string) (Map, []Problem) {
	m := Default()
	var problems []Problem
	type file struct{ path, shown string }
	var files []file
	add := func(path, shown string) {
		for _, f := range files {
			if sameFile(f.path, path) {
				return
			}
		}
		files = append(files, file{path, shown})
	}
	if home != "" {
		add(filepath.Join(home, FileName), "~/"+FileName)
	}
	if dir := getenv(EnvConfigDir); dir != "" {
		add(filepath.Join(dir, "jdrc"), filepath.Join(dir, "jdrc"))
	}
	if projectDir != "" {
		add(filepath.Join(projectDir, FileName), "./"+FileName)
	}
	for _, f := range files {
		info, err := os.Stat(f.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil && !info.Mode().IsRegular() { // a link to a terminal or a pipe would block the start
			problems = append(problems, Problem{Source: f.shown, Text: "not a regular file, skipped"})
			continue
		}
		var handle *os.File
		if err == nil {
			handle, err = os.Open(f.path)
		}
		if err != nil {
			problems = append(problems, Problem{Source: f.shown, Text: err.Error()})
			continue
		}
		assignments, found := Parse(handle, f.shown)
		_ = handle.Close()
		problems = append(problems, found...)
		var more []Problem
		m, more = m.Apply(assignments, f.shown)
		problems = append(problems, more...)
	}
	return m, problems
}

func sameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(infoA, infoB)
	}
	return a == b
}

// Render prints the mapping in the .jdrc syntax: every command with its keys (the defaults where no file changes anything), each line
// with its source and what the command does in a comment. The output is a valid .jdrc file that gives the same mapping.
func (m Map) Render() string {
	var out strings.Builder
	out.WriteString("# The shortcuts of jd. Save this as ~/.jdrc (or .jdrc in a project) and change what you like.\n")
	out.WriteString("# Syntax:  command = key[, key ...]   (none unbinds a command; the first key is the one shown in the UI)\n")
	out.WriteString("# Keys:    ctrl+j  alt+x  f4  esc  tab  shift+tab  enter  backspace  delete  up  down  left  right  pgup  pgdown  home  end\n")
	out.WriteString("#          space  comma  hash  or a single character. The vim editing keys are not part of this file.\n")
	for _, scope := range Scopes() {
		fmt.Fprintf(&out, "\n# %s: %s\n", scope, scope.Title())
		width := 0
		for _, d := range definitions {
			if d.scope == scope {
				width = max(width, len(d.command))
			}
		}
		for _, d := range definitions {
			if d.scope != scope {
				continue
			}
			keys := m.bindings[d.command].keys
			value := None
			if len(keys) > 0 {
				written := make([]string, len(keys))
				for i, key := range keys {
					written[i] = Written(key)
				}
				value = strings.Join(written, ", ")
			}
			fmt.Fprintf(&out, "%-*s = %-22s # %s: %s\n", width, d.command, value, m.bindings[d.command].source, d.doc)
		}
	}
	return out.String()
}
