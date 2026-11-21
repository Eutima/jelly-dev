package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/config"
)

// ---- lineEditor

func key(runes string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)} }

func typeKeys(editor lineEditor, msgs ...tea.KeyMsg) (lineEditor, editAction) {
	action := editContinue
	for _, msg := range msgs {
		editor, action = editor.handle(msg)
	}
	return editor, action
}

func TestLineEditor_InsertsAtCursor_WhenTyping(t *testing.T) {
	editor, _ := typeKeys(newLineEditor("ac"), tea.KeyMsg{Type: tea.KeyLeft}, key("b"))
	if editor.String() != "abc" || editor.cursor != 2 {
		t.Fatalf("got %q cursor %d", editor.String(), editor.cursor)
	}
}

func TestLineEditor_DeletesAroundCursor_WhenBackspaceAndDeletePressed(t *testing.T) {
	editor, _ := typeKeys(newLineEditor("abcd"), tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyBackspace})
	if editor.String() != "abd" {
		t.Fatalf("backspace: %q", editor.String())
	}
	editor, _ = typeKeys(editor, tea.KeyMsg{Type: tea.KeyHome}, tea.KeyMsg{Type: tea.KeyDelete})
	if editor.String() != "bd" || editor.cursor != 0 {
		t.Fatalf("delete: %q cursor %d", editor.String(), editor.cursor)
	}
}

func TestLineEditor_IgnoresEdges_WhenBackspaceAtStartAndDeleteAtEnd(t *testing.T) {
	editor, _ := typeKeys(newLineEditor("ab"), tea.KeyMsg{Type: tea.KeyDelete})
	if editor.String() != "ab" {
		t.Fatalf("delete at end: %q", editor.String())
	}
	editor, _ = typeKeys(editor, tea.KeyMsg{Type: tea.KeyHome}, tea.KeyMsg{Type: tea.KeyBackspace})
	if editor.String() != "ab" || editor.cursor != 0 {
		t.Fatalf("backspace at start: %q", editor.String())
	}
}

func TestLineEditor_DeletesToStartAndEnd_WhenCtrlUAndCtrlKPressed(t *testing.T) {
	editor, _ := typeKeys(newLineEditor("abcdef"), tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyLeft},
		tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyCtrlU})
	if editor.String() != "def" || editor.cursor != 0 {
		t.Fatalf("ctrl+u: %q", editor.String())
	}
	editor, _ = typeKeys(editor, tea.KeyMsg{Type: tea.KeyRight}, tea.KeyMsg{Type: tea.KeyCtrlK})
	if editor.String() != "d" {
		t.Fatalf("ctrl+k: %q", editor.String())
	}
}

func TestLineEditor_ReturnsActions_WhenEnterAndEscapePressed(t *testing.T) {
	if _, action := typeKeys(newLineEditor("x"), tea.KeyMsg{Type: tea.KeyEnter}); action != editApply {
		t.Fatal("enter must apply")
	}
	for _, msg := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		if _, action := typeKeys(newLineEditor("x"), msg); action != editCancel {
			t.Fatalf("%v must cancel", msg)
		}
	}
}

func TestLineEditor_DropsLineBreaksAndControlCharacters_WhenPastedOrTyped(t *testing.T) {
	editor, _ := typeKeys(newLineEditor(""), key("a\nb\r\x1b[31mc\u2028d"))
	if editor.String() != "ab[31mcd" {
		t.Fatalf("got %q", editor.String())
	}
	// The existing text is kept exactly (so opening and applying a line never alters the file); only
	// inserted text is sanitized, and control characters are shown as a placeholder.
	original := newLineEditor("x\ty\x07")
	if original.String() != "x\ty\x07" {
		t.Fatalf("existing text must be kept: %q", original.String())
	}
	if view := original.view(20, lipgloss.NewStyle(), lipgloss.NewStyle()); strings.Contains(view, "\x07") {
		t.Fatalf("control character must not be drawn: %q", view)
	}
}

func TestLineEditor_KeepsTabsAndSpaces_WhenTyped(t *testing.T) {
	editor, _ := typeKeys(newLineEditor(""), tea.KeyMsg{Type: tea.KeyTab}, tea.KeyMsg{Type: tea.KeySpace}, key("x"))
	if editor.String() != "\t x" {
		t.Fatalf("got %q", editor.String())
	}
}

