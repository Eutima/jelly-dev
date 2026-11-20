package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newTestModel(oldLines, newLines []string) DiffModel {
	model := NewDiffModel("a.txt", "b.txt", oldLines, newLines)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	return updated.(DiffModel)
}

func press(model DiffModel, key string) DiffModel {
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated.(DiffModel)
}

func TestBuildRows_PairsLinesAndAddsFillers_WhenChangeHasUnequalSides(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"a", "b", "c"}, []string{"a", "X", "Y", "Z", "c"})
	if len(model.rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(model.rows))
	}
	if model.rows[1].Left.Kind != CellModified || model.rows[2].Left.Kind != CellFiller ||
		model.rows[2].Right.Kind != CellAdded {
		t.Fatalf("unexpected rows: %+v", model.rows)
	}
	if len(model.hunkStarts) != 1 || model.hunkStarts[0] != 1 {
		t.Fatalf("unexpected hunk starts: %v", model.hunkStarts)
	}
}

func TestBuildRows_NumbersLinesFromOne_WhenBlocksFollowEachOther(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"a", "b"}, []string{"a", "c"})
	if model.rows[1].Left.No != 2 || model.rows[1].Right.No != 2 {
		t.Fatalf("unexpected numbers: %+v", model.rows[1])
	}
}

func TestView_ShowsBothFileNamesAndContent_WhenSizeKnown(t *testing.T) {
	view := newTestModel([]string{"alpha"}, []string{"beta"}).View()
	for _, want := range []string{"a.txt", "b.txt", "alpha", "beta", "1 changes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
}

func TestUpdate_SelectsNextChangeAndWraps_WhenNPressed(t *testing.T) {
	model := newTestModel([]string{"a", "b", "c", "d", "e"}, []string{"a", "B", "c", "D", "e"})
	model = press(model, "n")
	if model.current != 0 {
		t.Fatalf("want hunk 0, got %d", model.current)
	}
	model = press(press(model, "n"), "n")
	if model.current != 0 {
		t.Fatalf("want wrap to 0, got %d", model.current)
	}
}

func TestUpdate_SelectsLastChange_WhenPPressedFirst(t *testing.T) {
	model := newTestModel([]string{"a", "b", "c"}, []string{"a", "B", "c", "D"})
	if model = press(model, "p"); model.current != 1 {
		t.Fatalf("want hunk 1, got %d", model.current)
	}
}

func TestUpdate_Quits_WhenQPressed(t *testing.T) {
	_, cmd := newTestModel([]string{"a"}, []string{"b"}).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func manyLines(count int) []string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return lines
}

func TestUpdate_ClampsScroll_WhenScrollingPastEnd(t *testing.T) {
	newLines := manyLines(50)
	newLines[0] = "changed"
	model := newTestModel(manyLines(50), newLines)
	model = press(press(model, "G"), "j")
	if want := len(model.rows) - model.bodyHeight(); model.offset != want {
		t.Fatalf("offset %d, want %d", model.offset, want)
	}
	if model = press(press(model, "g"), "k"); model.offset != 0 {
		t.Fatalf("offset %d, want 0", model.offset)
	}
}

func TestUpdate_ScrollsHunkIntoView_WhenNPressed(t *testing.T) {
	newLines := manyLines(50)
	newLines[30] = "changed"
	model := press(newTestModel(manyLines(50), newLines), "n")
	if want := model.hunkStarts[0] - model.bodyHeight()/3; model.offset != want {
		t.Fatalf("offset %d, want %d", model.offset, want)
	}
}

func TestUpdate_WrapsToLastChange_WhenPPressedOnFirst(t *testing.T) {
	model := newTestModel([]string{"a", "b", "c"}, []string{"A", "b", "C"})
	model = press(press(model, "n"), "p")
	if model.current != 1 {
		t.Fatalf("want hunk 1, got %d", model.current)
	}
}

func TestUpdate_ScrollsByWheel_WhenMouseWheelUsed(t *testing.T) {
	newLines := manyLines(50)
	newLines[0] = "changed"
	model := newTestModel(manyLines(50), newLines)
	updated, _ := model.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if got := updated.(DiffModel).offset; got != wheelLines {
		t.Fatalf("offset %d, want %d", got, wheelLines)
	}
}

func TestView_ShowsGutterArrows_WhenLinesOnlyOnOneSide(t *testing.T) {
	view := newTestModel([]string{"a"}, []string{"a", "b"}).View()
	if !strings.Contains(view, "«") {
		t.Fatalf("missing « in:\n%s", view)
	}
	view = newTestModel([]string{"a", "b"}, []string{"a"}).View()
	if !strings.Contains(view, "»") {
		t.Fatalf("missing » in:\n%s", view)
	}
}

func TestView_StripsControlCharacters_WhenFileContainsEscapes(t *testing.T) {
	model := NewDiffModel("evil\x1b]0;x\x07.txt", "b", []string{"\x1b[31mred\rX"}, []string{"ok"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	view := updated.(DiffModel).View()
	for _, bad := range []string{"\x1b[31m", "\x1b]0", "\x07", "\r"} {
		if strings.Contains(view, bad) {
			t.Fatalf("view contains %q", bad)
		}
	}
	if !strings.Contains(view, "·[31mred·X") {
		t.Fatalf("expected sanitized text in:\n%s", view)
	}
}

func TestSanitize_AlignsToTabStops_WhenTabFollowsText(t *testing.T) {
	if got := sanitize("ab\tc"); got != "ab  c" {
		t.Fatalf("got %q", got)
	}
}

func TestView_TruncatesWideRunes_WhenLineTooLong(t *testing.T) {
	model := NewDiffModel("a", "b", []string{strings.Repeat("日本語", 40)}, []string{"x"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	for _, line := range strings.Split(updated.(DiffModel).View(), "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("line width %d > 40: %q", width, line)
		}
	}
}

func TestView_FitsWidth_WhenTerminalVeryNarrow(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"hello"}, []string{"world"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 12, Height: 6})
	for _, line := range strings.Split(updated.(DiffModel).View(), "\n") {
		if width := lipgloss.Width(line); width > 12 {
			t.Fatalf("line width %d > 12: %q", width, line)
		}
	}
}

func TestView_DoesNotPanic_WhenTerminalTinyOrOddWidth(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 41} {
		model := NewDiffModel("a", "b", []string{"hello"}, []string{"world"})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 6})
		for _, line := range strings.Split(updated.(DiffModel).View(), "\n") {
			if got := lipgloss.Width(line); got > width && width >= gutterWidth {
				t.Fatalf("width %d: line is %d wide", width, got)
			}
		}
	}
}

