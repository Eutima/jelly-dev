package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/config"
	"github.com/eutima/jelly-diff/internal/merge"
)

type saver struct {
	saved [][]string
	err   error
}

func (s *saver) save(lines []string) error {
	if s.err != nil {
		return s.err
	}
	s.saved = append(s.saved, lines)
	return nil
}

func split(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

// newMergeTestModel merges base/ours/theirs and returns a sized model.
func newMergeTestModel(base, ours, theirs string, s *saver) (MergeModel, *MergeOutcome) {
	chunks := merge.Merge(split(base), split(ours), split(theirs))
	model, outcome := NewMergeModel(MergeTitles{Ours: "ours", Result: "result", Theirs: "theirs"}, chunks, s.save)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	return updated.(MergeModel), outcome
}

func mergePress(model MergeModel, keys ...string) MergeModel {
	for _, key := range keys {
		var msg tea.KeyMsg
		if key == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		updated, _ := model.Update(msg)
		model = updated.(MergeModel)
	}
	return model
}

const (
	baseText   = "1\n2\n3\n4\n5\n6\n7\n"
	oursText   = "1\nTWO\n3\n4\n5\n6\nOURS7\n"
	theirsText = "1\n2\n3\n4\n5\n6\nTHEIRS7\n"
)

func TestMergeModel_StartsWithAutoMergedResult_WhenChangesDoNotConflict(t *testing.T) {
	model, _ := newMergeTestModel(baseText, "1\nTWO\n3\n4\n5\n6\n7\n", "1\n2\n3\n4\n5\nSIX\n7\n", &saver{})
	want := split("1\nTWO\n3\n4\n5\nSIX\n7\n")
	if model.Unresolved() != 0 || !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
}

func TestMergeModel_KeepsBaseInResultAndCountsConflict_WhenSidesConflict(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	if model.Unresolved() != 1 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
	if got := model.Result()[len(model.Result())-1]; got != "7" {
		t.Fatalf("conflict should show base line, got %q", got)
	}
}

func TestMergeModel_RefusesToSave_WhenConflictsRemain(t *testing.T) {
	s := &saver{}
	model, outcome := newMergeTestModel(baseText, oursText, theirsText, s)
	model = mergePress(model, "s")
	if len(s.saved) != 0 || outcome.Saved || !strings.Contains(model.View(), "1 conflict(s) unresolved") {
		t.Fatalf("saved %v outcome %v\n%s", s.saved, outcome.Saved, model.View())
	}
}

func TestMergeModel_ResolvesConflictWithOurs_WhenOPressed(t *testing.T) {
	s := &saver{}
	model, outcome := newMergeTestModel(baseText, oursText, theirsText, s)
	model = mergePress(model, "n", "n", "o") // first change is TWO (auto), second is the conflict
	want := split("1\nTWO\n3\n4\n5\n6\nOURS7\n")
	if model.Unresolved() != 0 || !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
	model = mergePress(model, "s")
	if !outcome.Saved || len(s.saved) != 1 || !reflect.DeepEqual(s.saved[0], want) {
		t.Fatalf("saved %v", s.saved)
	}
}

func TestMergeModel_ResolvesWithTheirsBothAndBase_WhenKeysPressed(t *testing.T) {
	cases := map[string]string{
		"t": "THEIRS7", "r": "7",
	}
	for key, want := range cases {
		model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
		model = mergePress(model, "n", "n", key)
		if got := model.Result()[len(model.Result())-1]; got != want {
			t.Fatalf("%s: got %q want %q", key, got, want)
		}
	}
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n", "n", "b")
	if got := model.Result(); got[len(got)-2] != "OURS7" || got[len(got)-1] != "THEIRS7" {
		t.Fatalf("both: %v", got)
	}
}

func TestMergeModel_UndoesLastChoice_WhenUPressed(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n", "n", "o", "u")
	if model.Unresolved() != 1 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
	if model = mergePress(model, "u"); !strings.Contains(model.View(), "Nothing to undo") {
		t.Fatal("expected nothing-to-undo message")
	}
}

func TestMergeModel_CanRevertAutoMergedChange_WhenRPressed(t *testing.T) {
	model, _ := newMergeTestModel(baseText, "1\nTWO\n3\n4\n5\n6\n7\n", baseText, &saver{})
	model = mergePress(model, "n", "r")
	if !reflect.DeepEqual(model.Result(), split(baseText)) {
		t.Fatalf("result %v", model.Result())
	}
}

func TestMergeModel_AsksForConfirmation_WhenQuittingWithUnsavedResult(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil || !strings.Contains(updated.(MergeModel).View(), "press q again") {
		t.Fatal("first q must only warn")
	}
	_, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("second q must quit")
	}
}