func TestLineEditor_ViewHasExactWidthAndKeepsCursorVisible_WhenTextLongerThanWidth(t *testing.T) {
	base := lipgloss.NewStyle()
	editor := newLineEditor(strings.Repeat("0123456789", 5)) // cursor at the end
	view := editor.view(12, base, base.Reverse(true))
	if lipgloss.Width(view) != 12 {
		t.Fatalf("width %d", lipgloss.Width(view))
	}
	editor, _ = typeKeys(editor, tea.KeyMsg{Type: tea.KeyHome})
	if view = editor.view(12, base, base.Reverse(true)); lipgloss.Width(view) != 12 || !strings.HasPrefix(view, "0123") {
		t.Fatalf("view at home: %q", view)
	}
}

func TestLineEditor_ViewFitsWidth_WhenTabsAndWideRunes(t *testing.T) {
	base := lipgloss.NewStyle()
	for _, width := range []int{1, 2, 5, 20} {
		editor := newLineEditor("\t日本語\tx")
		if got := lipgloss.Width(editor.view(width, base, base)); got != width {
			t.Fatalf("width %d rendered %d", width, got)
		}
	}
	if newLineEditor("x").view(0, base, base) != "" {
		t.Fatal("zero width must render nothing")
	}
}

// ---- MergeModel editing

const (
	editBase   = "a\nb\nc\nd\ne\nf\n"
	editOurs   = "a\nb\nc\nd\ne\nf\n"
	editTheirs = "a\nb\nc\nd\ne\nf\n"
)

func editModel(t *testing.T, base, ours, theirs string) (MergeModel, *saver, *MergeOutcome) {
	t.Helper()
	s := &saver{}
	model, outcome := newMergeTestModel(base, ours, theirs, s)
	return model, s, outcome
}

func cursorLine(model MergeModel) string { return model.rows[model.cursor].cells[paneResult].Text }

func TestMergeModel_MovesCursorByLines_WhenJAndKPressed(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "j")
	if cursorLine(model) != "c" {
		t.Fatalf("cursor on %q", cursorLine(model))
	}
	model = mergePress(model, "k", "G")
	if cursorLine(model) != "f" || model.cursor != len(model.rows)-1 {
		t.Fatalf("cursor on %q row %d", cursorLine(model), model.cursor)
	}
	if model = mergePress(model, "j"); model.cursor != len(model.rows)-1 {
		t.Fatal("cursor must stop at the last row")
	}
	if model = mergePress(model, "g"); model.cursor != 0 {
		t.Fatal("g must go to the first row")
	}
}

func TestMergeModel_ScrollsToCursor_WhenCursorLeavesVisibleRows(t *testing.T) {
	long := strings.Repeat("line\n", 40)
	model, _, _ := editModel(t, long, long, long)
	model = mergePress(model, "G")
	if model.offset <= 0 || model.cursor < model.offset || model.cursor >= model.offset+model.bodyHeight() {
		t.Fatalf("cursor %d offset %d body %d", model.cursor, model.offset, model.bodyHeight())
	}
}

func TestMergeModel_EditsLineAndSaves_WhenEnterPressedInEditor(t *testing.T) {
	model, s, outcome := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "e") // edit line "b"
	if !model.editing {
		t.Fatal("expected edit mode")
	}
	model = typeModel(model, tea.KeyMsg{Type: tea.KeyBackspace}, key("B!"), tea.KeyMsg{Type: tea.KeyEnter})
	want := split("a\nB!\nc\nd\ne\nf\n")
	if model.editing || !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("editing %v result %v", model.editing, model.Result())
	}
	if model = mergePress(model, "s"); !outcome.Saved || !reflect.DeepEqual(s.saved[0], want) {
		t.Fatalf("saved %v", s.saved)
	}
}

func typeModel(model MergeModel, msgs ...tea.KeyMsg) MergeModel {
	for _, msg := range msgs {
		updated, _ := model.Update(msg)
		model = updated.(MergeModel)
	}
	return model
}

func TestMergeModel_KeepsLine_WhenEditCancelledWithEscape(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "e")
	model = typeModel(model, key("zzz"), tea.KeyMsg{Type: tea.KeyEsc})
	if model.editing || !reflect.DeepEqual(model.Result(), split(editBase)) || len(model.history) != 0 {
		t.Fatalf("result %v history %d", model.Result(), len(model.history))
	}
}

func TestMergeModel_TreatsQAsText_WhenEditing(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "e")
	model = typeModel(model, key("q"), tea.KeyMsg{Type: tea.KeyEnter})
	if cursorLine(model) != "aq" {
		t.Fatalf("got %q", cursorLine(model))
	}
}

