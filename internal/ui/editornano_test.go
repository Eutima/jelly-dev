package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/editor"
)

// newNanoForTest is an editor in nano style on a file with the text.
func newNanoForTest(t *testing.T, text string) (EditorModel, string) {
	t.Helper()
	model, path := newEditorForTest(t, text)
	return model.WithStyle(config.EditorNano), path
}

// nanoKeys feeds keys: named keys as keyMsg knows them (ctrl+x, alt+u, enter, left), "<text>" as typed text.
func nanoKeys(model EditorModel, keys ...string) (EditorModel, tea.Cmd) {
	var last tea.Cmd
	for _, key := range keys {
		var msg tea.KeyMsg
		if len([]rune(key)) > 1 && strings.HasPrefix(key, "<") && strings.HasSuffix(key, ">") {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key[1 : len(key)-1])}
		} else {
			msg = keyMsg(key)
		}
		updated, cmd := model.Update(msg)
		model = updated.(EditorModel)
		if cmd != nil {
			last = cmd
		}
	}
	return model, last
}

func text(model EditorModel) string { return strings.Join(model.vim.Lines(), "\n") }

func TestNano_TypesWithoutModesAndShowsItsKeys(t *testing.T) {
	model, _ := newNanoForTest(t, "")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 12}) // wide enough for every hint with its label
	model = updated.(EditorModel)
	model, _ = nanoKeys(model, "h", "i", "enter", "<ok>")
	if got := text(model); got != "hi\nok" {
		t.Fatalf("text %q", got)
	}
	view := stripANSI(model.View())
	for _, want := range []string{"NANO", "[ctrl+s] Save", "[ctrl+x] Exit", "[ctrl+w] Search", "[ctrl+k] Cut", "[ctrl+u] Paste", "[alt+u] Undo"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the footer lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "-- NORMAL --") || strings.Contains(view, "-- INSERT --") {
		t.Fatalf("no vim modes in the nano style:\n%s", view)
	}
	if model.Style() != config.EditorNano {
		t.Fatalf("style %q", model.Style())
	}
}

func TestNano_SaveCutPasteUndoAndGoto(t *testing.T) {
	model, path := newNanoForTest(t, "one\ntwo\nthree\n")
	model, _ = nanoKeys(model, "ctrl+k") // cut "one"
	model, _ = nanoKeys(model, "down", "ctrl+u")
	if got := text(model); got != "two\none\nthree" {
		t.Fatalf("after cut and paste: %q", got)
	}
	model, _ = nanoKeys(model, "alt+u")
	if got := text(model); got != "two\nthree" {
		t.Fatalf("after undo: %q", got)
	}
	model, _ = nanoKeys(model, "alt+g", "3", "enter")
	if row, _ := model.Cursor(); row != 1 {
		t.Fatalf("goto line 3 of 2 lines stays on the last: %d", row)
	}
	model, _ = nanoKeys(model, "ctrl+s")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "two\nthree\n" {
		t.Fatalf("saved %q %v", data, err)
	}
	if model.Modified() {
		t.Fatal("saved")
	}
}

func TestNano_SearchPromptIsShownAndFindsText(t *testing.T) {
	model, _ := newNanoForTest(t, "alpha\nbeta\ngamma\n")
	model, _ = nanoKeys(model, "ctrl+w")
	if view := stripANSI(model.View()); !strings.Contains(view, "Search: █") {
		t.Fatalf("the prompt:\n%s", view)
	}
	model, _ = nanoKeys(model, "g", "a", "m")
	if view := stripANSI(model.View()); !strings.Contains(view, "Search: gam█") {
		t.Fatalf("the typed text:\n%s", view)
	}
	model, _ = nanoKeys(model, "enter")
	if row, col := model.Cursor(); row != 2 || col != 0 {
		t.Fatalf("cursor %d,%d", row, col)
	}
	if view := stripANSI(model.View()); strings.Contains(view, "Search:") {
		t.Fatalf("the prompt is closed:\n%s", view)
	}
}

