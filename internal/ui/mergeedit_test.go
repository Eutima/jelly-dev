package ui

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/vimbuf"
)

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

func typeModel(model MergeModel, msgs ...tea.KeyMsg) MergeModel {
	for _, msg := range msgs {
		updated, _ := model.Update(msg)
		model = updated.(MergeModel)
	}
	return model
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

// vimKeys feeds keys to the model like a user typing: plain characters, and <esc> <cr> <bs> in angle brackets.
func vimKeys(model MergeModel, keys string) MergeModel {
	for len(keys) > 0 {
		var msg tea.KeyMsg
		switch {
		case strings.HasPrefix(keys, "<esc>"):
			msg, keys = tea.KeyMsg{Type: tea.KeyEsc}, keys[len("<esc>"):]
		case strings.HasPrefix(keys, "<cr>"):
			msg, keys = tea.KeyMsg{Type: tea.KeyEnter}, keys[len("<cr>"):]
		case strings.HasPrefix(keys, "<bs>"):
			msg, keys = tea.KeyMsg{Type: tea.KeyBackspace}, keys[len("<bs>"):]
		default:
			r := []rune(keys)
			msg, keys = tea.KeyMsg{Type: tea.KeyRunes, Runes: r[:1]}, string(r[1:])
		}
		updated, _ := model.Update(msg)
		model = updated.(MergeModel)
	}
	return model
}

func TestMergeEditor_OpensOnCursorLine_WhenEPressed(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "j", "e")
	if !model.editing {
		t.Fatal("expected the editor")
	}
	if row, _ := model.vim.Cursor(); row != 2 {
		t.Fatalf("editor cursor on line %d, want 3", row+1)
	}
	if !strings.Contains(model.View(), "-- NORMAL --") {
		t.Fatalf("view lacks the mode:\n%s", model.View())
	}
}

func TestMergeEditor_EditsSeveralLinesAndAppliesWithWriteQuit(t *testing.T) {
	model, s, outcome := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "e")
	model = vimKeys(model, "ddjAx<esc>jyyP:wq<cr>") // delete a, append x to c, duplicate d
	want := split("b\ncx\nd\nd\ne\nf\n")
	if model.editing || !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("editing %v result %v", model.editing, model.Result())
	}
	if model = mergePress(model, "s"); !outcome.Saved || !reflect.DeepEqual(s.saved[0], want) {
		t.Fatalf("saved %v", s.saved)
	}
}

func TestMergeEditor_EditsOnlyChunkUnderCursor_WhenCursorOnChange(t *testing.T) {
	base, ours := "a\nb\nc\nd\ne\nf\ng\n", "a\nB\nc\nd\ne\nF\ng\n"
	model, _, _ := editModel(t, base, ours, base)
	model = mergePress(model, "n", "e")        // the first change: B
	model = vimKeys(model, "ddAy<esc>:wq<cr>") // only the chunk can change
	want := split("a\ny\nc\nd\ne\nF\ng\n")
	if !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("result %v, want %v", model.Result(), want)
	}
	if model = mergePress(model, "u"); !reflect.DeepEqual(model.Result(), split(ours)) {
		t.Fatalf("one undo must revert the write: %v", model.Result())
	}
}

func TestMergeEditor_ShowsAllOtherLinesReadOnly_WhenEditingOneChunk(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, "L"+strconv.Itoa(i))
	}
	base := strings.Join(lines, "\n") + "\n"
	lines[14] = "CHANGED"
	model, _, _ := editModel(t, base, strings.Join(lines, "\n")+"\n", base)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	model = mergePress(updated.(MergeModel), "n", "e")
	view := model.View()
	for _, want := range []string{"L1 ", "L14", "CHANGED", "L16", "L30"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	// the cursor stays in the edited chunk: context lines cannot be edited or deleted
	model = vimKeys(model, "kkkxjjjx:wq<cr>") // x on a read-only line changes nothing
	got := model.Result()
	if len(got) != 30 || got[13] != "L14" || got[14] == "CHANGED" || got[15] != "L16" || got[0] != "L1" || got[29] != "L30" {
		t.Fatalf("context lines must stay as they are: %v", got)
	}
}