func TestMergeModel_AsksForConfirmation_WhenCtrlCPressedWithUnsavedResult(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	updated, cmd := model.Update(ctrlC)
	if cmd != nil || !strings.Contains(updated.(MergeModel).View(), "ctrl+c") {
		t.Fatal("first ctrl+c must only warn")
	}
	_, cmd = updated.Update(ctrlC)
	if cmd == nil {
		t.Fatal("second ctrl+c must quit")
	}
}

func TestMergeModel_QuitsImmediately_WhenResultSaved(t *testing.T) {
	model, _ := newMergeTestModel(baseText, "1\nTWO\n3\n4\n5\n6\n7\n", baseText, &saver{})
	model = mergePress(model, "s")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit")
	}
}

func TestMergeModel_ReportsError_WhenSaveFails(t *testing.T) {
	s := &saver{err: errors.New("disk full")}
	model, outcome := newMergeTestModel(baseText, "1\nTWO\n3\n4\n5\n6\n7\n", baseText, s)
	model = mergePress(model, "s")
	if outcome.Saved || !strings.Contains(model.View(), "disk full") {
		t.Fatalf("outcome %v\n%s", outcome.Saved, model.View())
	}
}

func TestMergeModel_ExplainsNeedToSelect_WhenResolvingWithoutSelection(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	if model = mergePress(model, "o"); !strings.Contains(model.View(), "Select a change first") {
		t.Fatal("expected hint")
	}
}

func TestMergeModel_JumpsToNextUnresolvedConflict_WhenResolving(t *testing.T) {
	base := "a\nb\nc\nd\ne\nf\ng\nh\ni\n"
	ours := "a\nX\nc\nd\ne\nf\ng\nX2\ni\n"
	theirs := "a\nY\nc\nd\ne\nf\ng\nY2\ni\n"
	model, _ := newMergeTestModel(base, ours, theirs, &saver{})
	model = mergePress(model, "n", "o")
	if model.Unresolved() != 1 || model.changed[model.current] != model.changed[1] {
		t.Fatalf("current %d unresolved %d", model.current, model.Unresolved())
	}
}

func TestMergeView_ShowsThreePanesWithTitles_WhenSized(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	view := model.View()
	for _, want := range []string{"ours", "result", "theirs", "OURS7", "THEIRS7", "1 conflict remaining"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "»") || !strings.Contains(view, "«") {
		t.Fatalf("missing arrows:\n%s", view)
	}
}

func TestMergeView_FitsTerminal_WhenVariousSizes(t *testing.T) {
	for _, size := range [][2]int{{120, 12}, {60, 8}, {41, 6}, {17, 5}} {
		model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := updated.(MergeModel).View()
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("size %v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got > size[0] && size[0] >= 2*gutterWidth {
				t.Fatalf("size %v line %d is %d wide", size, i, got)
			}
		}
	}
}

func TestMergeView_StripsControlCharacters_WhenContentHasEscapes(t *testing.T) {
	model, _ := newMergeTestModel("a\n", "a\n\x1b]0;x\x07\n", "a\n", &saver{})
	if view := model.View(); strings.Contains(view, "\x1b]") || strings.Contains(view, "\x07") {
		t.Fatalf("escape in view: %q", view)
	}
}