func TestNano_ExitAsksToSaveOnlyWhenModified(t *testing.T) {
	model, path := newNanoForTest(t, "x\n")
	_, cmd := nanoKeys(model, "ctrl+x")
	if !isQuit(cmd) {
		t.Fatal("an unchanged file is left at once")
	}
	model, _ = nanoKeys(model, "a")
	model, cmd = nanoKeys(model, "ctrl+x")
	if isQuit(cmd) {
		t.Fatal("a changed file asks first")
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "Save modified buffer?") {
		t.Fatalf("the question:\n%s", view)
	}
	model, cmd = nanoKeys(model, "esc")
	if isQuit(cmd) || strings.Contains(stripANSI(model.View()), "Save modified buffer?") {
		t.Fatal("esc cancels")
	}
	model, cmd = nanoKeys(model, "ctrl+x", "y")
	if !isQuit(cmd) || !model.Quitting() {
		t.Fatal("y saves and leaves")
	}
	if data, _ := os.ReadFile(path); string(data) != "ax\n" {
		t.Fatalf("saved %q", data)
	}
}

func TestNano_ExitWithoutSavingDiscards(t *testing.T) {
	model, path := newNanoForTest(t, "x\n")
	model, _ = nanoKeys(model, "a")
	_, cmd := nanoKeys(model, "ctrl+x", "n")
	if !isQuit(cmd) {
		t.Fatal("n leaves")
	}
	if data, _ := os.ReadFile(path); string(data) != "x\n" {
		t.Fatalf("nothing saved: %q", data)
	}
}

func TestNano_ClickMovesTheCursor_AndHintsAreClickable(t *testing.T) {
	model, _ := newNanoForTest(t, "hello world\nsecond\n")
	updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 1})
	model = updated.(EditorModel)
	if _, col := model.Cursor(); col == 0 {
		t.Fatal("a click in the text moves the cursor, also without modes")
	}
	// a click on the hint of ctrl+k cuts the line
	row := strings.Join(strings.Split(model.View(), "\n")[len(strings.Split(model.View(), "\n"))-2:], "\n")
	_ = row
	keysOfCut := model.footerKeys(strings.Index(stripANSI(strings.Split(model.View(), "\n")[model.height-1]), "[ctrl+k]")+1, model.height-1)
	if len(keysOfCut) == 0 || keysOfCut[0] != "ctrl+k" {
		t.Fatalf("the hint of cut sends its key: %v", keysOfCut)
	}
}

func TestNano_FormatKeepsTheStyle(t *testing.T) {
	model, _ := newNanoForTest(t, "x\n")
	model.vim.ReplaceLines([]string{"formatted"})
	if !model.vim.Modeless() || model.vim.Mode().String() != "-- INSERT --" {
		t.Fatal("formatting keeps the buffer modeless")
	}
}

func TestNano_StyleSwitchKeepsTheTextAndChangesTheKeys(t *testing.T) {
	model, _ := newEditorForTest(t, "one\ntwo\n")
	model = model.WithStyle(config.EditorNano)
	model, _ = nanoKeys(model, "<X>")
	if got := text(model); got != "Xone\ntwo" {
		t.Fatalf("nano typed: %q", got)
	}
	model = model.WithStyle(config.EditorVim)
	if model.Style() != config.EditorVim || model.vim.Mode().String() != "-- NORMAL --" {
		t.Fatalf("back to normal mode: %v", model.vim.Mode())
	}
	model, _ = typeEditor(model, "jx")
	if got := text(model); got != "Xone\nto" {
		t.Fatalf("vim keys again: %q", got)
	}
}

func TestNano_ReopenedFileKeepsTheStyle(t *testing.T) {
	model, _ := newNanoForTest(t, "a\n")
	path := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(path, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := editor.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	model = model.opened(doc)
	if model.Style() != config.EditorNano || !model.vim.Modeless() {
		t.Fatal("a new file in the same editor keeps the nano style")
	}
}
