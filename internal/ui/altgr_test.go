package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A Swiss keyboard types { and [ with AltGr; a terminal may send that as alt plus the character.
func TestEditorKeys_AltGrSymbolsAreTypedAndAltLettersAreNot(t *testing.T) {
	for _, symbol := range []rune{'{', '[', ']', '}', '\\', '@', '#', '|', '~'} {
		keys := editorKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{symbol}, Alt: true})
		if len(keys) != 1 || keys[0] != string(symbol) {
			t.Errorf("alt+%c is typed as %c: %v", symbol, symbol, keys)
		}
	}
	for _, command := range []rune{'t', 'd', '6', ' '} {
		if keys := editorKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{command}, Alt: true}); keys != nil {
			t.Errorf("alt+%c stays a command, not text: %v", command, keys)
		}
	}
}
