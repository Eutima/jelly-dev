package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

var altP = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p"), Alt: true}

func sendKey(model ProjectModel, msg tea.KeyMsg) ProjectModel {
	updated, cmd := model.Update(msg)
	return settleProject(updated.(ProjectModel), cmd)
}

func TestPalette_OpensFromTreeAndFromEditorInEveryMode(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if got := sendKey(model, altP); got.palette == nil {
		t.Fatal("alt+p must open the palette from the tree")
	}
	if got := sendKey(model, tea.KeyMsg{Type: tea.KeyF1}); got.palette == nil {
		t.Fatal("f1 must open the palette too")
	}
	model, _ = projectKeys(model, "G", "enter") // a file is open in the editor
	model, _ = projectKeys(model, "i")          // insert mode
	before := strings.Join(model.Editor().Lines(), "\n")
	got := sendKey(model, altP)
	if got.palette == nil || strings.Join(got.Editor().Lines(), "\n") != before {
		t.Fatal("alt+p must open the palette from insert mode without typing")
	}
}

func TestPalette_ListsEverythingTheBarsUsedToShow(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Theme = func(string) (Theme, bool) { return DarkTheme(), true }
	view := stripANSI(sendKey(model, altP).View())
	for _, want := range []string{"Switch project", "Show or hide the terminal", "Show the git diff", "Find file", "Search text in the project", "Save file"} {
		if !strings.Contains(view, want) {
			t.Errorf("the first rows of the palette lack %q:\n%s", want, view)
		}
	}
	labels := map[string]bool{}
	for _, action := range model.paletteActions() {
		labels[action.label] = true
	}
	for _, want := range []string{"Go to definition", "Find references", "Format file", "Next tab", "Close tab", "Quit", "New terminal tab"} {
		if !labels[want] {
			t.Errorf("no action %q", want)
		}
	}
}

func TestPalette_TypingNarrowsAndEnterRuns(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model = sendKey(model, altP)
	model, _ = projectKeys(model, "s", "w", "i", "t", "c", "h")
	if !strings.Contains(stripANSI(model.View()), "Switch project") || strings.Contains(stripANSI(model.View()), "Show or hide the terminal") {
		t.Fatalf("typing must narrow the list:\n%s", model.View())
	}
	model, _ = projectKeys(model, "enter")
	if model.palette != nil || model.picker == nil {
		t.Fatalf("enter runs the action: palette %v picker %v", model.palette != nil, model.picker != nil)
	}
}

func TestPalette_EscClosesWithoutRunningAnything(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model = sendKey(model, altP)
	model, _ = projectKeys(model, "esc")
	if model.palette != nil || model.picker != nil {
		t.Fatal("esc must close the palette and nothing else")
	}
}

func TestPalette_SaveActionWritesTheFile(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter", "i", "X", "esc") // a file with an unsaved change
	if !model.Editor().Modified() {
		t.Fatal("setup: the editor must be modified")
	}
	model = sendKey(model, altP)
	model, _ = projectKeys(model, "s", "a", "v", "e", " ", "f", "i", "l", "e", "enter")
	if model.Editor().Modified() {
		t.Fatal("the save action must write the file")
	}
	if _, err := os.Stat(filepath.Join(fx.root, "README.md")); err != nil {
		t.Fatal(err)
	}
}

func TestPalette_ClickOnAnEntryRunsIt(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model = sendKey(model, altP)
	lines := strings.Split(stripANSI(model.View()), "\n")
	row := -1
	column := 0
	for y, line := range lines {
		if i := strings.Index(line, "Switch project"); i >= 0 {
			row, column = y, len([]rune(line[:i]))
		}
	}
	if row < 0 {
		t.Fatalf("no entry to click:\n%s", model.View())
	}
	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: column + 1, Y: row})
	got := settleProject(updated.(ProjectModel), cmd)
	if got.palette != nil || got.picker == nil {
		t.Fatalf("a click runs the entry: palette %v picker %v", got.palette != nil, got.picker != nil)
	}
}

func TestProject_SlashSearchesTheReferenceListInTheWholeWindow(t *testing.T) {
	nav := &fakeNav{}
	model, root := navProject(t, nav)
	nav.references = []lsp.Place{
		{Path: filepath.Join(root, "main.go"), Row: 3, Text: "helper()"},
		{Path: filepath.Join(root, "util.go"), Row: 3, Text: "func helper() {}"},
		{Path: filepath.Join(root, "util.go"), Row: 2, Text: "// helper does it"},
	}
	model, _ = projectKeys(model, "g", "r")
	if model.Editor().list == nil {
		t.Fatal("setup: the reference list must be open")
	}
	model, _ = projectKeys(model, "/", "u", "t", "i", "l", "enter")
	if model.Editor().list == nil || model.Editor().list.sel != 1 || model.Editor().list.last != "util" {
		t.Fatalf("/util must select the first util.go place: %+v", model.Editor().list)
	}
	if model.palette != nil || model.search != nil || model.focus != focusProjectEdit {
		t.Fatalf("the slash belongs to the list: palette %v search %v focus %d", model.palette != nil, model.search != nil, model.focus)
	}
}

func TestPalette_ShowsShortcutColumnThenDescription(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	view := stripANSI(sendKey(model, altP).View())
	var lines []string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Switch project") || strings.Contains(line, "Show or hide the terminal") || strings.Contains(line, "Save file") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("entries not found:\n%s", view)
	}
	column := -1
	for _, line := range lines {
		i := strings.Index(line, "[")
		j := strings.Index(line, "]")
		if i < 0 || j < i {
			t.Fatalf("an entry must start with its [shortcut]: %q", line)
		}
		rest := strings.TrimLeft(line[j+1:], " ")
		start := len([]rune(line[:j+1])) + (len(line[j+1:]) - len(rest)) // column where the description starts
		if column >= 0 && start != column {
			t.Fatalf("the descriptions must line up: %d vs %d\n%s", start, column, view)
		}
		column = start
	}
	if !strings.Contains(view, "[ctrl+p]") || !strings.Contains(view, "[alt+d]") {
		t.Fatalf("shortcuts in brackets expected:\n%s", view)
	}
}
