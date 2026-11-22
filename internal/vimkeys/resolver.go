// Package vimkeys turns the keys a user presses in the editor into the keys the editor's engine (internal/vimbuf) is written for. The user's
// bindings come from the .jdrc files (internal/keymap): a command can be bound to other keys, or to a sequence of keys (g g). The engine keeps
// its own keys, so the resolver translates: it holds back the keys of a sequence that is not complete yet, and hands the engine the
// canonical keys of the command once it is (vim.top is g g, vim.left is h, ...).
package vimkeys

import "github.com/eutima/jelly-dev/internal/keymap"

// Mode tells which table of bindings counts.
type Mode int

const (
	// Normal is normal and visual mode (the vim.* commands).
	Normal Mode = iota
	// Insert is insert mode (the insert.* commands: keys that are not typed text).
	Insert
)

// Output is what to do with a key press.
type Output struct {
	// Keys are the canonical keys for the engine, in order (none while a sequence is incomplete).
	Keys []string
	// Cancel asks the engine to drop its pending count, operator and prefix: the key was a mismatch, or the key of a command that was bound
	// to other keys, which does nothing any more.
	Cancel bool
}

// Resolver holds the bindings and the keys of a sequence that is not complete yet. A nil Resolver passes every key on unchanged.
type Resolver struct {
	scope     map[Mode][]keymap.Entry
	shadowed  map[Mode][]keymap.Sequence
	pending   []string
	pendingIn Mode
}

// New builds a resolver for a key mapping.
func New(mapping keymap.Map) *Resolver {
	return &Resolver{
		scope:    map[Mode][]keymap.Entry{Normal: mapping.Entries(keymap.ScopeVim), Insert: mapping.Entries(keymap.ScopeInsert)},
		shadowed: map[Mode][]keymap.Sequence{Normal: mapping.Shadowed(keymap.ScopeVim), Insert: mapping.Shadowed(keymap.ScopeInsert)},
	}
}

// Pending is the part of a key sequence that was typed, as the status line shows it ("" when none): for example "g" while g g waits.
func (r *Resolver) Pending() string {
	if r == nil || len(r.pending) == 0 {
		return ""
	}
	return keymap.Sequence(r.pending).Pretty()
}

// Reset forgets a half typed sequence (the editor left the mode it was typed in, or was asked to drop what is pending).
func (r *Resolver) Reset() {
	if r != nil {
		r.pending = nil
	}
}

// Wants tells whether a key is the first key of a binding of the editor: the keys the engine does not know (alt+x) are ignored unless a
// binding gives them a meaning.
func (r *Resolver) Wants(key string) bool {
	if r == nil {
		return false
	}
	for _, entries := range r.scope {
		for _, e := range entries {
			if len(e.Seq) > 0 && e.Seq[0] == key {
				return true
			}
		}
	}
	return false
}

// Feed resolves one key press. literal says that the engine takes the next key as text (the character after r, the command line).
func (r *Resolver) Feed(key string, mode Mode, literal bool) Output {
	if r == nil {
		return Output{Keys: []string{key}}
	}
	if len(r.pending) > 0 && r.pendingIn != mode {
		r.pending = nil
	}
	if literal {
		r.pending = nil
		return Output{Keys: []string{key}}
	}
	if mode == Insert {
		return r.feedInsert(key)
	}
	if len(r.pending) == 0 && len(key) == 1 && key[0] >= '0' && key[0] <= '9' { // counts: 3j, 10G, 0
		return Output{Keys: []string{key}}
	}
	r.pending, r.pendingIn = append(r.pending, key), mode
	entries := r.scope[Normal]
	waiting := false
	for _, e := range entries {
		switch {
		case len(e.Seq) == len(r.pending) && equal(e.Seq, r.pending):
			r.pending = nil
			return Output{Keys: append([]string(nil), e.Canonical...)}
		case len(e.Seq) > len(r.pending) && equal(e.Seq[:len(r.pending)], r.pending):
			waiting = true
		}
	}
	if waiting {
		return Output{}
	}
	partial := len(r.pending) > 1
	r.pending = nil
	if partial || r.blocked(Normal, key) {
		return Output{Cancel: true}
	}
	return Output{Keys: []string{key}}
}

// feedInsert resolves a key typed in insert mode: only single keys that are not text can be bound there.
func (r *Resolver) feedInsert(key string) Output {
	r.pending = nil
	for _, e := range r.scope[Insert] {
		if len(e.Seq) == 1 && e.Seq[0] == key {
			return Output{Keys: append([]string(nil), e.Canonical...)}
		}
	}
	if r.blocked(Insert, key) {
		return Output{}
	}
	return Output{Keys: []string{key}}
}

// blocked tells whether a key is the first key of a default binding that was moved to other keys: the command it ran does nothing now.
func (r *Resolver) blocked(mode Mode, key string) bool {
	for _, s := range r.shadowed[mode] {
		if len(s) > 0 && s[0] == key {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