func TestMergeEditor_ResolvesConflict_WhenConflictLinesEdited(t *testing.T) {
	model, s, outcome := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	if model.Unresolved() != 1 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
	model = mergePress(model, "n", "e") // the editor starts on the conflict's base line "b"
	model = vimKeys(model, "A-merged<esc>:wq<cr>")
	if model.Unresolved() != 0 || !reflect.DeepEqual(model.Result(), split("a\nb-merged\nc\n")) {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
	if model = mergePress(model, "s"); !outcome.Saved || len(s.saved) != 1 {
		t.Fatalf("saved %v", s.saved)
	}
}

func TestMergeEditor_KeepsConflictOpen_WhenOtherLineEdited(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	model = mergePress(model, "e")
	model = vimKeys(model, "x:wq<cr>") // edits "a" only
	if model.Unresolved() != 1 || model.Result()[0] != "" {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
}

func TestMergeEditor_ChangesNothing_WhenOpenedAndLeftWithoutEditing(t *testing.T) {
	model, _, _ := editModel(t, "a\x1bb\nc\n", "a\x1bb\nc\n", "a\x1bb\nc\n")
	model = mergePress(model, "e")
	if view := model.View(); strings.Contains(view, "\x1bb") {
		t.Fatalf("control character must not reach the terminal while editing: %q", view)
	}
	model = vimKeys(model, ":wq<cr>")
	if model.editing || !reflect.DeepEqual(model.Result(), []string{"a\x1bb", "c"}) || len(model.history) != 0 {
		t.Fatalf("result %q history %d", model.Result(), len(model.history))
	}
}

func TestMergeEditor_RefusesQuit_WhenTextModified(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "x:q<cr>")
	if !model.editing || !strings.Contains(model.View(), "E37") {
		t.Fatalf("editing %v\n%s", model.editing, model.View())
	}
	model = vimKeys(model, ":q!<cr>")
	if model.editing || !reflect.DeepEqual(model.Result(), split(editBase)) || len(model.history) != 0 {
		t.Fatalf("editing %v result %v history %d", model.editing, model.Result(), len(model.history))
	}
}

func TestMergeEditor_WriteKeepsEditorOpen_WhenOnlyWriteCommand(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "x:w<cr>")
	if !model.editing || model.Result()[0] != "" {
		t.Fatalf("editing %v result %v", model.editing, model.Result())
	}
	if model = vimKeys(model, ":q<cr>"); model.editing {
		t.Fatal(":q must leave after :w")
	}
}

func TestMergeEditor_TreatsQAsText_WhenInserting(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "Aq<esc>:wq<cr>")
	if cursorLine(model) != "aq" {
		t.Fatalf("got %q", cursorLine(model))
	}
}

func TestMergeEditor_InsertsLineBelowInInsertMode_WhenIPressedInMergeView(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = mergePress(model, "j", "i")
	if !model.editing || model.vim.Mode() != vimbuf.Insert {
		t.Fatalf("editing %v mode %v", model.editing, model.vim.Mode())
	}
	model = vimKeys(model, "new<esc>:wq<cr>")
	if !reflect.DeepEqual(model.Result(), split("a\nb\nnew\nc\nd\ne\nf\n")) {
		t.Fatalf("result %v", model.Result())
	}
	if model = mergePress(model, "u"); !reflect.DeepEqual(model.Result(), split(editBase)) {
		t.Fatalf("one undo must remove the inserted line: %v", model.Result())
	}
}

func TestMergeEditor_DeletesAllLines_WhenWholeFileRemoved(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "dG:wq<cr>")
	if len(model.Result()) != 0 {
		t.Fatalf("result %v", model.Result())
	}
}

func TestMergeEditor_WorksOnEmptyResult(t *testing.T) {
	model, _, _ := editModel(t, "a\n", "a\n", "a\n")
	model = vimKeys(mergePress(model, "e"), "dd:w<cr>ix<esc>:wq<cr>")
	if !reflect.DeepEqual(model.Result(), []string{"x"}) {
		t.Fatalf("result %v", model.Result())
	}
}