func TestMergeModel_DoesNotMutateEarlierCopies_WhenResolving(t *testing.T) {
	before, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	selected := mergePress(before, "n", "n")
	after := mergePress(selected, "o")
	if selected.Unresolved() != 1 || after.Unresolved() != 0 {
		t.Fatalf("selected %d after %d", selected.Unresolved(), after.Unresolved())
	}
}

func TestMergeModel_CountsAsNotSaved_WhenResultChangesAfterSave(t *testing.T) {
	model, outcome := newMergeTestModel(baseText, "1\nTWO\n3\n4\n5\n6\n7\n", baseText, &saver{})
	model = mergePress(model, "s")
	if !outcome.Saved {
		t.Fatal("expected saved")
	}
	mergePress(model, "n", "r")
	if outcome.Saved {
		t.Fatal("a change after saving must clear the saved state")
	}
}

// monokaiKeyword is the true-color escape of a keyword in the dark theme's monokai style.
const monokaiKeyword = "38;2;249;38;113"

const (
	goBase   = "package main\n\nfunc a() {}\n\nfunc b() {}\n"
	goOurs   = "package main\n\nfunc a() {}\n\nfunc bOurs() {}\n"
	goTheirs = "package main\n\nfunc a() {}\n\nfunc bTheirs() {}\n"
)

func highlightedMergeModel(t *testing.T) MergeModel {
	t.Helper()
	model, _ := newMergeTestModel(goBase, goOurs, goTheirs, &saver{})
	return model.WithHighlight("main.go")
}

// findRowWith returns the first row whose cell in pane has the given text.
func findRowWith(model MergeModel, pane int, text string) (mergeRow, bool) {
	for _, row := range model.rows {
		if row.cells[pane].Text == text {
			return row, true
		}
	}
	return mergeRow{}, false
}

func TestMergeModel_HighlightsAllThreePanes_WhenGoFile(t *testing.T) {
	model := highlightedMergeModel(t)
	for pane, text := range map[int]string{paneOurs: "func bOurs() {}", paneTheirs: "func bTheirs() {}", paneResult: "func a() {}"} {
		row, ok := findRowWith(model, pane, text)
		if !ok || len(row.cells[pane].Syntax) == 0 {
			t.Fatalf("pane %d lacks syntax spans for %q: %+v", pane, text, row.cells[pane])
		}
	}
}

func TestMergeModel_RecomputesResultHighlighting_WhenConflictResolved(t *testing.T) {
	model := highlightedMergeModel(t)
	before, _ := findRowWith(model, paneResult, "func b() {}") // unresolved conflict shows the base lines
	if len(before.cells[paneResult].Syntax) == 0 {
		t.Fatal("base line in the result pane must be highlighted")
	}
	model = mergePress(model, "n", "t")
	row, ok := findRowWith(model, paneResult, "func bTheirs() {}")
	if !ok || len(row.cells[paneResult].Syntax) == 0 {
		t.Fatalf("resolved line must be highlighted too: %+v", row)
	}
}

func TestMergeModel_StaysUnhighlighted_WhenNoLexerMatches(t *testing.T) {
	model, _ := newMergeTestModel(goBase, goOurs, goTheirs, &saver{})
	model = model.WithHighlight("notes.unknownext123")
	for _, row := range model.rows {
		for _, cell := range row.cells {
			if len(cell.Syntax) != 0 {
				t.Fatalf("unexpected spans: %+v", cell)
			}
		}
	}
}

func TestMergeView_EmitsSyntaxColors_WhenDarkThemeAndGoFile(t *testing.T) {
	t.Parallel()
	model := highlightedMergeModel(t).WithTheme(trueColorTheme(t, config.ThemeDark))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	if view := updated.(MergeModel).View(); !strings.Contains(view, monokaiKeyword) {
		t.Fatalf("expected the monokai keyword color in the merge view:\n%q", view)
	}
}

