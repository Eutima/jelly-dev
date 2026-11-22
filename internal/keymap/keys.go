package keymap

import (
	"fmt"
	"strings"
)

// aliases are names for keys that cannot be written as themselves in a .jdrc line.
var aliases = map[string]string{"space": " ", "comma": ",", "hash": "#"}

// named are the keys with a name, as Bubble Tea reports them.
var named = map[string]bool{
	"esc": true, "tab": true, "shift+tab": true, "enter": true, "backspace": true, "delete": true, "insert": true,
	"up": true, "down": true, "left": true, "right": true, "pgup": true, "pgdown": true, "home": true, "end": true,
	"ctrl+up": true, "ctrl+down": true, "ctrl+left": true, "ctrl+right": true,
	"shift+up": true, "shift+down": true, "shift+left": true, "shift+right": true,
	"ctrl+home": true, "ctrl+end": true, "ctrl+pgup": true, "ctrl+pgdown": true,
}

// ambiguous are keys a terminal sends as another key: ctrl+i is tab, ctrl+m is enter, ctrl+[ is esc, ctrl+h is (often) backspace.
var ambiguous = map[string]string{"ctrl+i": "tab", "ctrl+m": "enter", "ctrl+[": "esc", "ctrl+h": "backspace"}

// Normalize checks a key name and returns it the way Bubble Tea reports it (the way the keys are matched). Modifiers and names are
// case-insensitive, a single character keeps its case, and space, comma and hash name the characters that cannot stand for themselves.
func Normalize(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty key name")
	}
	if actual, ok := aliases[strings.ToLower(key)]; ok {
		return actual, nil
	}
	if single(key) {
		return key, nil
	}
	lower := strings.ToLower(key)
	alt := strings.HasPrefix(lower, "alt+")
	if alt {
		lower, key = strings.TrimPrefix(lower, "alt+"), key[len("alt+"):]
		if actual, ok := aliases[lower]; ok {
			return "alt+" + actual, nil
		}
		if single(key) {
			return "alt+" + key, nil
		}
	}
	prefix := map[bool]string{true: "alt+", false: ""}[alt]
	if base, ok := ambiguous[lower]; ok {
		return "", fmt.Errorf("%s cannot be told apart from %s in a terminal", lower, base)
	}
	if lower == "ctrl+space" || lower == "ctrl+@" { // a terminal sends ctrl+space as NUL, which Bubble Tea names ctrl+@
		return prefix + "ctrl+@", nil
	}
	switch {
	case named[lower], functionKey(lower):
		return prefix + lower, nil
	case strings.HasPrefix(lower, "ctrl+") && len(lower) == len("ctrl+")+1 && controlChar(lower[len("ctrl+"):][0]):
		return prefix + lower, nil
	}
	return "", fmt.Errorf("unknown key %q (examples: ctrl+j, alt+x, f4, esc, tab, up, pgdown, a single character, space)", key)
}

func single(s string) bool { return len([]rune(s)) == 1 }

func controlChar(c byte) bool {
	return c >= 'a' && c <= 'z' || strings.IndexByte("]\\^_", c) >= 0
}

func functionKey(s string) bool {
	var n int
	if _, err := fmt.Sscanf(s, "f%d", &n); err != nil || fmt.Sprintf("f%d", n) != s {
		return false
	}
	return n >= 1 && n <= 20
}

// hasModifier tells whether a key is a ctrl, alt or shift combination or a function key: the keys the project window accepts, because a
// bare letter would be typed text in the editor and the shell.
func hasModifier(key string) bool {
	return strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "alt+") || strings.HasPrefix(key, "shift+") || functionKey(key)
}

// pretty names for the keys that have a symbol or a shorter name in the footers.
var pretty = map[string]string{
	"right": "→", "left": "←", "up": "↑", "down": "↓", "backspace": "⌫", " ": "space", "pgdown": "pgdn", "pgup": "pgup",
	"ctrl+right": "ctrl+→", "ctrl+left": "ctrl+←", "ctrl+up": "ctrl+↑", "ctrl+down": "ctrl+↓", "ctrl+@": "ctrl+space",
}

// Pretty returns how a key is shown in the footers and menus.
func Pretty(key string) string {
	if p, ok := pretty[key]; ok {
		return p
	}
	if rest, ok := strings.CutPrefix(key, "alt+"); ok {
		return "alt+" + Pretty(rest)
	}
	return key
}

// Written returns how a key is written in a .jdrc file (the inverse of Normalize).
func Written(key string) string {
	switch key {
	case "ctrl+@":
		return "ctrl+space"
	case " ":
		return "space"
	case ",":
		return "comma"
	case "#":
		return "hash"
	}
	if rest, ok := strings.CutPrefix(key, "alt+"); ok {
		return "alt+" + Written(rest)
	}
	return key
}

// Sequence is the keys of one binding, pressed one after the other: one key for almost every command, several for the editor's
// (g g). A sequence is written with its keys separated by spaces.
type Sequence []string

// ParseSequence reads a sequence of key names separated by white space; every key is normalized.
func ParseSequence(text string) (Sequence, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty key name")
	}
	seq := make(Sequence, len(fields))
	for i, field := range fields {
		key, err := Normalize(field)
		if err != nil {
			return nil, err
		}
		seq[i] = key
	}
	return seq, nil
}

// String is the sequence as it is written in a .jdrc file.
func (s Sequence) String() string {
	parts := make([]string, len(s))
	for i, key := range s {
		parts[i] = Written(key)
	}
	return strings.Join(parts, " ")
}

// Pretty is the sequence as the footers show it: gg for g g, but space d with the name of a longer key.
func (s Sequence) Pretty() string {
	parts := make([]string, len(s))
	joined := ""
	for i, key := range s {
		parts[i] = Pretty(key)
		if len([]rune(parts[i])) > 1 {
			joined = " "
		}
	}
	return strings.Join(parts, joined)
}

// overlaps tells whether one sequence is the other or the start of the other: the editor could not tell them apart without waiting.
func overlaps(a, b Sequence) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s Sequence) equal(o Sequence) bool { return len(s) == len(o) && overlaps(s, o) }
