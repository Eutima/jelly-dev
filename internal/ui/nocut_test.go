package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/lsp"
)

func TestDiffView_WrapsLongLinesInBothViews_AndNeverCutsThem(t *testing.T) {
	long := strings.Repeat("0123456789", 12)
	for _, unified := range []bool{false, true} {
		model := NewDiffModel("a", "b", []string{"x", long}, []string{"x", long + "!"})
		model.showUnified = unified
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		view := stripANSI(updated.(DiffModel).View())
		if strings.Contains(view, "…") {
			t.Errorf("unified=%v: a line is cut:\n%s", unified, view)
		}
		if got := strings.Count(view, "9"); got < 12 {
			t.Errorf("unified=%v: part of the long line is missing (%d nines):\n%s", unified, got, view)
		}
	}
}

func TestDiffView_ScrollsToTheLastRow_WhenRowsWrap(t *testing.T) {
	var old, updated []string
	for i := 0; i < 30; i++ {
		line := strings.Repeat("w", 90)
		old = append(old, line)
		updated = append(updated, line)
	}
	updated[29] = "the last line"
	model := NewDiffModel("a", "b", old, updated)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	model = sized.(DiffModel)
	for _, key := range []string{"G"} {
		next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		model = next.(DiffModel)
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "the last line") {
		t.Errorf("the last row is not reachable:\n%s", view)
	}
}

func TestScrollToShow_MovesTheTopSoTheWholeRowIsVisible(t *testing.T) {
	heights := []int{1, 3, 1, 4, 1}
	height := func(row int) int { return heights[row] }
	if got := scrollToShow(3, 0, 6, 5, height); got != 2 {
		t.Errorf("top = %d, want 2 (rows 2 and 3 take 5 of 6 lines)", got)
	}
	if got := scrollToShow(1, 3, 6, 5, height); got != 1 {
		t.Errorf("top = %d, want 1 (the row is above the top)", got)
	}
	if got := scrollToShow(3, 0, 3, 5, height); got != 3 {
		t.Errorf("top = %d, want 3 (a row taller than the body is shown from its first line)", got)
	}
}

func TestSplitRowLines_KeepsEveryRune(t *testing.T) {
	for _, text := range []string{"", "abc", "abcdefghij", "日本語日本語", "a\tb"} {
		lines := splitRowLines(text, 4)
		if got := strings.Join(lines, ""); got != text {
			t.Errorf("splitRowLines(%q) = %q loses text", text, lines)
		}
	}
}

func TestCompletionPopup_WrapsALongSuggestion_AndNeverCutsIt(t *testing.T) {
	long := strings.Repeat("detail ", 20)
	nav := &fakeNav{completions: []lsp.CompletionItem{{Label: "fooBar", Detail: long}}}
	model := completionEditor(t, nav, "foo\n", "A")
	model = request(model)
	if model.completion == nil {
		t.Fatal("no popup")
	}
	lines := model.completionLines(model.completionWidth(model.textWidth()))
	if len(lines) < 2 {
		t.Errorf("a long suggestion took %d line, want it wrapped", len(lines))
	}
	if joined := stripANSI(strings.Join(lines, "")); strings.Contains(joined, "…") {
		t.Errorf("the suggestion is cut: %q", joined)
	}
}

func TestScrollToShow_LooksOnlyAsFarBackAsTheBodyReaches(t *testing.T) {
	calls := 0
	height := func(int) int { calls++; return 1 }
	if got := scrollToShow(100000, 0, 10, 100001, height); got != 99991 {
		t.Errorf("top = %d, want 99991", got)
	}
	if calls > 20 {
		t.Errorf("height was asked %d times for a body of 10 lines", calls)
	}
}

func TestHoverRows_StopAtTheCapWithoutAMarker(t *testing.T) {
	nav := &fakeNav{hoverText: strings.Repeat("a line of the documentation\n", 80)}
	model, _ := newEditorForTest(t, "x\n")
	model = askDoc(model.WithNavigator(nav, ""))
	rows := model.hoverRows(60)
	if len(rows) != hoverCap(model.height) {
		t.Errorf("%d rows, want the cap %d", len(rows), hoverCap(model.height))
	}
}

