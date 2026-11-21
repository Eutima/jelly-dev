package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/editor"
)

func newEditorForTest(t *testing.T, text string) (EditorModel, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := editor.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := NewEditorModel(doc).Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return updated.(EditorModel), path
}

// typeEditor feeds keys like vimKeys does and returns the model and the last command.
func typeEditor(model EditorModel, keys string) (EditorModel, tea.Cmd) {
	var last tea.Cmd
	for len(keys) > 0 {
		var msg tea.KeyMsg
		switch {
		case strings.HasPrefix(keys, "<esc>"):
			msg, keys = tea.KeyMsg{Type: tea.KeyEsc}, keys[len("<esc>"):]
		case strings.HasPrefix(keys, "<cr>"):
			msg, keys = tea.KeyMsg{Type: tea.KeyEnter}, keys[len("<cr>"):]
		default:
			r := []rune(keys)
			msg, keys = tea.KeyMsg{Type: tea.KeyRunes, Runes: r[:1]}, string(r[1:])
		}
		updated, cmd := model.Update(msg)
		model = updated.(EditorModel)
		if cmd != nil {
			last = cmd
		}
	}
	return model, last
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestEditor_EditsAndSavesFile(t *testing.T) {
	model, path := newEditorForTest(t, "package main\n\nfunc main() {}\n")
	model, _ = typeEditor(model, "ggAx<esc>jddp:w<cr>")
	want := []string{"package mainx", "func main() {}", ""}
	if got := model.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines %q, want %q", got, want)
	}
	if data, _ := os.ReadFile(path); string(data) != "package mainx\nfunc main() {}\n\n" {
		t.Fatalf("file %q", data)
	}
	if model.vim.Modified() || !strings.Contains(model.View(), "written") {
		t.Fatalf("modified %v\n%s", model.vim.Modified(), model.View())
	}
}

func TestEditor_QuitRefusesUnsavedChanges(t *testing.T) {
	model, _ := newEditorForTest(t, "a\n")
	model, cmd := typeEditor(model, "x:q<cr>")
	if isQuit(cmd) || !strings.Contains(model.View(), "E37") {
		t.Fatalf("quit must be refused:\n%s", model.View())
	}
	if _, cmd = typeEditor(model, ":q!<cr>"); !isQuit(cmd) {
		t.Fatal(":q! must quit")
	}
	if _, cmd = typeEditor(model, ":wq<cr>"); !isQuit(cmd) {
		t.Fatal(":wq must quit")
	}
}

func TestEditor_WritesToOtherFile_WhenWriteWithName(t *testing.T) {
	model, path := newEditorForTest(t, "a\n")
	other := filepath.Join(filepath.Dir(path), "copy.txt")
	model, _ = typeEditor(model, "x:w "+other+"<cr>")
	if data, _ := os.ReadFile(other); string(data) != "\n" {
		t.Fatalf("copy %q", data)
	}
	if !model.vim.Modified() {
		t.Fatal("writing to another file must keep the buffer modified")
	}
}

func TestEditor_OpensOtherFile_WhenEditCommand(t *testing.T) {
	model, path := newEditorForTest(t, "one\n")
	other := filepath.Join(filepath.Dir(path), "two.txt")
	if err := os.WriteFile(other, []byte("two\nlines\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model, _ = typeEditor(model, "x:e "+other+"<cr>")
	if !strings.Contains(model.View(), "E37") || model.Path() != path {
		t.Fatalf("unsaved changes must block :e:\n%s", model.View())
	}
	model, _ = typeEditor(model, ":e! "+other+"<cr>")
	if model.Path() != other || !reflect.DeepEqual(model.Lines(), []string{"two", "lines"}) || model.vim.Modified() {
		t.Fatalf("path %q lines %q", model.Path(), model.Lines())
	}
}

func TestEditor_ExplainsUnknownCommand(t *testing.T) {
	model, _ := newEditorForTest(t, "a\n")
	model, _ = typeEditor(model, ":frobnicate<cr>")
	if !strings.Contains(model.View(), "E492") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestEditor_ViewFitsAndShowsCursorLine(t *testing.T) {
	text := "package main\n\n\tfmt.Println(\"日本語\")\n" + strings.Repeat("long ", 40) + "\n"
	model, _ := newEditorForTest(t, text)
	for _, size := range [][2]int{{20, 6}, {80, 12}, {200, 30}} {
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = typeEditor(updated.(EditorModel), "G$")
		lines := strings.Split(model.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("size %v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("size %v: line %d is %d wide", size, i, lipgloss.Width(line))
			}
		}
	}
}

func TestEditor_ScrollsHorizontallyAndVertically_WhenCursorMoves(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line "+strings.Repeat("x", 120))
	}
	model, _ := newEditorForTest(t, strings.Join(lines, "\n")+"\n")
	model, _ = typeEditor(model, "G$")
	if model.top == 0 || model.left == 0 {
		t.Fatalf("top %d left %d", model.top, model.left)
	}
	view := stripANSI(model.View())
	if !strings.Contains(view, "100 ") || !strings.Contains(view, "xxxx") {
		t.Fatalf("the cursor line must be visible:\n%s", view)
	}
	model, _ = typeEditor(model, "gg0")
	if model.top != 0 || model.left != 0 {
		t.Fatalf("top %d left %d after gg0", model.top, model.left)
	}
}

func TestEditor_ClickMovesCursorAndFooterCommandsWork(t *testing.T) {
	model, _ := newEditorForTest(t, "alpha\nbeta\ngamma\n")
	updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2 + 3, Y: 1 + 2})
	model = updated.(EditorModel)
	if row, col := model.vim.Cursor(); row != 2 || col != 3 {
		t.Fatalf("cursor %d,%d", row, col)
	}
	model, _ = typeEditor(model, "x")
	view := stripANSI(model.View())
	lines := strings.Split(view, "\n")
	footer := lines[len(lines)-1]
	x := strings.Index(footer, "[:w]") // at 80 columns the labels are already cut
	if x < 0 {
		t.Fatalf("footer %q", footer)
	}
	updated, _ = model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: len([]rune(footer[:x])), Y: len(lines) - 1})
	if after := updated.(EditorModel); after.vim.Modified() {
		t.Fatal("[:w] must save")
	}
}