func TestMergeEditor_ScrollsToCursor_WhenFileLongerThanScreen(t *testing.T) {
	long := strings.Repeat("line\n", 40)
	model, _, _ := editModel(t, long, long, long)
	model = vimKeys(mergePress(model, "e"), "G")
	if model.editTop <= 0 || !strings.Contains(model.View(), "40 ") {
		t.Fatalf("top %d\n%s", model.editTop, model.View())
	}
}

func TestMergeEditor_ShowsVisualSelectionAndCommandLine(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "Vj")
	if !strings.Contains(model.View(), "-- VISUAL LINE --") {
		t.Fatalf("view lacks the visual mode:\n%s", model.View())
	}
	model = vimKeys(model, "<esc>:w")
	if !strings.Contains(model.View(), ":w") {
		t.Fatalf("view lacks the command line:\n%s", model.View())
	}
}

func TestMergeEditor_FitsWidth_WhenSizesVary(t *testing.T) {
	for _, width := range []int{20, 41, 80, 120} {
		model, _, _ := editModel(t, editBase, editOurs, editTheirs)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 10})
		model = vimKeys(mergePress(updated.(MergeModel), "j", "e"), "A"+strings.Repeat("long ", 30))
		for i, line := range strings.Split(model.View(), "\n") {
			if got := lipgloss.Width(line); got > width && width >= 2*mergeGutterWidth {
				t.Fatalf("width %d line %d is %d wide", width, i, got)
			}
		}
	}
}

func TestMergeEditor_DoesNotMutateEarlierCopies(t *testing.T) {
	before, _, _ := editModel(t, editBase, editOurs, editTheirs)
	after := vimKeys(mergePress(before, "e"), "x:wq<cr>")
	if !reflect.DeepEqual(before.Result(), split(editBase)) || reflect.DeepEqual(after.Result(), before.Result()) {
		t.Fatalf("before %v after %v", before.Result(), after.Result())
	}
}

func TestMergeEditor_RestoresEditedLines_WhenResolveOverridesCustomAndIsUndone(t *testing.T) {
	model, _, _ := editModel(t, "a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n")
	model = vimKeys(mergePress(model, "n", "e"), "A!<esc>:wq<cr>") // custom "b!"
	model = mergePress(model, "o")                                 // override by ours
	if got := model.Result(); got[1] != "OURS" {
		t.Fatalf("ours expected: %v", got)
	}
	if model = mergePress(model, "u"); model.Result()[1] != "b!" {
		t.Fatalf("undo must bring the hand-edited line back: %v", model.Result())
	}
}

func TestMergeModel_KeepsCursorLine_WhenAutoResolveOrTakeAllPressed(t *testing.T) {
	var base, ours, theirs []string
	for i := 1; i <= 40; i++ {
		line := "L" + strconv.Itoa(i)
		base, ours, theirs = append(base, line), append(ours, line), append(theirs, line)
	}
	ours[29], theirs[29] = "L30 ours", "L30 theirs" // a real conflict far below the cursor
	ours[5], theirs[5] = "L6 ", "L6"                // a whitespace-only conflict: auto-resolves
	join := func(lines []string) string { return strings.Join(lines, "\n") + "\n" }
	for _, key := range []string{"m", "ctrl+o"} {
		model, _, _ := editModel(t, join(base), join(ours), join(theirs))
		model = mergePress(model, "j", "j", "j") // cursor on L4
		offset := model.offset
		updated, _ := model.Update(keyNamed(key))
		model = updated.(MergeModel)
		if cursorLine(model) != "L4" || model.offset != offset {
			t.Fatalf("%s: cursor on %q (offset %d, was %d), want L4 where it was", key, cursorLine(model), model.offset, offset)
		}
	}
}

func keyNamed(name string) tea.KeyMsg {
	if name == "ctrl+o" {
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func plainView(model MergeModel) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(model.View(), "")
}

func TestMergeView_WrapsLongLines_WhenWrapOn(t *testing.T) {
	long := strings.Repeat("0123456789", 12) // 120 characters in a pane of about 30 columns
	model, _, _ := editModel(t, "a\n"+long+"\nz\n", "a\n"+long+"\nz\n", "a\n"+long+"\nz\n")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	view := plainView(updated.(MergeModel))
	if strings.Contains(view, "…") {
		t.Fatalf("nothing may be cut while wrapping:\n%s", view)
	}
	if got := strings.Count(view, "9"); got != 12*3 { // the text holds twelve nines, none of it may be lost in any pane
		t.Fatalf("all text must be visible in the three panes, found %d nines:\n%s", got, view)
	}
	for i, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 100 {
			t.Fatalf("line %d is %d wide", i, lipgloss.Width(line))
		}
	}
}

