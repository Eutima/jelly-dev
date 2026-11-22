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
	seqs   []Sequence
	source string // defaultSource, or "file:line"
}

// steal records a key sequence that moved from a command that had it by default to the command a file assigned it to.
type steal struct {
	victim, thief Command
	lost, given   Sequence // the default sequence the victim loses, and the sequence of the thief that overlaps it
	file          string   // where the assignment is
	line          int
}

// Map is the mapping of commands to keys in force: the defaults with the files laid over them.
type Map struct {
	bindings map[Command]binding
	index    map[Scope]map[string]Command // single key -> command, per scope (the keys of the project window, merge and diff views)
	steals   []steal
}

// Default returns the mapping jd has when no file changes anything.
func Default() Map {
	m := Map{bindings: map[Command]binding{}}
	for _, d := range definitions {
		m.bindings[d.command] = binding{seqs: d.sequences(), source: defaultSource}
	}
	m.reindex()
	return m
}

func (m *Map) reindex() {
	m.index = map[Scope]map[string]Command{}
	for _, d := range definitions {
		for _, seq := range m.bindings[d.command].seqs {
			if len(seq) != 1 {
				continue
			}
			if m.index[d.scope] == nil {
				m.index[d.scope] = map[string]Command{}
			}
			m.index[d.scope][seq[0]] = d.command
		}
	}
}

// Apply lays the assignments of one file over the mapping and returns the lines it could not use. A command that is assigned loses its
// previous keys. A key that another command has from the defaults moves to the assigned command (Conflicts reports it unless that command
// gets keys of its own); a key another command was given by a file is refused (that line is skipped), so two commands never share a key
// or the start of each other's key sequence in one scope.
func (m Map) Apply(assignments []Assignment, source string) (Map, []Problem) {
	next := Map{bindings: make(map[Command]binding, len(m.bindings)), steals: append([]steal(nil), m.steals...)}
	for command, b := range m.bindings {
		next.bindings[command] = binding{seqs: append([]Sequence(nil), b.seqs...), source: b.source}
	}
	var problems []Problem
	lines := map[Command]int{} // where a command was set in this file
	for _, a := range assignments {
		if first, again := lines[a.Command]; again {
			problems = append(problems, Problem{Source: source, Line: a.Line, Text: fmt.Sprintf("%s is set twice in this file (line %d and line %d): keep one", a.Command, first, a.Line)})
			continue
		}
		scope := ScopeOf(a.Command)
		if owner, seq, clash := next.clash(a, scope); clash {
			problems = append(problems, Problem{Source: source, Line: a.Line, Text: fmt.Sprintf("%s: %s is used by %s (set that to another key or none first)", a.Command, seq, owner)})
			continue
		}
		for _, given := range a.Keys { // take the keys from the commands that only have them by default
			for _, d := range definitions {
				if d.scope != scope || d.command == a.Command {
					continue
				}
				b := next.bindings[d.command]
				if b.source != defaultSource {
					continue
				}
				var kept []Sequence
				for _, seq := range b.seqs {
					if overlaps(seq, given) {
						next.steals = append(next.steals, steal{victim: d.command, thief: a.Command, lost: seq, given: given, file: source, line: a.Line})
						continue
					}
					kept = append(kept, seq)
				}
				b.seqs = kept
				next.bindings[d.command] = b
			}
		}
		lines[a.Command] = a.Line
		next.bindings[a.Command] = binding{seqs: a.Keys, source: fmt.Sprintf("%s:%d", source, a.Line)}
	}
	next.reindex()
	return next, problems
}

// clash finds a sequence of the assignment that overlaps one another command got from a file.
func (m Map) clash(a Assignment, scope Scope) (owner Command, seq Sequence, found bool) {
	for _, given := range a.Keys {
		for _, d := range definitions {
			if d.scope != scope || d.command == a.Command {
				continue
			}
			b := m.bindings[d.command]
			if b.source == defaultSource {
				continue
			}
			for _, other := range b.seqs {
				if overlaps(other, given) {
					return d.command, given, true
				}
			}
		}
	}
	return "", nil, false
}

func holds(seqs []Sequence, key Sequence) bool {
	for _, s := range seqs {
		if overlaps(s, key) {
			return true
		}
	}
	return false
}

// problemAt makes a problem at the file and line a command was set at ("file:line").
func (m Map) problemAt(command Command, text string) Problem {
	source := m.bindings[command].source
	if i := strings.LastIndex(source, ":"); i >= 0 {
		line := 0
		if _, err := fmt.Sscanf(source[i+1:], "%d", &line); err == nil {
			return Problem{Source: source[:i], Line: line, Text: text}
		}
	}
	return Problem{Source: source, Text: text}
}

// Conflicts lists what the files left ambiguous: sequences that were taken from a command that has them by default and that no file gave
// other keys (the command lost them without the files saying what it should have instead; rebinding or unbinding it settles that), and
// editor or insert keys that a project window command handles first.
func (m Map) Conflicts() []Problem {
	var out []Problem
	for _, s := range m.steals {
		if m.bindings[s.victim].source != defaultSource {
			continue // a file gave the command keys of its own
		}
		text := fmt.Sprintf("%s: %s is the default key of %s, which would lose it; give %s other keys (or none) in a file", s.thief, s.given, s.victim, s.victim)
		if !s.given.equal(s.lost) {
			text = fmt.Sprintf("%s: %s overlaps %s, a default key of %s, which would lose it; give %s other keys (or none) in a file", s.thief, s.given, s.lost, s.victim, s.victim)
		}
		out = append(out, Problem{Source: s.file, Line: s.line, Text: text})
	}
	return append(out, m.windowKeyConflicts()...)
}