func TestMergeView_FitsWidthWithSyntax_WhenSizesVary(t *testing.T) {
	t.Parallel()
	for _, width := range []int{41, 80, 120} {
		updated, _ := highlightedMergeModel(t).WithTheme(trueColorTheme(t, config.ThemeDark)).
			Update(tea.WindowSizeMsg{Width: width, Height: 10})
		for i, line := range strings.Split(updated.(MergeModel).View(), "\n") {
			if line != "" && lipgloss.Width(line) > width {
				t.Fatalf("width %d line %d is %d wide", width, i, lipgloss.Width(line))
			}
		}
	}
}

func TestMergeModel_ResolvesSimpleConflictsOnly_WhenMPressed(t *testing.T) {
	base := "intro\nthe quick brown fox\nmiddle\nchoose A\noutro\n"
	ours := "intro\nthe slow brown fox\nmiddle\nchoose OURS\noutro\n"
	theirs := "intro\nthe quick brown dog\nmiddle\nchoose THEIRS\noutro\n"
	model, _ := newMergeTestModel(base, ours, theirs, &saver{})
	if model.Unresolved() != 2 {
		t.Fatalf("setup: Unresolved() = %d, want 2", model.Unresolved())
	}

	model = mergePress(model, "m")

	if model.Unresolved() != 1 {
		t.Errorf("Unresolved() = %d, want 1 (the real conflict stays)", model.Unresolved())
	}
	want := []string{"intro", "the slow brown dog", "middle", "choose A", "outro"}
	if got := model.Result(); !reflect.DeepEqual(got, want) {
		t.Errorf("Result() = %q, want %q", got, want)
	}
	if !strings.Contains(model.status, "Auto-resolved 1 of 2") {
		t.Errorf("status = %q, want it to report 1 of 2", model.status)
	}
}

func TestMergeModel_RevertsAllWithOneUndo_WhenMagicResolvedSeveral(t *testing.T) {
	// Untouched lines between the conflicts keep them separate.
	base := "one two\nx\ny\nthree four\n"
	ours := "ONE two\nx\ny\nTHREE four\n"
	theirs := "one TWO\nx\ny\nthree FOUR\n"
	model, _ := newMergeTestModel(base, ours, theirs, &saver{})
	before := model.Unresolved()
	if before != 2 {
		t.Fatalf("setup: Unresolved() = %d, want 2", before)
	}

	model = mergePress(model, "m")
	if model.Unresolved() != 0 {
		t.Fatalf("after m: Unresolved() = %d, want 0", model.Unresolved())
	}
	model = mergePress(model, "u")

	if model.Unresolved() != before {
		t.Errorf("after one undo: Unresolved() = %d, want %d", model.Unresolved(), before)
	}
}

func TestMergeModel_ReportsNothingToDo_WhenOnlyRealConflictsLeft(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})

	model = mergePress(model, "m")

	if !strings.Contains(model.status, "Nothing to resolve automatically") || model.Unresolved() != 1 {
		t.Errorf("status = %q, Unresolved() = %d", model.status, model.Unresolved())
	}
}

func TestMergeModel_SavesAfterMagic_WhenAllConflictsWereSimple(t *testing.T) {
	s := &saver{}
	model, outcome := newMergeTestModel("the quick fox\n", "the slow fox\n", "the quick dog\n", s)

	mergePress(model, "m", "s")

	if len(s.saved) != 1 || !reflect.DeepEqual(s.saved[0], []string{"the slow dog"}) || !outcome.Saved {
		t.Errorf("saved = %q, outcome = %+v", s.saved, outcome)
	}
}

const twoConflictsBase, twoConflictsOurs, twoConflictsTheirs = "a\nb\nc\nd\ne\nf\ng\nh\ni\n",
	"a\nX\nc\nd\ne\nf\ng\nX2\ni\n", "a\nY\nc\nd\ne\nf\ng\nY2\ni\n"

func resize(model MergeModel, width, height int) MergeModel {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(MergeModel)
}