func TestEditor_NewFileIsCreatedOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	doc, err := editor.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := NewEditorModel(doc).Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	model, _ := typeEditor(updated.(EditorModel), "ihello<esc>:w<cr>")
	if data, _ := os.ReadFile(path); string(data) != "hello\n" {
		t.Fatalf("file %q (%s)", data, model.View())
	}
}

func TestEditor_QuitsWithCtrlD_LikeCtrlC(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlD, tea.KeyCtrlC} {
		model, _ := newEditorForTest(t, "a\nb\n")
		_, cmd := model.Update(tea.KeyMsg{Type: key})
		if !isQuit(cmd) {
			t.Errorf("key %v must quit when nothing changed", key)
		}
		model, _ = typeEditor(model, "x")
		updated, cmd := model.Update(tea.KeyMsg{Type: key})
		if isQuit(cmd) || !strings.Contains(updated.View(), "Unsaved changes") {
			t.Errorf("key %v must warn first:\n%s", key, updated.View())
		}
		if _, cmd = updated.Update(tea.KeyMsg{Type: key}); !isQuit(cmd) {
			t.Errorf("key %v must quit on the second press", key)
		}
		// any other key disarms the warning
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
		if _, cmd = updated.Update(tea.KeyMsg{Type: key}); isQuit(cmd) {
			t.Errorf("key %v: the warning must start over after another key", key)
		}
	}
}

func TestEditor_CtrlDAfterSaveQuitsAtOnce(t *testing.T) {
	model, _ := newEditorForTest(t, "a\n")
	model, _ = typeEditor(model, "x:w<cr>")
	if _, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD}); !isQuit(cmd) {
		t.Fatal("a saved buffer must close with ctrl+d")
	}
}

func TestEditor_CtrlCLeavesInsertModeWithoutQuitting(t *testing.T) {
	model, _ := newEditorForTest(t, "a\n")
	model, _ = typeEditor(model, "ix")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if isQuit(cmd) || updated.(EditorModel).vim.Mode().String() != "-- NORMAL --" {
		t.Fatalf("ctrl+c in insert mode must only leave insert mode (mode %v)", updated.(EditorModel).vim.Mode())
	}
}

func TestProject_QuitsWithCtrlDFromEditorAndStoresSession(t *testing.T) {
	model, fx := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = settleProject(updated.(ProjectModel), cmd)
	if model.dlg == nil || len(fx.sessions) != 0 {
		t.Fatal("ctrl+d must ask before it closes the window")
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlD}) // the same key again answers yes
	if !isQuit(cmd) || len(fx.sessions) != 1 || updated.(ProjectModel).focus != focusProjectEdit {
		t.Fatalf("ctrl+d in the editor must close the window and store the session: sessions %d", len(fx.sessions))
	}
}

