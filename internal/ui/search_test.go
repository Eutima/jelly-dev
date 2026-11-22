package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() { searchDebounce = time.Millisecond }

var searchFilesFixture = map[string]string{
	"README.md":                 "# Demo\nsee the quickstart\n",
	"src/main.go":               "package main\n\nfunc main() {\n\tneedle()\n}\n",
	"src/util/needle.go":        "package util\n\nfunc needle() {}\n",
	"node_modules/dep/index.js": "needle in a dependency\n",
	".venv/lib/python/site.py":  "needle in a venv\n",
	"docs/notes.txt":            "nothing here\n",
}

// settle feeds the commands a model returned back into it, like the runtime does, until none is left.
func settleProject(model ProjectModel, cmd tea.Cmd) ProjectModel {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return model
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				model = settleProject(model, c)
			}
			return model
		}
		if _, quit := msg.(tea.QuitMsg); quit {
			return model
		}
		var updated tea.Model
		updated, cmd = model.Update(msg)
		model = updated.(ProjectModel)
	}
	return model
}

func projectSend(model ProjectModel, msgs ...tea.Msg) ProjectModel {
	for _, msg := range msgs {
		updated, cmd := model.Update(msg)
		model = settleProject(updated.(ProjectModel), cmd)
	}
	return model
}

func runeKeys(text string) []tea.Msg {
	var out []tea.Msg
	for _, r := range text {
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

func TestSearch_FindsFilesByNameAndOpensOne(t *testing.T) {
	model, fx := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.search == nil {
		t.Fatal("ctrl+f must open the search")
	}
	model = projectSend(model, runeKeys("needle")...)
	view := stripANSI(model.View())
	if !strings.Contains(view, "needle.go") || strings.Contains(view, "node_modules") || strings.Contains(view, ".venv") {
		t.Fatalf("results:\n%s", view)
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.search != nil || model.Editor().Path() != filepath.Join(fx.root, "src", "util", "needle.go") {
		t.Fatalf("enter must open the file: %q", model.Editor().Path())
	}
}

func TestSearch_FindsTextInFilesAndJumpsToTheLine(t *testing.T) {
	model, fx := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	// the content search starts after a short pause: deliver the pause by hand
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	view := stripANSI(model.View())
	for _, want := range []string{"src/main.go:4", "src/util/needle.go:3", "2 matches"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "node_modules") || strings.Contains(view, ".venv") {
		t.Fatalf("dependency folders must be skipped:\n%s", view)
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	row, col := model.Editor().Cursor()
	if model.Editor().Path() != filepath.Join(fx.root, "src", "util", "needle.go") || row != 2 || col != 5 {
		t.Fatalf("path %q cursor %d,%d", model.Editor().Path(), row, col)
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) // normal mode key still works
	_ = model
}

func TestSearch_TabSwitchesModeAndKeepsQuery(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	model = projectSend(model, runeKeys("demo")...)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyTab})
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	view := stripANSI(model.View())
	if model.search.mode != searchText || !strings.Contains(view, "README.md:1") || !strings.Contains(view, "> demo") {
		t.Fatalf("the query must carry over to the content search (smart case):\n%s", view)
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyTab})
	if model.search.mode != searchFiles {
		t.Fatal("tab must switch back")
	}
}

func TestSearch_EscapeClosesAndKeepsEditorState(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF}, tea.KeyMsg{Type: tea.KeyEsc})
	if model.search != nil || model.focus != focusTree {
		t.Fatalf("esc must close the search (search %v)", model.search)
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG}, tea.KeyMsg{Type: tea.KeyCtrlD})
	if model.search != nil {
		t.Fatal("ctrl+d must close the search like ctrl+c")
	}
}

func TestSearch_OpensFromEditorAndCommands(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}, tea.KeyMsg{Type: tea.KeyEnter}) // README.md opens, the editor has the focus
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.search == nil || model.search.mode != searchFiles {
		t.Fatal("ctrl+f in the editor's normal mode must open the file search")
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyEsc})
	model, _ = typeCommand(model, "grep quick")
	model = settleProject(model, nil)
	if model.search == nil || model.search.mode != searchText || string(model.search.input) != "quick" {
		t.Fatalf("`:grep quick` must open the content search with the query: %+v", model.search)
	}
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if !strings.Contains(stripANSI(model.View()), "README.md:2") {
		t.Fatalf("view:\n%s", model.View())
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyEsc})
	model, _ = typeCommand(model, "find util")
	if model.search == nil || model.search.mode != searchFiles || string(model.search.input) != "util" {
		t.Fatalf("`:find util` must open the file search: %+v", model.search)
	}
}

func TestSearch_InsertModeKeepsCtrlF(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}, tea.KeyMsg{Type: tea.KeyEnter}) // README.md is the last row
	model = projectSend(model, runeKeys("i")...)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.search != nil {
		t.Fatal("ctrl+f in insert mode must not open the search")
	}
}