// windowKeyConflicts finds editor and insert keys that the project window takes first: the window handles its commands before a key reaches
// the editor, so a key of a sequence in any position (a sequence that could never complete) or a default key that a file gave to a window
// command (which would silently stop working) is a conflict. shift+tab in insert mode is left to the editor.
func (m Map) windowKeyConflicts() []Problem {
	var out []Problem
	for _, d := range definitions {
		if d.scope != ScopeVim && d.scope != ScopeInsert {
			continue
		}
		b := m.bindings[d.command]
		for _, seq := range b.seqs {
			for _, key := range seq {
				owner, ok := m.index[ScopeIDE][key]
				if !ok || (d.scope == ScopeInsert && key == "shift+tab") {
					continue
				}
				if b.source != defaultSource {
					out = append(out, m.problemAt(d.command, fmt.Sprintf("%s: %s uses %s, which is the key of %s in the project window; the window handles it first", d.command, seq, Written(key), owner)))
				} else if m.bindings[owner].source != defaultSource {
					out = append(out, m.problemAt(owner, fmt.Sprintf("%s: %s is the default key of %s in the editor, which would stop working; give %s other keys (or none) in a file", owner, Written(key), d.command, d.command)))
				}
			}
		}
	}
	return out
}

// Lookup returns the command a single key runs in a scope.
func (m Map) Lookup(scope Scope, key string) (Command, bool) {
	command, ok := m.index[scope][key]
	return command, ok
}

// Is tells whether a single key runs a command.
func (m Map) Is(command Command, key string) bool {
	for _, seq := range m.bindings[command].seqs {
		if len(seq) == 1 && seq[0] == key {
			return true
		}
	}
	return false
}

// Keys returns the single keys of a command (none when it is unbound; the sequences of the editor are in Sequences).
func (m Map) Keys(command Command) []string {
	var out []string
	for _, seq := range m.bindings[command].seqs {
		if len(seq) == 1 {
			out = append(out, seq[0])
		}
	}
	return out
}

// Sequences returns every key sequence of a command.
func (m Map) Sequences(command Command) []Sequence { return m.bindings[command].seqs }

// Label returns how the first key (or sequence) of a command is shown in the UI; "" for an unbound command.
func (m Map) Label(command Command) string {
	seqs := m.bindings[command].seqs
	if len(seqs) == 0 {
		return ""
	}
	return seqs[0].Pretty()
}

// Key returns the first key of a command, the one a click on its footer hint sends; "" for an unbound command.
func (m Map) Key(command Command) string {
	seqs := m.bindings[command].seqs
	if len(seqs) == 0 {
		return ""
	}
	return seqs[0][0]
}

// Sequence returns the first sequence of a command, the one a click on its footer hint sends; nil for an unbound command.
func (m Map) Sequence(command Command) Sequence {
	seqs := m.bindings[command].seqs
	if len(seqs) == 0 {
		return nil
	}
	return seqs[0]
}

// Source tells where the keys of a command come from: "default" or "file:line".
func (m Map) Source(command Command) string { return m.bindings[command].source }

// Entry is one binding of the editor: the sequence the user presses, the command it runs and the keys the editor's engine knows that
// command by (the key itself when it is a default of the command, else the first default of the command).
type Entry struct {
	Seq       Sequence
	Canonical Sequence
	Command   Command
}

// Entries lists the bindings of a scope (the editor's normal and visual mode, or its insert mode).
func (m Map) Entries(scope Scope) []Entry {
	var out []Entry
	for _, d := range definitions {
		if d.scope != scope {
			continue
		}
		defaults := d.sequences()
		for _, seq := range m.bindings[d.command].seqs {
			canonical := Canonical(d.command)
			if containsSeq(defaults, seq) {
				canonical = seq // a default key is its own canonical key: the engine treats some of a command's keys differently (ctrl+n in the completion popup)
			}
			out = append(out, Entry{Seq: seq, Canonical: canonical, Command: d.command})
		}
	}
	return out
}

// Shadowed lists the keys the editor's engine would act on by itself, because they are the default keys of a command in the scope, but
// that no longer run it: the command was bound to other keys. The editor ignores them.
func (m Map) Shadowed(scope Scope) []Sequence {
	var out []Sequence
	for _, d := range definitions {
		if d.scope != scope {
			continue
		}
		for _, seq := range d.sequences() {
			if !containsSeq(m.bindings[d.command].seqs, seq) {
				out = append(out, seq)
			}
		}
	}
	return out
}

func containsSeq(seqs []Sequence, key Sequence) bool {
	for _, s := range seqs {
		if s.equal(key) {
			return true
		}
	}
	return false
}

// Load reads the key mapping: the defaults, then ~/.jdrc, then jdrc in the jd config folder, then .jdrc in projectDir (each one only when it
// exists). The problems are the lines that were skipped, the files that could not be read and the conflicts: a key moved away from a command
// that has it by default without the files saying what that command gets instead.
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
	problems = append(problems, m.Conflicts()...)
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
	out.WriteString("#          space  comma  hash  or a single character. In the vim.* commands a key sequence is keys separated by spaces (g g).\n")
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
			seqs := m.bindings[d.command].seqs
			value := None
			if len(seqs) > 0 {
				written := make([]string, len(seqs))
				for i, seq := range seqs {
					written[i] = seq.String()
				}
				value = strings.Join(written, ", ")
			}
			fmt.Fprintf(&out, "%-*s = %-22s # %s: %s\n", width, d.command, value, m.bindings[d.command].source, d.doc)
		}
	}
	return out.String()
}