func TestMergeView_ShowsPositionAndProgress_WhenConflictSelected(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n")
	view := model.View()
	for _, want := range []string{"Jelly-Merge", "OURS", "RESULT", "THEIRS", "2 conflicts remaining", "Conflict 1 of 2 · unresolved",
		"0/2 resolved", "Resolve conflict 1/2", "[o] Use ours", "● Modified"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	model = mergePress(model, "o")
	view = model.View()
	for _, want := range []string{"1 conflict remaining", "Conflict 2 of 2 · unresolved", "1/2 resolved", "✓ Conflict 1: used ours"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q after resolving:\n%s", want, view)
		}
	}
}

func TestMergeView_MarksResolvedAndUnresolvedConflicts_WithSymbols(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n", "o")
	view := model.View()
	if !strings.Contains(view, symbolResolved) || !strings.Contains(view, symbolUnresolved) || !strings.Contains(view, symbolCurrent) {
		t.Fatalf("resolved, unresolved and current markers expected:\n%s", view)
	}
	if model.stateOf(model.changed[0]) != stateResolved || model.stateOf(model.changed[1]) != stateUnresolved {
		t.Fatalf("states %v %v", model.stateOf(model.changed[0]), model.stateOf(model.changed[1]))
	}
}

func TestMergeView_ShowsSaveState_WhenSavedOrFailed(t *testing.T) {
	s := &saver{}
	model, _ := newMergeTestModel(baseText, oursText, theirsText, s)
	model = mergePress(model, "n", "n", "o")
	if view := model.View(); !strings.Contains(view, "● Modified") || !strings.Contains(view, "all conflicts resolved") {
		t.Fatalf("modified expected:\n%s", view)
	}
	s.err = errors.New("disk full")
	model = mergePress(model, "s")
	if view := model.View(); !strings.Contains(view, "✗ Save failed") || !strings.Contains(view, "disk full") {
		t.Fatalf("failed save expected:\n%s", view)
	}
	s.err = nil
	model = mergePress(model, "s")
	if view := model.View(); !strings.Contains(view, "✓ Saved") || strings.Contains(view, "Modified") {
		t.Fatalf("saved expected:\n%s", view)
	}
	model = mergePress(model, "u")
	if view := model.View(); !strings.Contains(view, "● Modified") {
		t.Fatalf("undo must mark the result modified:\n%s", view)
	}
}

func TestMergeView_ShowsOnePaneAtATime_WhenTerminalIsNarrow(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = resize(model, 60, 12)
	view := model.View()
	if !strings.Contains(view, "RESULT") || strings.Contains(view, "THEIRS7") || strings.Contains(view, "OURS7") {
		t.Fatalf("only the result pane expected:\n%s", view)
	}
	model = mergePress(model, "v")
	if view = model.View(); !strings.Contains(view, "THEIRS") || !strings.Contains(view, "THEIRS7") {
		t.Fatalf("v must show the theirs pane:\n%s", view)
	}
	model = mergePress(model, "v", "v")
	if model.viewPane != paneResult {
		t.Fatalf("view pane %d", model.viewPane)
	}
	wide := resize(model, 120, 12)
	if view = wide.View(); !strings.Contains(view, "OURS7") || !strings.Contains(view, "THEIRS7") {
		t.Fatalf("wide terminal shows all panes:\n%s", view)
	}
}

func TestMergeView_GivesResultPaneMostWidth_WhenWide(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	_, widths := resize(model, 140, 20).layout()
	if widths[paneResult] <= widths[paneOurs] || widths[paneResult] <= widths[paneTheirs] {
		t.Fatalf("widths %v", widths)
	}
}

func TestMergeView_ShowsUnavailableActionsAsFaint_WhenNothingSelected(t *testing.T) {
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	if view := model.View(); !strings.Contains(view, "save (1 left)") || !strings.Contains(view, "Press n to jump") {
		t.Fatalf("contextual hints expected:\n%s", view)
	}
	model = mergePress(model, "n", "n", "t")
	if view := model.View(); !strings.Contains(view, "all conflicts resolved, press s to save") {
		t.Fatalf("completion message expected:\n%s", view)
	}
}