func TestMergeView_CutsLongLines_WhenWrapToggledOff(t *testing.T) {
	long := strings.Repeat("0123456789", 12)
	model, _, _ := editModel(t, "a\n"+long+"\n", "a\n"+long+"\n", "a\n"+long+"\n")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = mergePress(updated.(MergeModel), "w")
	if view := plainView(model); !strings.Contains(view, "…") {
		t.Fatalf("lines must be cut with w off:\n%s", view)
	}
	if view := plainView(mergePress(model, "w")); strings.Contains(view, "…") {
		t.Fatalf("w must switch wrapping on again:\n%s", view)
	}
}

func TestMergeView_KeepsCursorVisible_WhenLinesWrapped(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, strings.Repeat("w", 90)+strconv.Itoa(i)) // wraps to several screen lines
	}
	text := strings.Join(lines, "\n") + "\n"
	model, _, _ := editModel(t, text, text, text)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model = updated.(MergeModel)
	for _, key := range []string{"G", "g", "j", "j", "j", "j", "j", "G"} {
		model = mergePress(model, key)
		if !model.rowFullyVisible(model.cursor) {
			t.Fatalf("after %q the cursor row %d is not visible (offset %d)", key, model.cursor, model.offset)
		}
	}
	if view := plainView(model); !strings.Contains(view, "19") {
		t.Fatalf("the last line must be on screen at the end:\n%s", view)
	}
	if got := len(strings.Split(model.View(), "\n")); got != 14 {
		t.Fatalf("the view must fill exactly the terminal height, got %d lines", got)
	}
}

func ctrlD(model MergeModel) MergeModel {
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	return updated.(MergeModel)
}

func TestMergeEditor_LeavesAtOnce_WhenCtrlDAndNothingChanged(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = ctrlD(mergePress(model, "e"))
	if model.editing || len(model.history) != 0 {
		t.Fatalf("editing %v history %d", model.editing, len(model.history))
	}
}

func TestMergeEditor_AsksToApply_WhenCtrlDAndTextChanged(t *testing.T) {
	for name, tc := range map[string]struct {
		answer     string
		wantEdit   bool
		wantResult []string
	}{
		"y applies":      {"y", false, split("\nb\nc\nd\ne\nf\n")},
		"n discards":     {"n", false, split(editBase)},
		"other keeps on": {"z", true, split(editBase)},
		"esc keeps on":   {"<esc>", true, split(editBase)},
	} {
		model, _, _ := editModel(t, editBase, editOurs, editTheirs)
		model = vimKeys(mergePress(model, "e"), "ggx") // "a" becomes ""
		model = ctrlD(model)
		if !model.editing || !strings.Contains(model.View(), "Apply the changes before leaving?") {
			t.Fatalf("%s: prompt expected (editing %v):\n%s", name, model.editing, model.View())
		}
		model = vimKeys(model, tc.answer)
		if model.editing != tc.wantEdit || !reflect.DeepEqual(model.Result(), tc.wantResult) {
			t.Errorf("%s: editing %v result %v", name, model.editing, model.Result())
		}
	}
}

func TestMergeEditor_AsksAlsoInInsertMode_WhenCtrlDPressed(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = ctrlD(vimKeys(mergePress(model, "e"), "Ax"))
	if !model.editing || !strings.Contains(model.View(), "Apply the changes") {
		t.Fatalf("prompt expected:\n%s", model.View())
	}
	if model = vimKeys(model, "n"); model.editing || !reflect.DeepEqual(model.Result(), split(editBase)) {
		t.Fatalf("editing %v result %v", model.editing, model.Result())
	}
}

func TestMergeEditor_LeavesAtOnce_WhenCtrlDAfterWrite(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = ctrlD(vimKeys(mergePress(model, "e"), "x:w<cr>"))
	if model.editing || model.Result()[0] != "" {
		t.Fatalf("editing %v result %v", model.editing, model.Result())
	}
}