func TestSearch_RegexToggleAndInvalidPattern(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG}, tea.KeyMsg{Type: tea.KeyCtrlR})
	model = projectSend(model, runeKeys(`func \w+\(`)...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	view := stripANSI(model.View())
	if !strings.Contains(view, "regex") || !strings.Contains(view, "src/main.go:3") {
		t.Fatalf("regex search:\n%s", view)
	}
	model = projectSend(model, runeKeys("(")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if !strings.Contains(stripANSI(model.View()), "invalid regular expression") {
		t.Fatalf("an invalid pattern must be explained:\n%s", model.View())
	}
}

func TestSearch_IgnoresAnswersOfOlderQueries(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	for _, key := range runeKeys("needle") { // like the runtime, but without waiting for the pause
		updated, _ := model.Update(key)
		model = updated.(ProjectModel)
	}
	oldSeq := model.search.seq
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // the query changed: seq moves on
	model = updated.(ProjectModel)
	updated, _ = model.Update(textResultMsg{seq: oldSeq, matches: nil})
	model = updated.(ProjectModel)
	if !model.search.searching {
		t.Fatal("an answer to an older query must be ignored")
	}
}

func TestSearch_MouseClickOpensResultAndFitsScreen(t *testing.T) {
	model, fx := newProjectFixture(t, 100, searchFilesFixture)
	for _, size := range [][2]int{{30, 8}, {100, 14}} {
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m := projectSend(updated.(ProjectModel), tea.KeyMsg{Type: tea.KeyCtrlF})
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("size %v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("size %v: line %d is %d wide", size, i, lipgloss.Width(line))
			}
		}
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	model = projectSend(model, runeKeys("notes")...)
	model = projectSend(model, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 4, Y: 2})
	if model.Editor().Path() != filepath.Join(fx.root, "docs", "notes.txt") {
		t.Fatalf("a click must open the result: %q", model.Editor().Path())
	}
}

func TestSearch_BackJumpReturnsToPlaceBeforeSearch(t *testing.T) {
	model, fx := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	model = projectSend(model, runeKeys("main.go")...)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyEnter})
	model = projectSend(model, runeKeys("jj")...)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	model = projectSend(model, runeKeys("needle.go")...)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.Editor().Path() != filepath.Join(fx.root, "src", "util", "needle.go") {
		t.Fatalf("path %q", model.Editor().Path())
	}
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlB})
	if row, _ := model.Editor().Cursor(); model.Editor().Path() != filepath.Join(fx.root, "src", "main.go") || row != 2 {
		t.Fatalf("ctrl+b must return to the place before the search: %q row %d", model.Editor().Path(), row)
	}
}

func TestSearch_CaseToggleMakesALowerCaseQueryCaseSensitive(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("demo")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if view := stripANSI(model.View()); !strings.Contains(view, "README.md:1") {
		t.Fatalf("smart case: a lower case query must find '# Demo':\n%s", view)
	}
	model = projectSend(model, keyMsg("alt+c"))
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	view := stripANSI(model.View())
	if strings.Contains(view, "README.md:1") || !strings.Contains(view, "case") {
		t.Fatalf("alt+c must make the search case sensitive:\n%s", view)
	}
	model = projectSend(model, keyMsg("alt+c"))
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if view := stripANSI(model.View()); !strings.Contains(view, "README.md:1") {
		t.Fatalf("a second alt+c must switch back to smart case:\n%s", view)
	}
}

func TestSearch_FileNamesAreFuzzyUntilAltFSwitchesToContains(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	model = projectSend(model, runeKeys("ndl")...) // the letters of needle, spread over the name

	view := stripANSI(model.View())
	if !strings.Contains(view, "needle.go") || !strings.Contains(view, "fuzzy") {
		t.Fatalf("fuzzy is the default and finds needle.go with ndl:\n%s", view)
	}
	model = projectSend(model, keyMsg("alt+f"))
	view = stripANSI(model.View())
	if strings.Contains(view, "needle.go") || !strings.Contains(view, "contains") {
		t.Fatalf("alt+f must switch to contains, where ndl finds nothing:\n%s", view)
	}
	model = projectSend(model, keyMsg("ctrl+u"))
	for range 3 {
		model = projectSend(model, keyMsg("backspace"))
	}
	model = projectSend(model, runeKeys("EEDL")...) // in one piece, other case
	if view := stripANSI(model.View()); !strings.Contains(view, "needle.go") {
		t.Fatalf("contains ignores case and finds eedl in needle.go:\n%s", view)
	}
	model = projectSend(model, keyMsg("alt+f"))
	if view := stripANSI(model.View()); !strings.Contains(view, "fuzzy") {
		t.Fatalf("a second alt+f must switch back to fuzzy:\n%s", view)
	}
}

func TestSearch_AltFDoesNothingInTheTextTab(t *testing.T) {
	model, _ := newProjectFixture(t, 100, searchFilesFixture)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, keyMsg("alt+f"))
	if model.search.containsOnly {
		t.Error("alt+f changed the file name matching from the text tab")
	}
}