func TestSanitize_ReplacesBidiOverrides_WhenTextContainsThem(t *testing.T) {
	if got := sanitize("a\u202eb"); got != "a·b" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderCell_NeverExceedsCellWidth_WhenPaneNarrowerThanNumberColumn(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"x"}, []string{"y"})
	cell := Cell{Kind: CellModified, No: 12345, Text: "some long text"}
	for cellWidth := 0; cellWidth <= 12; cellWidth++ {
		if got := lipgloss.Width(model.theme.renderCell(cell, 6, cellWidth)); got != cellWidth {
			t.Fatalf("cell width %d rendered %d wide", cellWidth, got)
		}
	}
}

func TestRenderCell_NeverExceedsCellWidth_WhenCellIsFiller(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"x"}, []string{"y"})
	if got := lipgloss.Width(model.theme.renderCell(Cell{Kind: CellFiller}, 3, 5)); got != 5 {
		t.Fatalf("rendered %d wide", got)
	}
}

func TestView_FillsExactWidth_WhenTerminalWidthIsOdd(t *testing.T) {
	model := NewDiffModel("a", "b", []string{"hello"}, []string{"world"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 41, Height: 6})
	lines := strings.Split(updated.(DiffModel).View(), "\n")
	for i, line := range lines {
		if line == "" {
			continue // padding below the last row
		}
		if got := lipgloss.Width(line); got != 41 {
			t.Fatalf("line %d is %d wide, want 41: %q", i, got, line)
		}
	}
}

func TestUpdate_KeepsScrollPosition_WhenTogglingWithoutSelection(t *testing.T) {
	changed := manyLines(60)
	changed[0] = "changed"
	model := press(newTestModel(manyLines(60), changed), "j")
	model = press(model, "t")
	if model.offset != 1 {
		t.Fatalf("offset %d, want 1", model.offset)
	}
}