func TestEditor_CtrlDBehavesLikeCtrlCInEveryMode(t *testing.T) {
	setups := map[string]string{"insert mode": "ix", "visual mode": "vl", "command mode": ":w", "pending operator": "d", "pending count": "2"}
	for name, keys := range setups {
		ctrlC, _ := newEditorForTest(t, "abc\n")
		ctrlD, _ := newEditorForTest(t, "abc\n")
		ctrlC, _ = typeEditor(ctrlC, keys)
		ctrlD, _ = typeEditor(ctrlD, keys)
		afterC, cmdC := ctrlC.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		afterD, cmdD := ctrlD.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
		c, d := afterC.(EditorModel), afterD.(EditorModel)
		if isQuit(cmdC) != isQuit(cmdD) || c.vim.Mode() != d.vim.Mode() || c.vim.Pending() != d.vim.Pending() ||
			strings.Join(c.Lines(), "\n") != strings.Join(d.Lines(), "\n") {
			t.Errorf("%s: ctrl+d (mode %v, pending %q, quit %v) must equal ctrl+c (mode %v, pending %q, quit %v)",
				name, d.vim.Mode(), d.vim.Pending(), isQuit(cmdD), c.vim.Mode(), c.vim.Pending(), isQuit(cmdC))
		}
	}
	model, _ := newEditorForTest(t, "abc\n")
	model, _ = typeEditor(model, "ix")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if isQuit(cmd) || updated.(EditorModel).vim.Mode().String() != "-- NORMAL --" {
		t.Fatal("ctrl+d in insert mode must only leave insert mode, like ctrl+c")
	}
	if _, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyCtrlD}); isQuit(cmd) {
		t.Fatal("with unsaved changes the next ctrl+d only warns, like ctrl+c")
	}
}

// memoryClipboard is a clipboard in memory.
type memoryClipboard struct{ text string }

func (c *memoryClipboard) read() (string, error) { return c.text, nil }
func (c *memoryClipboard) write(text string) error {
	c.text = text
	return nil
}

func TestEditor_CopyGoesToClipboardAndPasteComesFromIt(t *testing.T) {
	model, _ := newEditorForTest(t, "alpha\nbeta\n")
	clip := &memoryClipboard{}
	model = model.WithClipboard(clip.read, clip.write)
	model, _ = typeEditor(model, "yy")
	model.vim.FlushClipboard()
	if clip.text != "alpha\n" {
		t.Fatalf("yy must copy to the system clipboard: %q", clip.text)
	}
	clip.text = "from the browser"
	model, _ = typeEditor(model, "jp")
	if got := model.Lines(); got[1] != "bfrom the browsereta" {
		t.Fatalf("p must paste the system clipboard: %q", got)
	}
}

func TestEditor_TerminalPasteIsTextNotCommands(t *testing.T) {
	model, _ := newEditorForTest(t, "abc\n")
	paste := func(m EditorModel, text string) EditorModel {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
		return updated.(EditorModel)
	}
	model = paste(model, "dd\nx")
	if got := model.Lines(); len(got) != 2 || !strings.Contains(strings.Join(got, "|"), "dd") {
		t.Fatalf("pasted text must not run as vim commands: %q", got)
	}
	model, _ = typeEditor(model, "ggAz")
	model = paste(model, "1\r\n2")
	if got := model.Lines(); got[0] != "adbcz1" && !strings.HasPrefix(got[0], "a") {
		t.Fatalf("paste in insert mode: %q", got)
	}
	if !strings.Contains(strings.Join(model.Lines(), "|"), "z1|2") {
		t.Fatalf("insert mode paste keeps line breaks: %q", model.Lines())
	}
}

func TestEditor_FlushesClipboardWhenQuitting(t *testing.T) {
	model, _ := newEditorForTest(t, "abc\n")
	slow := &memoryClipboard{}
	model = model.WithClipboard(slow.read, func(text string) error {
		time.Sleep(30 * time.Millisecond)
		return slow.write(text)
	})
	model, _ = typeEditor(model, "yy")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !isQuit(cmd) || slow.text != "abc\n" {
		t.Fatalf("the copy must be on the clipboard when the editor is left: %q (%v)", slow.text, updated.(EditorModel).Quitting())
	}
}

func TestEditor_SearchPromptJumpsAndHighlights(t *testing.T) {
	model, _ := newEditorForTest(t, "alpha\nbeta needle\ngamma\nneedle again\n")
	model, _ = typeEditor(model, "/")
	if view := stripANSI(model.View()); !strings.Contains(view, "\n/") {
		t.Fatalf("the prompt must start with /:\n%s", view)
	}
	model, _ = typeEditor(model, "needle<cr>")
	if row, col := model.Cursor(); row != 1 || col != 5 {
		t.Fatalf("cursor %d,%d", row, col)
	}
	if model, _ = typeEditor(model, "n"); model.vim.Mode() != 0 {
		t.Fatal("n stays in normal mode")
	}
	if row, col := model.Cursor(); row != 3 || col != 0 {
		t.Fatalf("n must find the next match: %d,%d", row, col)
	}
	if model.warn {
		t.Fatalf("a found match is no warning: %q", model.status)
	}
	if got := model.vim.Matches(1); len(got) != 1 || got[0] != [2]int{5, 11} {
		t.Fatalf("matches %v", got)
	}
	model, _ = typeEditor(model, "/zzz<cr>")
	if !model.warn || !strings.Contains(model.status, "E486") {
		t.Fatalf("a missing pattern must warn: %q", model.status)
	}
}