// previewFixture is a file of 30 lines (line N reads "line N") with one hit, in a window high enough for the preview.
func previewFixture(t *testing.T, hitLine int) ProjectModel {
	t.Helper()
	var lines []string
	for i := 1; i <= 30; i++ {
		text := "line " + strconv.Itoa(i)
		if i == hitLine {
			text = "the needle is here"
		}
		lines = append(lines, text)
	}
	model, _ := newProjectFixture(t, 100, map[string]string{"src/big.go": strings.Join(lines, "\n") + "\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	return projectSend(model, searchTickMsg{seq: model.search.seq})
}

func TestSearch_PreviewShowsFiveLinesAroundTheSelectedMatch(t *testing.T) {
	view := stripANSI(previewFixture(t, 15).View())
	for _, want := range []string{"src/big.go:15", "10 │ line 10", "14 │ line 14", "15 │ the needle is here", "16 │ line 16", "20 │ line 20"} {
		if !strings.Contains(view, want) {
			t.Errorf("the preview lacks %q:\n%s", want, view)
		}
	}
	for _, not := range []string{"9 │ line 9", "21 │ line 21"} {
		if strings.Contains(view, not) {
			t.Errorf("the preview shows %q, which is more than five lines away:\n%s", not, view)
		}
	}
}

func TestSearch_PreviewClampsAtTheStartOfTheFile(t *testing.T) {
	view := stripANSI(previewFixture(t, 2).View())
	if !strings.Contains(view, "1 │ line 1") || !strings.Contains(view, "7 │ line 7") || strings.Contains(view, "8 │ line 8") {
		t.Errorf("a hit on line 2 shows lines 1 to 7:\n%s", view)
	}
}

func TestSearch_PreviewIsOnlyInTheTextTabAndNeedsRoom(t *testing.T) {
	model := previewFixture(t, 15)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyTab}) // the files tab
	if view := stripANSI(model.View()); strings.Contains(view, "14 │ line 14") {
		t.Errorf("the files tab shows a preview:\n%s", view)
	}
	small, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	model = small.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyTab}) // back to the text tab
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if view := stripANSI(model.View()); strings.Contains(view, "14 │ line 14") {
		t.Errorf("a window of 12 lines shows a preview:\n%s", view)
	}
}

func TestSearch_PreviewWrapsALongLineWithoutCutting(t *testing.T) {
	long := strings.Repeat("0123456789", 20)
	model, _ := newProjectFixture(t, 100, map[string]string{"a.txt": "one\nneedle " + long + "\nthree\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	model = updated.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	view := stripANSI(model.View())
	if strings.Contains(view, "…") || strings.Count(view, "9") != 40 { // 20 in the result row and 20 in the preview
		t.Errorf("the long line is cut or doubled (%d nines, want 40):\n%s", strings.Count(view, "9"), view)
	}
}

func TestSearch_PreviewSaysSoWhenTheFileIsGoneOrChanged(t *testing.T) {
	model, fx := newProjectFixture(t, 100, map[string]string{"a.txt": "one\ntwo needle\nthree\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	if view := stripANSI(model.View()); !strings.Contains(view, "2 │ two needle") {
		t.Fatalf("no preview before the file changes:\n%s", view)
	}
	if err := os.WriteFile(filepath.Join(fx.root, "a.txt"), []byte("short\n"), 0o644); err != nil { // the hit line is gone
		t.Fatal(err)
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "the file changed: search again") || strings.Contains(view, "two needle\n") && strings.Contains(view, "2 │") {
		t.Errorf("a shrunk file must not show stale lines:\n%s", view)
	}
	if err := os.Remove(filepath.Join(fx.root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "cannot read the file") {
		t.Errorf("a deleted file must say so:\n%s", view)
	}
}

func TestSearch_PreviewOfAHugeLineStaysSmall(t *testing.T) {
	huge := strings.Repeat("x", 500000) + "needle" + strings.Repeat("y", 400000)
	model, _ := newProjectFixture(t, 100, map[string]string{"min.js": "a\n" + huge + "\nb\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	start := time.Now()
	view := stripANSI(model.View())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("drawing the preview of a huge line took %v", elapsed)
	}
	if !strings.Contains(view, "needle") || strings.Count(view, "\n") > 40 {
		t.Errorf("the match must stay visible and the view inside the window:\n%.2000s", view)
	}
}

func TestExpandPreviewText_KeepsTheColumnWithTabsAndAroundALongLine(t *testing.T) {
	runes, col := expandPreviewText("\tfoo", 1)
	if string(runes) != "    foo" || col != 4 {
		t.Errorf("got %q at %d, want the tab as four spaces and the match at 4", string(runes), col)
	}
	long := strings.Repeat("a", 10000) + "X" + strings.Repeat("b", 10000)
	runes, col = expandPreviewText(long, 10000)
	if len(runes) != previewLineRunes || runes[col] != 'X' {
		t.Errorf("kept %d runes, match at %d: %q", len(runes), col, string(runes[max(col-2, 0):min(col+3, len(runes))]))
	}
}