func contextModel(t *testing.T) MergeModel {
	t.Helper()
	base, ours := "a\nb\nc\nd\ne\nf\ng\n", "a\nb\nC\nd\ne\nf\ng\n"
	model, _, _ := editModel(t, base, ours, base)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return mergePress(updated.(MergeModel), "n", "e") // the editable chunk is the changed line C
}

func TestMergeEditor_MovesOverReadOnlyLines_WhenCursorLeavesChunk(t *testing.T) {
	model := contextModel(t)
	if model.editCtx != -1 || model.vim.LineCount() != 1 {
		t.Fatalf("ctx %d lines %d", model.editCtx, model.vim.LineCount())
	}
	model = vimKeys(model, "jj")
	if model.editCtx != 4 { // a b C d e: the second line below the chunk
		t.Fatalf("j j must walk into the lines below, ctx %d", model.editCtx)
	}
	if !strings.Contains(model.View(), "READ-ONLY") {
		t.Fatalf("status lacks the read-only marker:\n%s", model.View())
	}
	model = vimKeys(model, "kk")
	if model.editCtx != -1 {
		t.Fatalf("k must come back into the chunk, ctx %d", model.editCtx)
	}
	model = vimKeys(model, "kkk")
	if model.editCtx != 0 {
		t.Fatalf("k k must reach the top, ctx %d", model.editCtx)
	}
	if model = vimKeys(model, "k"); model.editCtx != 0 {
		t.Fatalf("the cursor must stop at the top, ctx %d", model.editCtx)
	}
}

func TestMergeEditor_RefusesChangesOnReadOnlyLine(t *testing.T) {
	model := contextModel(t)
	model = vimKeys(model, "kxiddAz")
	if model.editCtx < 0 || !strings.Contains(model.View(), "Read-only line") {
		t.Fatalf("ctx %d:\n%s", model.editCtx, model.View())
	}
	if model.vim.Modified() || model.vim.Mode() != vimbuf.Normal {
		t.Fatalf("a read-only line must not change anything: modified %v mode %v", model.vim.Modified(), model.vim.Mode())
	}
	model = vimKeys(model, "j") // back into the chunk
	model = vimKeys(model, "Ax<esc>:wq<cr>")
	if got := model.Result(); !reflect.DeepEqual(got, split("a\nb\nCx\nd\ne\nf\ng\n")) {
		t.Fatalf("result %v", got)
	}
}

func TestMergeEditor_JumpsWithGAndGG_WhenReadOnlyLinesAround(t *testing.T) {
	model := contextModel(t)
	if model = vimKeys(model, "G"); model.editCtx != 6 {
		t.Fatalf("G in the chunk's last line must go to the end, ctx %d", model.editCtx)
	}
	model = vimKeys(model, "gg")
	if model.editCtx != 0 {
		t.Fatalf("gg on a read-only line must go to the top, ctx %d", model.editCtx)
	}
	if model = vimKeys(model, "G"); model.editCtx != 6 {
		t.Fatalf("G on a read-only line must go to the end, ctx %d", model.editCtx)
	}
}

func TestMergeEditor_AllowsCommandLineOnReadOnlyLine(t *testing.T) {
	model := contextModel(t)
	model = vimKeys(model, "j:q<cr>")
	if model.editing {
		t.Fatal(":q must leave from a read-only line")
	}
}

func TestMergeEditor_KeepsCursorLineVisible_WhenWalkingOverContext(t *testing.T) {
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, "L"+strconv.Itoa(i))
	}
	base := strings.Join(lines, "\n") + "\n"
	lines[29] = "CHANGED"
	model, _, _ := editModel(t, base, strings.Join(lines, "\n")+"\n", base)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	model = mergePress(updated.(MergeModel), "n", "e")
	model = vimKeys(model, "G")
	if !strings.Contains(plainView(model), "L60") {
		t.Fatalf("the last line must be shown after G:\n%s", plainView(model))
	}
	model = vimKeys(model, "gg")
	if !strings.Contains(plainView(model), "L1 ") {
		t.Fatalf("the first line must be shown after gg:\n%s", plainView(model))
	}
}