func TestMergeModel_ResolvesConflict_WhenLineEditedByHand(t *testing.T) {
	model, s, outcome := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	if model.Unresolved() != 1 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
	model = mergePress(model, "n", "e") // cursor on the conflict's base line "b"
	model = typeModel(model, key("-merged"), tea.KeyMsg{Type: tea.KeyEnter})
	if model.Unresolved() != 0 || !reflect.DeepEqual(model.Result(), split("a\nb-merged\nc\n")) {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
	if model = mergePress(model, "s"); !outcome.Saved || len(s.saved) != 1 {
		t.Fatalf("saved %v", s.saved)
	}
}

func TestMergeModel_DeletesLine_WhenDPressed(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "d")
	if !reflect.DeepEqual(model.Result(), split("a\nc\nd\ne\nf\n")) || cursorLine(model) != "c" {
		t.Fatalf("result %v cursor on %q", model.Result(), cursorLine(model))
	}
}

func TestMergeModel_DeletesLastLineOfChunk_WhenCursorOnFinalRow(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\n", "a\nb\n", "a\nb\n")
	model = mergePress(model, "G", "d")
	if !reflect.DeepEqual(model.Result(), []string{"a"}) || model.cursor != len(model.rows)-1 {
		t.Fatalf("result %v cursor %d rows %d", model.Result(), model.cursor, len(model.rows))
	}
}

func TestMergeModel_InsertsAndEditsNewLine_WhenIPressed(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "i") // insert below "b"
	if !model.editing || model.editRow != model.cursor {
		t.Fatalf("editing %v row %d cursor %d", model.editing, model.editRow, model.cursor)
	}
	model = typeModel(model, key("new"), tea.KeyMsg{Type: tea.KeyEnter})
	if !reflect.DeepEqual(model.Result(), split("a\nb\nnew\nc\nd\ne\nf\n")) {
		t.Fatalf("result %v", model.Result())
	}
	if model = mergePress(model, "u"); !reflect.DeepEqual(model.Result(), split(editBase)) {
		t.Fatalf("one undo must remove the inserted line: %v", model.Result())
	}
}

func TestMergeModel_RemovesInsertedLine_WhenInsertCancelled(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "i")
	model = typeModel(model, key("x"), tea.KeyMsg{Type: tea.KeyEsc})
	if model.editing || !reflect.DeepEqual(model.Result(), split(editBase)) || len(model.history) != 0 {
		t.Fatalf("result %v history %d", model.Result(), len(model.history))
	}
}

func TestMergeModel_UndoesEditStepByStep_WhenUPressedRepeatedly(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "e")
	model = typeModel(model, key("1"), tea.KeyMsg{Type: tea.KeyEnter}) // a1
	model = mergePress(model, "j", "e")
	model = typeModel(model, key("2"), tea.KeyMsg{Type: tea.KeyEnter}) // b2
	if got := model.Result(); got[0] != "a1" || got[1] != "b2" {
		t.Fatalf("result %v", got)
	}
	model = mergePress(model, "u")
	if got := model.Result(); got[0] != "a1" || got[1] != "b" {
		t.Fatalf("after first undo %v", got)
	}
	model = mergePress(model, "u")
	if !reflect.DeepEqual(model.Result(), split(editBase)) {
		t.Fatalf("after second undo %v", model.Result())
	}
}

func TestMergeModel_RestoresEditedLines_WhenResolveOverridesCustomAndIsUndone(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	model = mergePress(model, "n", "e")
	model = typeModel(model, key("!"), tea.KeyMsg{Type: tea.KeyEnter}) // custom "b!"
	model = mergePress(model, "o")                                     // override by ours
	if got := model.Result(); got[1] != "OURS" {
		t.Fatalf("ours expected: %v", got)
	}
	if model = mergePress(model, "u"); model.Result()[1] != "b!" {
		t.Fatalf("undo must bring the hand-edited line back: %v", model.Result())
	}
}

func TestMergeModel_ExplainsWhy_WhenNothingToEditOnFillerRow(t *testing.T) {
	// ours deletes "b"; with theirs unchanged the result drops it too, so the ours pane keeps a row
	// where the result has none.
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nc\n", "a\nb\nc\n")
	row := -1
	for i, r := range model.rows {
		if r.cells[paneResult].Kind == CellFiller {
			row = i
		}
	}
	if row < 0 {
		t.Skip("no filler row in this layout")
	}
	model.cursor = row
	model = mergePress(model, "e")
	if model.editing || !strings.Contains(model.View(), "No result line here to edit") {
		t.Fatalf("editing %v\n%s", model.editing, model.View())
	}
	model = mergePress(model, "d")
	if !strings.Contains(model.View(), "No result line here to delete") {
		t.Fatalf("delete on a filler row must explain itself:\n%s", model.View())
	}
}

