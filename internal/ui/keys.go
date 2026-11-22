package ui

import (
	"strings"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// keys is the mapping of commands to shortcuts in force. It is the default one until main installs what the .jdrc files say; it never
// changes while jd runs, which is why a package variable is enough (the models are values built in many places).
var keys = keymap.Default()

// UseKeys installs the key mapping every view reads its shortcuts from, and shows in its footers and menus.
func UseKeys(mapping keymap.Map) { keys = mapping }

// Keys returns the mapping in force.
func Keys() keymap.Map { return keys }

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
