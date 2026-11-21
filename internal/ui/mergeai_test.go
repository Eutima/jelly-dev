package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-diff/internal/ai"
)

// runAssistantKey presses key and, like the bubbletea runtime, executes the returned command and feeds its message back.
func runAssistantKey(t *testing.T, model MergeModel, key string) MergeModel {
	t.Helper()
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	model = updated.(MergeModel)
	if cmd == nil {
		return model
	}
	updated, _ = model.Update(cmd())
	return updated.(MergeModel)
}

func fakeAssistant(answer string, err error) ai.Assistant {
	return ai.New("Fake", "a", func(context.Context, string, string) (string, error) { return answer, err })
}

func TestMergeAssistant_AppliesResolution_WhenAssistantAnswers(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	if model.Unresolved() != 2 || len(model.changed) != 2 {
		t.Fatalf("conflicts = %d, changed = %v", model.Unresolved(), model.changed)
	}
	// Conflict ids are chunk indexes; an id that was not asked for must not be applied.
	answer := fmt.Sprintf(`{"resolutions":[{"id":%d,"lines":["XY"]},{"id":%d,"lines":["X2Y2"]}]}`, model.changed[0], model.changed[1])
	model = model.WithAssistants(fakeAssistant(answer, nil))

	model = runAssistantKey(t, model, "a")

	if model.Unresolved() != 0 || !strings.Contains(model.status, "Fake resolved 2 conflict(s)") {
		t.Fatalf("unresolved = %d, status = %q", model.Unresolved(), model.status)
	}
	want := []string{"a", "XY", "c", "d", "e", "f", "g", "X2Y2", "i"}
	if !reflect.DeepEqual(model.Result(), want) {
		t.Fatalf("result = %q, want %q", model.Result(), want)
	}
	model = mergePress(model, "u")
	if model.Unresolved() != 2 {
		t.Fatalf("one undo should revert the whole assistant answer, unresolved = %d", model.Unresolved())
	}
}

func TestMergeAssistant_ReportsError_WhenAssistantFails(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = model.WithAssistants(fakeAssistant("", errors.New("not logged in")))

	model = runAssistantKey(t, model, "a")

	if model.Unresolved() != 2 || !model.warn || !strings.Contains(model.status, "not logged in") {
		t.Fatalf("unresolved = %d, warn = %v, status = %q", model.Unresolved(), model.warn, model.status)
	}
	if model.aiBusy {
		t.Fatal("model must not stay busy after an error")
	}
}

func TestMergeAssistant_IgnoresKey_WhenNoAssistantInstalled(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	model = runAssistantKey(t, model, "a")
	if model.Unresolved() != 2 || model.aiBusy {
		t.Fatalf("unresolved = %d, busy = %v", model.Unresolved(), model.aiBusy)
	}
}
