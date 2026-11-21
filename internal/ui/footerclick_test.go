package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/source"
)

func TestCommandKeys_MapsShownTextToKeys(t *testing.T) {
	for shown, want := range map[string][]string{
		"→": {"right"}, "⌫": {"backspace"}, "n/p": {"n"}, "s": {"s"}, "ctrl+o": {"ctrl+o"},
		":wq": {":", "w", "q", "enter"}, ":q!": {":", "q", "!", "enter"}, "esc": {"esc"},
	} {
		if got := commandKeys(shown); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", shown, got, want)
		}
	}
}

func TestKeyMsg_RoundTripsWithString(t *testing.T) {
	for _, name := range []string{"enter", "esc", "backspace", "left", "right", "down", "tab", "ctrl+o", "ctrl+d", "q", "w", "!", ":"} {
		if got := keyMsg(name).String(); got != name {
			t.Errorf("%q came back as %q", name, got)
		}
	}
}

func TestBar_ClickRegionsFollowDisplayWidth(t *testing.T) {
	theme := DarkTheme()
	b := &bar{}
	b.add(barPart{" ", theme.Footer})
	b.command("a", "first", true, theme)   // " [a] first": columns 1..10
	b.command("b", "second", false, theme) // disabled: inert
	b.command("日", "wide", true, theme)    // a wide rune takes two columns
	if keys := b.keysAt(0); keys != nil {
		t.Errorf("leading space must not be clickable: %v", keys)
	}
	if keys := b.keysAt(1); !reflect.DeepEqual(keys, []string{"a"}) {
		t.Errorf("start of the first command: %v", keys)
	}
	if keys := b.keysAt(10); keys != nil {
		t.Errorf("the gap between commands must not be clickable: %v", keys)
	}
	for x := 12; x < 12+len("[b] second"); x++ {
		if keys := b.keysAt(x); keys != nil {
			t.Fatalf("a disabled command must be inert at %d: %v", x, keys)
		}
	}
	if width := lipgloss.Width(b.render(200, theme.Footer)); width != 200 {
		t.Errorf("width %d", width)
	}
	if keys := b.visibleKeysAt(5, 3); keys != nil {
		t.Errorf("columns beyond the drawn width must not be clickable: %v", keys)
	}
}

// clickOn clicks the first column of the footer command whose "[key] label" text is in line y of the plain view.
func clickOn(t *testing.T, model tea.Model, view, text string) tea.Model {
	t.Helper()
	lines := strings.Split(plainText(view), "\n")
	for y, line := range lines {
		if index := strings.Index(line, text); index >= 0 {
			x := len([]rune(line[:index]))
			updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
			return updated
		}
	}
	t.Fatalf("%q not found in the view:\n%s", text, view)
	return nil
}

func plainText(view string) string {
	return stripANSI(view)
}

func TestMergeFooter_ClicksRunTheCommands(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	merged := clickOn(t, model, model.View(), "[n/p] change").(MergeModel)
	if merged.current < 0 {
		t.Fatal("[n/p] must select the first change")
	}
	merged = clickOn(t, merged, merged.View(), "[→] ours").(MergeModel)
	if merged.Result()[1] != "X" {
		t.Fatalf("[→] must apply ours: %v", merged.Result())
	}
	merged = clickOn(t, merged, merged.View(), "[u] undo").(MergeModel)
	if merged.Unresolved() != 2 {
		t.Fatalf("[u] must undo, unresolved %d", merged.Unresolved())
	}
	merged = clickOn(t, merged, merged.View(), "[b] both").(MergeModel)
	if got := merged.Result(); merged.Unresolved() != 1 || got[1] != "X" || got[2] != "Y" {
		t.Fatalf("[b] must take both sides: %v unresolved %d", got, merged.Unresolved())
	}
}

func TestMergeFooter_DisabledCommandsDoNothing(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	for _, command := range []string{"[→] ours", "[s] save", "[u] undo"} { // nothing selected, unresolved, nothing to undo
		after := clickOn(t, model, model.View(), command).(MergeModel)
		if after.Unresolved() != 2 || len(after.history) != 0 || after.outcome.Saved {
			t.Errorf("%s must be inert", command)
		}
	}
}

func TestMergeFooter_EditorCommandsAreClickable(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "x")
	model = clickOn(t, model, model.View(), "[:w] apply").(MergeModel)
	if !model.editing || model.Result()[0] != "" {
		t.Fatalf("[:w] must apply and stay: editing %v result %v", model.editing, model.Result())
	}
	model = clickOn(t, model, model.View(), "[:q] leave").(MergeModel)
	if model.editing {
		t.Fatal("[:q] must leave the editor")
	}
}

func TestMergeFooter_CompactFooterAndOtherRowsIgnoreClicks(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	small, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	for y := 0; y < 8; y++ {
		updated, _ := small.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: y})
		if after := updated.(MergeModel); after.Unresolved() != 2 || len(after.history) != 0 {
			t.Fatalf("row %d: the compact footer must not react", y)
		}
	}
}

func TestDiffFooter_ClickTogglesUnified(t *testing.T) {
	diffModel := NewDiffModel("a", "b", []string{"x"}, []string{"y"})
	updated, _ := diffModel.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	model := clickOn(t, updated, updated.View(), "[t] split/unified").(DiffModel)
	if !model.showUnified {
		t.Fatal("[t] must switch to the unified view")
	}
}

func TestDirMergeFooter_ClickQuits(t *testing.T) {
	files := []source.ChangedFile{{Path: "a.txt"}}
	model := NewDirMergeModel("left", "right", files, nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	view := updated.View()
	lines := strings.Split(plainText(view), "\n")
	y := len(lines) - 1
	x := strings.Index(lines[y], "[q] quit")
	if x < 0 {
		t.Fatalf("no [q] quit in the footer: %q", lines[y])
	}
	_, cmd := updated.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
}

func TestGitFooter_ClicksReachListAndDiffCommands(t *testing.T) {
	model, _ := newGitTestModel(140, 12)
	model = settle(model)
	view := model.View()
	// the list footer (left) and the diff footer (right) are both clickable
	for _, command := range []string{"[tab] focus", "[t] split/unified"} {
		lines := strings.Split(plainText(view), "\n")
		y := len(lines) - 1
		x := strings.Index(lines[y], command)
		if x < 0 {
			t.Fatalf("%q missing in the footer: %q", command, lines[y])
		}
		x = len([]rune(lines[y][:x]))
		updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
		after := updated.(GitModel)
		switch command {
		case "[tab] focus":
			if after.focus == model.focus {
				t.Errorf("[tab] must switch the focus")
			}
		default:
			if after.diff.showUnified == model.diff.showUnified {
				t.Errorf("[t] must toggle the unified view")
			}
		}
	}
}
