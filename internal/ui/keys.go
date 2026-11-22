package ui

import (
	"strings"
	"sync"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// keyMap holds the mapping of commands to shortcuts in force and hands out reads and the one replacement safely: windows (and tests) may
// start at the same time, and UseKeys is called by each.
type keyMap struct {
	mu      sync.RWMutex
	mapping keymap.Map
}

func (k *keyMap) get() keymap.Map {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.mapping
}

func (k *keyMap) Is(command keymap.Command, key string) bool { return k.get().Is(command, key) }
func (k *keyMap) Lookup(scope keymap.Scope, key string) (keymap.Command, bool) {
	return k.get().Lookup(scope, key)
}
func (k *keyMap) Label(command keymap.Command) string  { return k.get().Label(command) }
func (k *keyMap) Key(command keymap.Command) string    { return k.get().Key(command) }
func (k *keyMap) Keys(command keymap.Command) []string { return k.get().Keys(command) }
func (k *keyMap) Sequences(command keymap.Command) []keymap.Sequence {
	return k.get().Sequences(command)
}
func (k *keyMap) Sequence(command keymap.Command) keymap.Sequence { return k.get().Sequence(command) }
func (k *keyMap) Entries(scope keymap.Scope) []keymap.Entry       { return k.get().Entries(scope) }
func (k *keyMap) Shadowed(scope keymap.Scope) []keymap.Sequence   { return k.get().Shadowed(scope) }
func (k *keyMap) Source(command keymap.Command) string            { return k.get().Source(command) }

// keys is the mapping in force. It is the default one until main installs what the .jdrc files say.
var keys = &keyMap{mapping: keymap.Default()}

// UseKeys installs the key mapping every view reads its shortcuts from, and shows in its footers and menus.
func UseKeys(mapping keymap.Map) {
	keys.mu.Lock()
	keys.mapping = mapping
	keys.mu.Unlock()
}

// Keys returns the mapping in force.
func Keys() keymap.Map { return keys.get() }

// keyLabel is how a command's key is named in messages: its first key, or "(unbound)" when .jdrc removed every key of it.
func keyLabel(command keymap.Command) string {
	if label := keys.Label(command); label != "" {
		return label
	}
	return "(" + string(command) + " is unbound)"
}

// quitKeys is the key part of the message about unsaved work, "q again (or ctrl+c, ctrl+d)": the first key of merge.quit, then ctrl+c and ctrl+d
// when they are keys of it too, and ctrl+c always (it quits whatever merge.quit is bound to).
func quitKeys() string {
	var names []string
	for i, key := range keys.Keys(keymap.MergeQuit) {
		if i == 0 || key == "ctrl+c" || key == "ctrl+d" {
			names = append(names, keymap.Pretty(key))
		}
	}
	if len(names) == 0 || !contains(names, "ctrl+c") {
		names = append(names, "ctrl+c")
	}
	if len(names) == 1 {
		return names[0] + " again"
	}
	return names[0] + " again (or " + strings.Join(names[1:], ", ") + ")"
}

func contains(list []string, item string) bool {
	for _, s := range list {
		if s == item {
			return true
		}
	}
	return false
}
