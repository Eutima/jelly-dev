package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/merge"
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
			if got := lipgloss.Width(line); got > size[0] && size[0] >= 2*mergeGutterWidth {
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
	for _, want := range []string{"Jelly Dev", "OURS", "RESULT", "THEIRS", "2 conflicts remaining", "Conflict 1 of 2 · unresolved",
		"0/2 resolved", "Resolve conflict 1/2", "[→] ours", "● Modified"} {
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
	if view := model.View(); !strings.Contains(view, "save (1 left)") || !strings.Contains(view, "Press [n] to jump") {
		t.Fatalf("contextual hints expected:\n%s", view)
	}
	model = mergePress(model, "n", "n", "t")
	if view := model.View(); !strings.Contains(view, "all conflicts resolved, press s to save") {
		t.Fatalf("completion message expected:\n%s", view)
	}
}

// clickArrow clicks the gutter arrow of the first row of change number index (0-based) on the left or right side.
func clickArrow(t *testing.T, model MergeModel, index, side int) MergeModel {
	t.Helper()
	_, widths := model.layout()
	x := markerWidth + widths[paneOurs] + 3 // the » of the left gutter
	if side == paneTheirs {
		x = markerWidth + widths[paneOurs] + mergeGutterWidth + widths[paneResult] + 1 // the « of the right gutter
	}
	y := model.headerLines() + model.starts[index] - model.offset
	updated, _ := model.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return updated.(MergeModel)
}

func TestMergeModel_AppliesOursOrTheirs_WhenArrowClicked(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	model = clickArrow(t, model, 0, paneOurs)
	if got := model.Result()[1]; got != "X" || model.Unresolved() != 1 {
		t.Fatalf("» must apply ours: result %v unresolved %d", model.Result(), model.Unresolved())
	}
	model = clickArrow(t, model, 1, paneTheirs)
	if got := model.Result()[7]; got != "Y2" || model.Unresolved() != 0 {
		t.Fatalf("« must apply theirs: result %v unresolved %d", model.Result(), model.Unresolved())
	}
}

func TestMergeModel_IgnoresClick_WhenNotOnAnArrow(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	before := model.Result()
	press := func(x, y int) {
		updated, _ := model.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		model = updated.(MergeModel)
	}
	press(0, 0)                                 // header
	press(markerWidth+2, model.headerLines()+1) // text of the ours pane
	press(markerWidth+1, model.headerLines()+4) // unchanged row, wrong x
	_, widths := model.layout()
	press(markerWidth+widths[paneOurs]+3, model.headerLines()) // arrow column of an unchanged row
	if !reflect.DeepEqual(model.Result(), before) || len(model.history) != 0 {
		t.Fatalf("nothing may change: %v", model.Result())
	}
}

// clickCross clicks the discard cross next to the arrow of change number index on the given side.
func clickCross(t *testing.T, model MergeModel, index, side int) MergeModel {
	t.Helper()
	_, widths := model.layout()
	x := markerWidth + widths[paneOurs] + 1 // the × of the left gutter
	if side == paneTheirs {
		x = markerWidth + widths[paneOurs] + mergeGutterWidth + widths[paneResult] + 3 // the × of the right gutter
	}
	y := model.headerLines() + model.starts[index] - model.offset
	updated, _ := model.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return updated.(MergeModel)
}

func TestMergeModel_KeepsTheOtherSide_WhenCrossClicked(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	model = clickCross(t, model, 0, paneOurs)
	if got := model.Result()[1]; got != "Y" || model.Unresolved() != 1 {
		t.Fatalf("× on ours must keep theirs: %v", model.Result())
	}
	model = clickCross(t, model, 1, paneTheirs)
	if got := model.Result()[7]; got != "X2" || model.Unresolved() != 0 {
		t.Fatalf("× on theirs must keep ours: %v", model.Result())
	}
}

func TestMergeModel_CombinesBothSides_WhenBothArrowsClickedOnTheSameChange(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	model = clickArrow(t, model, 0, paneOurs)
	model = clickArrow(t, model, 0, paneTheirs)
	if model.choices[model.changed[0]] != ChoiceBoth || !reflect.DeepEqual(model.Result()[1:3], []string{"X", "Y"}) {
		t.Fatalf("both expected: %v", model.Result())
	}
	model = clickCross(t, model, 0, paneOurs) // discard ours again: only theirs is left
	if model.choices[model.changed[0]] != ChoiceTheirs {
		t.Fatalf("choice %v", model.choices[model.changed[0]])
	}
	model = clickCross(t, model, 0, paneTheirs) // discard the last wanted side: back to the base
	if model.choices[model.changed[0]] != ChoiceBase || model.Result()[1] != "b" {
		t.Fatalf("base expected: %v", model.Result())
	}
}

func TestMergeModel_StaysOnChange_WhenClickingArrows(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	model = clickArrow(t, model, 0, paneOurs)
	if model.current != 0 || model.cursor != model.starts[0] {
		t.Fatalf("a click must not jump to the next conflict: current %d cursor %d", model.current, model.cursor)
	}
}

func TestMergeModel_IgnoresClick_WhenEditingOrNarrow(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	editing := mergePress(model, "j", "e")
	if got := clickArrow(t, editing, 0, paneOurs); got.Unresolved() != 2 || !got.editing {
		t.Fatalf("click while editing must do nothing: unresolved %d", got.Unresolved())
	}
	narrow := resize(model, 60, 20)
	updated, _ := narrow.Update(tea.MouseMsg{X: 20, Y: narrow.headerLines() + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if updated.(MergeModel).Unresolved() != 2 {
		t.Fatal("click in the single pane layout must do nothing")
	}
}

func TestMergeView_HidesAppliedButton_WhenSideWasApplied(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = resize(model, 120, 20)
	row := model.rows[model.starts[0]]
	for _, want := range [][3]any{{paneOurs, true, true}, {paneTheirs, true, true}} {
		side := want[0].(int)
		if !model.buttonShown(row, side, true) || !model.buttonShown(row, side, false) {
			t.Fatalf("side %d: both buttons expected on an unresolved conflict", side)
		}
	}
	model = clickArrow(t, model, 0, paneOurs) // apply ours
	row = model.rows[model.starts[0]]
	if model.buttonShown(row, paneOurs, true) || !model.buttonShown(row, paneOurs, false) {
		t.Fatal("» must be gone after ours was applied, × stays to take it back")
	}
	if !model.buttonShown(row, paneTheirs, true) || model.buttonShown(row, paneTheirs, false) {
		t.Fatal("« stays for theirs, × of theirs is pointless because theirs is already left out")
	}
	history := len(model.history)
	model = clickArrow(t, model, 0, paneOurs) // clicking where » was must do nothing
	if len(model.history) != history {
		t.Fatal("a hidden button must not be clickable")
	}
	model = clickArrow(t, model, 0, paneTheirs) // now both
	row = model.rows[model.starts[0]]
	if model.buttonShown(row, paneOurs, true) || model.buttonShown(row, paneTheirs, true) {
		t.Fatal("no apply button may be left when both sides are applied")
	}
}

func arrowPress(model MergeModel, keyType tea.KeyType) MergeModel {
	updated, _ := model.Update(tea.KeyMsg{Type: keyType})
	return updated.(MergeModel)
}

func TestMergeModel_AddsSideAndStays_WhenArrowKeysPressed(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n") // the first change is the conflict at line 2
	current := model.current
	model = arrowPress(model, tea.KeyRight)
	if model.Result()[1] != "X" || model.current != current || model.Unresolved() != 1 {
		t.Fatalf("right arrow must take ours and stay: %v current %d", model.Result(), model.current)
	}
	model = arrowPress(model, tea.KeyLeft)
	if got := model.Result(); got[1] != "X" || got[2] != "Y" || model.current != current {
		t.Fatalf("left arrow after right must add theirs (both): %v", got)
	}
}

func TestMergeModel_RemovesSide_WhenSameArrowPressedAgain(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n")
	model = arrowPress(arrowPress(model, tea.KeyRight), tea.KeyLeft) // both
	model = arrowPress(model, tea.KeyRight)                          // remove ours
	if got := model.Result(); got[1] != "Y" || got[2] == "X" {
		t.Fatalf("second right arrow must remove ours: %v", got)
	}
	model = arrowPress(model, tea.KeyLeft)
	if model.choices[model.changed[model.current]] != ChoiceAuto || model.Unresolved() != 2 || model.Result()[1] != "b" {
		t.Fatalf("removing the last side must make the conflict unresolved again: %v", model.Result())
	}
	if model.stateOf(model.changed[model.current]) != stateUnresolved || model.paneKinds(model.changed[model.current], model.Result()[1:2])[paneOurs] != CellConflict {
		t.Fatal("the initial conflict highlighting must be back")
	}
}

func TestMergeModel_CancelsDecision_WhenBackspacePressed(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = arrowPress(mergePress(model, "n"), tea.KeyRight)
	if model.Unresolved() != 1 {
		t.Fatalf("unresolved %d", model.Unresolved())
	}
	model = arrowPress(model, tea.KeyBackspace)
	if model.Unresolved() != 2 || model.Result()[1] != "b" || !strings.Contains(model.View(), "decision cancelled") {
		t.Fatalf("unresolved %d result %v\n%s", model.Unresolved(), model.Result(), model.View())
	}
	if model = arrowPress(model, tea.KeyBackspace); !strings.Contains(model.View(), "Nothing to cancel") {
		t.Fatalf("cancelling twice must explain itself:\n%s", model.View())
	}
	model = mergePress(model, "u") // the cancel is one undo step: ours is applied again
	if model.Result()[1] != "X" || model.Unresolved() != 1 {
		t.Fatalf("undo must bring ours back: %v unresolved %d", model.Result(), model.Unresolved())
	}
}

func TestMergeModel_JumpsOn_WhenLetterKeysPressed(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n", "o")
	if model.Result()[1] != "X" || model.changed[model.current] == model.changed[0] {
		t.Fatalf("o must take ours and move on: %v current %d", model.Result(), model.current)
	}
}

func TestMergeModel_ExplainsArrow_WhenNoChangeSelectedOrSideUnchanged(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	if model = arrowPress(model, tea.KeyRight); !strings.Contains(model.View(), "Select a change first") {
		t.Fatalf("no selection must be explained:\n%s", model.View())
	}
	one, _ := newMergeTestModel("a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", &saver{}) // only ours changed
	one = arrowPress(mergePress(one, "n"), tea.KeyLeft)
	if !strings.Contains(one.View(), "theirs has no change here") {
		t.Fatalf("an unchanged side must be explained:\n%s", one.View())
	}
}

func TestMergeModel_TakesOneSideForAllConflicts_WhenCtrlArrowPressed(t *testing.T) {
	base := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"
	ours := "a\nX\nc\nd\ne\nf\ng\nX2\ni\nJ\nk\nl\nm\n"   // j is changed by ours only: merges automatically
	theirs := "a\nY\nc\nd\ne\nf\ng\nY2\ni\nj\nk\nL\nm\n" // l is changed by theirs only: merges automatically
	model, _ := newMergeTestModel(base, ours, theirs, &saver{})
	taken, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	result := taken.(MergeModel)
	want := split("a\nX\nc\nd\ne\nf\ng\nX2\ni\nJ\nk\nL\nm\n")
	if !reflect.DeepEqual(result.Result(), want) || result.Unresolved() != 0 {
		t.Fatalf("ctrl+right must take ours for every conflict and keep the automatic merges: %v", result.Result())
	}
	again, _ := result.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if !strings.Contains(again.(MergeModel).View(), "No unresolved conflicts left") {
		t.Fatal("a second press must say that nothing is left")
	}
	if undone := mergePress(result, "u"); undone.Unresolved() != 2 {
		t.Fatalf("one undo must revert the whole step: unresolved %d", undone.Unresolved())
	}
	theirsAll, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if got := theirsAll.(MergeModel).Result(); got[1] != "Y" || got[7] != "Y2" {
		t.Fatalf("ctrl+left must take theirs: %v", got)
	}
}

func TestMergeModel_TakesOneSideForAllConflicts_WhenCtrlOrCtrlTPressed(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n", "t") // conflict 1 is decided by hand: theirs
	ctrlO, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	got := ctrlO.(MergeModel).Result()
	if got[1] != "Y" || got[7] != "X2" {
		t.Fatalf("ctrl+o must take ours for the open conflict only and keep the decided one: %v", got)
	}
	model, _ = newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	ctrlT, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if got := ctrlT.(MergeModel).Result(); got[1] != "Y" || got[7] != "Y2" {
		t.Fatalf("ctrl+t must take theirs everywhere: %v", got)
	}
}

func TestMergeView_ShowsAllConflictsResolved_WhenLastConflictResolvedByAnyAction(t *testing.T) {
	for name, keys := range map[string][]string{
		"letter o":    {"n", "o"},
		"arrow right": {"n", "right"},
		"both":        {"n", "b"},
		"auto":        {"ctrl+o"},
		"edit":        {"n", "e", "A", "x", "esc", ":", "w", "q", "enter"},
	} {
		model, _ := newMergeTestModel("a\nb\nc\n", "a\nOURS\nc\n", "a\nTHEIRS\nc\n", &saver{})
		for _, key := range keys {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
			switch key {
			case "right":
				msg = tea.KeyMsg{Type: tea.KeyRight}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "ctrl+o":
				msg = tea.KeyMsg{Type: tea.KeyCtrlO}
			}
			updated, _ := model.Update(msg)
			model = updated.(MergeModel)
		}
		if view := model.View(); model.Unresolved() != 0 || !strings.Contains(strings.ToLower(view), "all conflicts resolved") ||
			!strings.Contains(view, "press s to save") {
			t.Errorf("%s: message expected (unresolved %d):\n%s", name, model.Unresolved(), view)
		}
	}
}

func TestMergeView_KeepsOtherMessage_WhenConflictsStillOpen(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n", "o")
	if strings.Contains(strings.ToLower(model.status), "all conflicts resolved") {
		t.Fatalf("one conflict is left: %q", model.status)
	}
}

func TestMergeModel_IgnoresConflictAndStays_WhenXPressed(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = mergePress(model, "n")
	current, cursor := model.current, model.cursor
	model = mergePress(model, "x")
	if model.Unresolved() != 1 || model.current != current || model.cursor != cursor {
		t.Fatalf("unresolved %d current %d (was %d) cursor %d (was %d)", model.Unresolved(), model.current, current, model.cursor, cursor)
	}
	if got := model.Result(); got[1] != "b" || got[7] != "h" {
		t.Fatalf("the current lines must be kept: %v", got)
	}
	if !strings.Contains(model.status, "ignored") {
		t.Fatalf("status %q", model.status)
	}
	if model = mergePress(model, "u"); model.Unresolved() != 2 {
		t.Fatalf("u must undo the ignore, unresolved %d", model.Unresolved())
	}
	model = arrowPress(mergePress(model, "x"), tea.KeyBackspace)
	if model.Unresolved() != 2 {
		t.Fatalf("backspace must cancel the ignore, unresolved %d", model.Unresolved())
	}
}

func TestMergeModel_ExplainsX_WhenNothingToIgnore(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	if model = mergePress(model, "x"); !strings.Contains(model.View(), "Select a change first") {
		t.Fatalf("no selection must be explained:\n%s", model.View())
	}
	model = mergePress(model, "n", "o", "p", "x") // the first conflict is decided already
	if !strings.Contains(model.View(), "Already decided") || model.Unresolved() != 1 || model.Result()[1] != "X" {
		t.Fatalf("a decided conflict must be left alone:\n%s", model.View())
	}
	auto, _ := newMergeTestModel("a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", &saver{}) // only ours changed: merges automatically
	if auto = mergePress(auto, "n", "x"); !strings.Contains(auto.View(), "Nothing to ignore") {
		t.Fatalf("an automatic change must be explained:\n%s", auto.View())
	}
}

func TestMergeModel_IgnoresInTwoWayMerge_WhenXPressed(t *testing.T) {
	chunks := merge.TwoWay(split("a\nb\nc"), split("a\nB\nc"))
	model, _ := NewMergeModel(MergeTitles{}, chunks, (&saver{}).save)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	model = mergePress(updated.(MergeModel), "n", "x")
	if model.Unresolved() != 0 || !reflect.DeepEqual(model.Result(), []string{"a", "c"}) {
		t.Fatalf("unresolved %d result %v", model.Unresolved(), model.Result())
	}
	if !strings.Contains(strings.ToLower(model.View()), "all conflicts resolved") {
		t.Fatalf("completion message expected:\n%s", model.View())
	}
}

// Coloured text on the blue bars (green, pink, blue) cannot be read when the terminal maps the colours to a small palette:
// everything the merge view writes on its title bar and its status line is drawn in the white style of the bar.
func TestMergeView_TextOnTheBarsIsWhite(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	white := model.theme.Header.GetForeground()
	check := func(name string, part barPart) {
		if part.style.GetForeground() != white {
			t.Errorf("%s (%q) must be white on the bar, its colour is %v", name, part.text, part.style.GetForeground())
		}
	}
	check("conflicts remaining", model.conflictSummary())
	check("save badge (modified)", model.saveBadge())
	model.saveFailed = true
	check("save badge (failed)", model.saveBadge())
	model.saveFailed, model.dirty = false, false
	check("save badge (saved)", model.saveBadge())
	for _, line := range []string{"Jelly Dev", "OURS"} {
		if !strings.Contains(stripANSI(model.View()), line) {
			t.Fatalf("the view lacks %q", line)
		}
	}
	// the title and the status line of the footer use the bar styles too
	model = mergePress(model, "n")
	model.status, model.warn = "something went wrong", true
	for _, part := range model.actionBar().parts {
		if strings.Contains(part.text, "something went wrong") && part.style.GetForeground() != white {
			t.Errorf("a warning on the footer bar must be white, its colour is %v", part.style.GetForeground())
		}
	}
}

func TestDarkTheme_TextOnColouredRowsIsWhite(t *testing.T) {
	theme := DarkTheme()
	for name, style := range map[string]lipgloss.Style{
		"added": theme.Added, "deleted": theme.Deleted, "modified": theme.Modified, "conflict": theme.Conflict, "ours": theme.Ours, "theirs": theme.Theirs,
	} {
		if got := style.GetForeground(); got != lipgloss.Color("#ffffff") {
			t.Errorf("%s rows must have white text, they have %v", name, got)
		}
	}
}