func TestMergeModel_InsertsIntoChunk_WhenCursorOnFillerRow(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nc\n", "a\nb\nc\n")
	for i, r := range model.rows {
		if r.cells[paneResult].Kind == CellFiller {
			model.cursor = i
		}
	}
	model = mergePress(model, "i")
	model = typeModel(model, key("X"), tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(strings.Join(model.Result(), ","), "X") {
		t.Fatalf("result %v", model.Result())
	}
}

func TestMergeModel_SelectsChangeUnderCursor_WhenMovingByLine(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	if model.current != -1 {
		t.Fatalf("current %d", model.current)
	}
	model = mergePress(model, "j") // cursor moves onto the conflict
	if model.current != 0 {
		t.Fatalf("cursor on the conflict must select it, current %d", model.current)
	}
	model = mergePress(model, "o") // resolves without pressing n first
	if model.Unresolved() != 0 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
}

func TestMergeView_ShowsEditorAndHints_WhenEditing(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "e")
	view := model.View()
	for _, want := range []string{"Editing line 2", "enter apply", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestMergeView_FitsWidthWhileEditing_WhenSizesVary(t *testing.T) {
	for _, width := range []int{20, 41, 80, 120} {
		model, _, _ := editModel(t, editBase, editOurs, editTheirs)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 10})
		model = mergePress(updated.(MergeModel), "j", "e")
		model = typeModel(model, key(strings.Repeat("long ", 30)))
		for i, line := range strings.Split(model.View(), "\n") {
			if got := lipgloss.Width(line); got > width && width >= 2*mergeGutterWidth {
				t.Fatalf("width %d line %d is %d wide", width, i, got)
			}
		}
	}
}

func TestMergeView_HighlightsEditedResult_WhenGoFile(t *testing.T) {
	t.Parallel()
	model, _, _ := editModel(t, "package a\n", "package a\n", "package a\n")
	model = model.WithHighlight("a.go").WithTheme(trueColorTheme(t, config.ThemeDark))
	model = mergePress(model, "e")
	model = typeModel(model, tea.KeyMsg{Type: tea.KeyCtrlU}, key("func f() {}"), tea.KeyMsg{Type: tea.KeyEnter})
	row := model.rows[0].cells[paneResult]
	if row.Text != "func f() {}" || len(row.Syntax) == 0 {
		t.Fatalf("edited line must be re-highlighted: %+v", row)
	}
}

func TestMergeModel_DoesNotMutateEarlierCopies_WhenEditing(t *testing.T) {
	before, _, _ := editModel(t, editBase, editOurs, editTheirs)
	after := mergePress(before, "e")
	after = typeModel(after, key("x"), tea.KeyMsg{Type: tea.KeyEnter})
	if !reflect.DeepEqual(before.Result(), split(editBase)) || reflect.DeepEqual(after.Result(), before.Result()) {
		t.Fatalf("before %v after %v", before.Result(), after.Result())
	}
}

func TestMergeModel_KeepsLineUntouched_WhenOpenedAndAppliedWithoutTyping(t *testing.T) {
	model, _, _ := editModel(t, "a\x1bb\nc\n", "a\x1bb\nc\n", "a\x1bb\nc\n")
	model = mergePress(model, "e")
	if view := model.View(); strings.Contains(view, "\x1bb") {
		t.Fatalf("control character must not reach the terminal while editing: %q", view)
	}
	model = typeModel(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !reflect.DeepEqual(model.Result(), []string{"a\x1bb", "c"}) || len(model.history) != 0 {
		t.Fatalf("result %q history %d", model.Result(), len(model.history))
	}
}

func TestMergeModel_JumpsToNextChangeBelowCursor_WhenNPressedOnUnchangedLine(t *testing.T) {
	base := "a\nb\nc\nd\ne\nf\ng\n"
	ours := "a\nb\nC\nd\ne\nF\ng\n"
	model, _, _ := editModel(t, base, ours, base)
	model = mergePress(model, "G", "k", "k", "k") // cursor between the two changes, on "d"
	if cursorLine(model) != "d" {
		t.Fatalf("cursor on %q", cursorLine(model))
	}
	if model = mergePress(model, "n"); cursorLine(model) != "F" {
		t.Fatalf("n must go to the next change below the cursor, cursor on %q", cursorLine(model))
	}
	model = mergePress(model, "g", "j", "j", "j", "p") // cursor on "d" again: previous change is C
	if cursorLine(model) != "C" {
		t.Fatalf("p must go to the change above the cursor, cursor on %q", cursorLine(model))
	}
	if model = mergePress(model, "p"); cursorLine(model) != "F" {
		t.Fatalf("p must wrap to the last change, cursor on %q", cursorLine(model))
	}
}

func TestMergeModel_ReturnsCursorToInsertRow_WhenInsertedLineCancelled(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "j") // cursor on "c"
	before := model.cursor
	model = mergePress(model, "i")
	model = typeModel(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.cursor != before {
		t.Fatalf("cursor %d, want %d", model.cursor, before)
	}
}
