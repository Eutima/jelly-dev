package ui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/diff"
	"github.com/eutima/jelly-dev/internal/editor"
)

// benchEditor is an editor with a large Go file, the size of a big source file.
func benchEditor(b *testing.B) EditorModel {
	b.Helper()
	data, err := os.ReadFile("gittui.go")
	if err != nil {
		b.Fatal(err)
	}
	lines := diff.SplitLines(strings.Repeat(string(data), 12))
	model := NewEditorModel(editor.Document{Path: "big.go", Lines: lines, LineEnding: "\n", FinalNewline: true})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return updated.(EditorModel)
}

// BenchmarkEditor_HoldArrowDown is what happens per key repeat: the key is handled, then the screen is drawn.
func BenchmarkEditor_HoldArrowDown(b *testing.B) {
	model := benchEditor(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(EditorModel)
		_ = model.View()
	}
}

// BenchmarkEditor_TypeCharacter is what happens per typed character in insert mode.
func BenchmarkEditor_TypeCharacter(b *testing.B) {
	model := benchEditor(b)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	model = updated.(EditorModel)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		model = updated.(EditorModel)
		_ = model.View()
	}
}
