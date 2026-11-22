package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/editor"
)

func goEditor(text string) EditorModel {
	model := NewEditorModel(editor.Document{Path: "x.go", Lines: strings.Split(text, "\n"), LineEnding: "\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	return updated.(EditorModel)
}

func TestSyntaxCache_HighlightsOncePerRevision_WhenScreenIsDrawnRepeatedly(t *testing.T) {
	model := goEditor("package x\n\nfunc f() {}\n")
	model.View()
	first := model.syntax.spans
	if len(first) == 0 {
		t.Fatal("a Go file must be highlighted")
	}
	model.View()
	if &model.syntax.spans[0] != &first[0] {
		t.Fatal("drawing the screen again must reuse the colors, not compute them again")
	}
}

func TestSyntaxCache_KeepsOldColorsWhileTyping_AndRefreshesWhenTypingPauses(t *testing.T) {
	model := goEditor("package x\n\nfunc f() {}\n")
	model.View()
	before := model.syntax.revision
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	model = updated.(EditorModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model = updated.(EditorModel)
	model.View()
	if model.syntax.revision != before {
		t.Fatal("while typing the screen must not wait for new colors")
	}
	if cmd == nil && !model.syntax.scheduled {
		t.Fatal("a refresh must be scheduled")
	}
	// typing paused: the tick of the cache refreshes the colors
	updated, _ = model.Update(syntaxTickMsg{cache: model.syntax})
	model = updated.(EditorModel)
	updated, _ = model.Update(syntaxTickMsg{cache: model.syntax}) // the first tick may only reschedule: edits came after it was set
	model = updated.(EditorModel)
	model.View()
	if model.syntax.revision != model.vim.Revision() {
		t.Fatalf("colors are for revision %d, the text is at %d", model.syntax.revision, model.vim.Revision())
	}
}

func TestSyntaxCache_RefreshesAtOnce_WhenTheTickNeverArrives(t *testing.T) {
	model := goEditor("package x\n")
	model.View()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model = updated.(EditorModel)
	model.syntax.scheduled, model.syntax.scheduledAt = true, time.Now().Add(-2*syntaxMaxWait)
	model.View()
	if model.syntax.revision != model.vim.Revision() {
		t.Fatal("stale colors must not stay forever when the tick is lost")
	}
}
