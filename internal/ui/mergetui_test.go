package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	for _, want := range []string{"ours", "result", "theirs", "OURS7", "THEIRS7", "1 conflict(s) left"} {
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
