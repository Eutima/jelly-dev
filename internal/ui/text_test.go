package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/diff"
)

func TestSegments_MarkHighlight_WhenSpansGiven(t *testing.T) {
	got := groupSegments(displayCells("let foo = 1", []diff.Span{{Start: 4, End: 7}}, nil, nil))
	want := []segment{{text: "let "}, {text: "foo", highlight: true}, {text: " = 1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSegments_KeepHighlightOnRightRunes_WhenTabPrecedesSpan(t *testing.T) {
	got := groupSegments(displayCells("\tfoo", []diff.Span{{Start: 1, End: 4}}, nil, nil))
	want := []segment{{text: "    "}, {text: "foo", highlight: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRenderSyntaxLines_WrapsWithoutCutting_WhenTextTooLong(t *testing.T) {
	base := lipgloss.NewStyle()
	lines := renderSyntaxLines("abcdefghij", nil, nil, nil, 5, base, base)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) != 5 || strings.Contains(line, "…") {
			t.Errorf("line %q is cut or not 5 wide", line)
		}
	}
}

func TestRenderSyntaxLines_FitsWidth_WhenWideRunesDoNotFitExactly(t *testing.T) {
	base := lipgloss.NewStyle()
	for _, line := range renderSyntaxLines("日本語日本語", nil, nil, nil, 5, base, base) {
		if got := lipgloss.Width(line); got != 5 {
			t.Fatalf("width %d", got)
		}
	}
}

func TestBuildRows_AddsWordSpans_WhenLinesPaired(t *testing.T) {
	rows, _ := BuildRows(diff.Lines([]string{"let foo = 1"}, []string{"let bar = 1"}))
	want := []diff.Span{{Start: 4, End: 7}}
	if !reflect.DeepEqual(rows[0].Left.Spans, want) || !reflect.DeepEqual(rows[0].Right.Spans, want) {
		t.Fatalf("spans: %+v / %+v", rows[0].Left.Spans, rows[0].Right.Spans)
	}
}

func TestBuildUnified_ListsDeletedBeforeAdded_WhenLinesChange(t *testing.T) {
	rows := BuildUnified(diff.Lines([]string{"a", "b", "c"}, []string{"a", "X", "c"}))
	if len(rows) != 4 {
		t.Fatalf("rows: %+v", rows)
	}
	deleted, added := rows[1], rows[2]
	if deleted.Kind != CellDeleted || deleted.OldNo != 2 || deleted.NewNo != 0 ||
		added.Kind != CellAdded || added.NewNo != 2 || added.OldNo != 0 {
		t.Fatalf("unexpected rows: %+v %+v", deleted, added)
	}
	if rows[0].OldNo != 1 || rows[0].NewNo != 1 || rows[3].OldNo != 3 || rows[3].NewNo != 3 {
		t.Fatalf("unexpected equal rows: %+v %+v", rows[0], rows[3])
	}
}

func TestBuildUnified_PairsWordSpansByIndex_WhenBlockUnequal(t *testing.T) {
	rows := BuildUnified(diff.Lines([]string{"foo 1"}, []string{"bar 1", "extra"}))
	if len(rows[0].Spans) == 0 || len(rows[1].Spans) == 0 || len(rows[2].Spans) != 0 {
		t.Fatalf("spans: %+v", rows)
	}
}

func TestUpdate_TogglesUnifiedView_WhenTPressed(t *testing.T) {
	model := press(newTestModel([]string{"alpha"}, []string{"beta"}), "t")
	if !model.showUnified {
		t.Fatal("expected unified view")
	}
	view := model.View()
	for _, want := range []string{"a.txt → b.txt", "- ", "+ ", "alpha", "beta"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	if model = press(model, "t"); model.showUnified {
		t.Fatal("expected split view again")
	}
}

func TestView_FillsExactWidth_WhenUnifiedView(t *testing.T) {
	model := press(newTestModel([]string{"a", "日本語"}, []string{"a", "x"}), "t")
	for i, line := range strings.Split(model.View(), "\n") {
		if line == "" {
			continue
		}
		if got := lipgloss.Width(line); got != 80 {
			t.Fatalf("line %d is %d wide: %q", i, got, line)
		}
	}
}

func TestUpdate_NavigatesChangesInUnifiedView_WhenNPressed(t *testing.T) {
	model := press(newTestModel([]string{"a", "b", "c", "d"}, []string{"A", "b", "c", "D"}), "t")
	model = press(model, "down")
	if model.current != 0 || model.offset != 0 {
		t.Fatalf("current %d offset %d", model.current, model.offset)
	}
	if model = press(model, "down"); model.current != 1 {
		t.Fatalf("current %d", model.current)
	}
}

func TestUpdate_KeepsSelectedChange_WhenTogglingView(t *testing.T) {
	lines := manyLines(60)
	changed := manyLines(60)
	changed[40] = "changed"
	model := press(press(newTestModel(lines, changed), "down"), "t")
	if model.current != 0 || model.offset != model.unifiedAt[0]-model.bodyHeight()/3 {
		t.Fatalf("current %d offset %d", model.current, model.offset)
	}
}

func TestSanitize_ReplacesControlCharacters_WhenTextContainsEscapes(t *testing.T) {
	got := sanitize("a\x1b[31m\x7f \u0085b")
	if got != "a·[31m···b" {
		t.Fatalf("got %q", got)
	}
}