func TestWrappedPromptRows_KeepTheEndOfALongCommandLine(t *testing.T) {
	model, _ := newEditorForTest(t, "x\n") // 80 columns, 12 lines
	text := strings.Repeat("0123456789", 40) + "THE-END"
	rows := model.wrappedPromptRows(text, 80)
	if len(rows) > messageCap(model.height) {
		t.Errorf("%d rows, more than the cap %d", len(rows), messageCap(model.height))
	}
	if last := stripANSI(rows[len(rows)-1]); !strings.Contains(last, "THE-END") {
		t.Errorf("the end of the line is not shown: %q", last)
	}
}

func TestScrollToShow_IgnoresAnEmptyList(t *testing.T) {
	height := func(int) int { t.Fatal("height asked for a row of an empty list"); return 1 }
	if got := scrollToShow(0, 0, 5, 0, height); got != 0 {
		t.Errorf("top = %d, want 0", got)
	}
}

func TestSplitRowLines_HandlesWideRunesAndTinyWidths(t *testing.T) {
	if got := splitRowLines("日本", 1); len(got) != 2 || got[0] != "日" || got[1] != "本" {
		t.Errorf("a rune wider than the width must stand alone, got %q", got)
	}
	if got := splitRowLines("日本語", 4); len(got) != 2 || got[0] != "日本" || got[1] != "語" {
		t.Errorf("got %q, want [日本 語]", got)
	}
	if got := splitRowLines("abc", 0); len(got) != 1 || got[0] != "" {
		t.Errorf("a zero width gives one empty line, got %q", got)
	}
	if got := splitRowLines("", 5); len(got) != 1 {
		t.Errorf("empty text is one empty line, got %q", got)
	}
}

func TestDrawRows_FillsTheBodyAndMapsEveryLineToItsRow(t *testing.T) {
	rows := [][]string{{"a"}, {"b1", "b2", "b3"}, {"c"}}
	lines, rowOf := drawRows(1, len(rows), 3, func(row int) []string { return rows[row] })
	if len(lines) != 3 || lines[0] != "b1" || lines[2] != "b3" {
		t.Errorf("lines = %q, want the three lines of row 1", lines)
	}
	for i, row := range rowOf {
		if row != 1 {
			t.Errorf("line %d belongs to row %d, want 1", i, row)
		}
	}
}

func TestCompletionPopup_KeepsItsWidthWhileTheSelectionScrolls(t *testing.T) {
	var items []lsp.CompletionItem
	for i := 0; i < 20; i++ {
		label := "item" + strconv.Itoa(i)
		detail := ""
		if i == 15 {
			detail = "a much longer description than the others have"
		}
		items = append(items, lsp.CompletionItem{Label: label, Detail: detail})
	}
	model := completionEditor(t, &fakeNav{completions: items}, "it\n", "A")
	model = request(model)
	before := model.completionWidth(model.textWidth())
	for range 12 { // down past the first screen of the popup
		model.moveCompletion(1)
	}
	if after := model.completionWidth(model.textWidth()); after != before {
		t.Errorf("the popup changed from %d to %d columns while scrolling", before, after)
	}
	if rows := model.completionLines(before); len(rows) == 0 || !strings.Contains(stripANSI(strings.Join(model.completionLines(before), "\n")), "item12") {
		t.Errorf("the selected suggestion is not in the popup")
	}
}

func TestDiffView_ShowsTheLastRowFromItsStart_WhenItAloneFillsTheWindow(t *testing.T) {
	long := strings.Repeat("0123456789", 30)
	model := NewDiffModel("a", "b", []string{"x", long}, []string{"x", long + "!"})
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	view := stripANSI(sized.(DiffModel).View())
	if !strings.Contains(view, "0123456789") {
		t.Errorf("a tiny window shows nothing of the last row:\n%s", view)
	}
}

func TestGitMessagePane_StaysInsideTheWindow_WhenThePathIsLong(t *testing.T) {
	model, _ := newGitTestModel(60, 9)
	model.files[2].Path = strings.Repeat("deeply/nested/folder/", 8) + "c.bin"
	model.selected = 2
	if lines := strings.Count(model.messageView("a binary file"), "\n") + 1; lines > model.height {
		t.Errorf("the pane takes %d lines in a window of %d", lines, model.height)
	}
}
