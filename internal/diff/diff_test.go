package diff

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSplitLines_ReturnsNoLines_WhenTextIsEmpty(t *testing.T) {
	if got := SplitLines(""); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestSplitLines_IgnoresTrailingNewlineAndCRLF_WhenPresent(t *testing.T) {
	got := SplitLines("a\r\nb\n")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLines_ReturnsSingleEqualBlock_WhenInputsMatch(t *testing.T) {
	blocks := Lines([]string{"a", "b"}, []string{"a", "b"})
	if len(blocks) != 1 || blocks[0].Kind != Equal || len(blocks[0].Old) != 2 {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestLines_ReturnsChangeBlockWithBothSides_WhenLineModified(t *testing.T) {
	blocks := Lines([]string{"a", "b", "c"}, []string{"a", "X", "c"})
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %+v", blocks)
	}
	change := blocks[1]
	if change.Kind != Change || !reflect.DeepEqual(change.Old, []string{"b"}) ||
		!reflect.DeepEqual(change.New, []string{"X"}) || change.OldStart != 1 || change.NewStart != 1 {
		t.Fatalf("unexpected change block: %+v", change)
	}
}

func TestLines_ReturnsPureInsertion_WhenLinesAdded(t *testing.T) {
	blocks := Lines([]string{"a"}, []string{"a", "b"})
	last := blocks[len(blocks)-1]
	if last.Kind != Change || len(last.Old) != 0 || !reflect.DeepEqual(last.New, []string{"b"}) {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestLines_ReturnsPureDeletion_WhenOldHasLines_AndNewIsEmpty(t *testing.T) {
	blocks := Lines([]string{"a", "b"}, nil)
	if len(blocks) != 1 || blocks[0].Kind != Change || len(blocks[0].New) != 0 || len(blocks[0].Old) != 2 {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestLines_ReturnsNoBlocks_WhenBothInputsEmpty(t *testing.T) {
	if blocks := Lines(nil, nil); len(blocks) != 0 {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestWords_MarksOnlyChangedWord_WhenOneWordDiffers(t *testing.T) {
	oldSpans, newSpans := Words("let foo = 1", "let bar = 1")
	if !reflect.DeepEqual(oldSpans, []Span{{4, 7}}) || !reflect.DeepEqual(newSpans, []Span{{4, 7}}) {
		t.Fatalf("old=%v new=%v", oldSpans, newSpans)
	}
}

func TestWords_MarksInsertedText_WhenWordAppended(t *testing.T) {
	oldSpans, newSpans := Words("a b", "a b c")
	if len(oldSpans) != 0 || !reflect.DeepEqual(newSpans, []Span{{3, 5}}) {
		t.Fatalf("old=%v new=%v", oldSpans, newSpans)
	}
}

func TestWords_ReturnsNoSpans_WhenLinesIdentical(t *testing.T) {
	oldSpans, newSpans := Words("same", "same")
	if len(oldSpans) != 0 || len(newSpans) != 0 {
		t.Fatalf("old=%v new=%v", oldSpans, newSpans)
	}
}

func TestWords_ReturnsByteOffsets_WhenLineHasMultiByteRunes(t *testing.T) {
	oldSpans, newSpans := Words("é foo", "é bar")
	want := []Span{{3, 6}}
	if !reflect.DeepEqual(oldSpans, want) || !reflect.DeepEqual(newSpans, want) {
		t.Fatalf("old=%v new=%v", oldSpans, newSpans)
	}
}

func TestWords_MergesAdjacentSpans_WhenConsecutiveTokensChange(t *testing.T) {
	oldSpans, _ := Words("a b c", "a x y c")
	if !reflect.DeepEqual(oldSpans, []Span{{2, 3}}) {
		t.Fatalf("old=%v", oldSpans)
	}
}

func TestWords_ReturnsWholeLine_WhenTooManyTokens(t *testing.T) {
	line := strings.Repeat("a ", maxTokens)
	oldSpans, newSpans := Words(line, "b")
	if !reflect.DeepEqual(oldSpans, []Span{{0, len(line)}}) || !reflect.DeepEqual(newSpans, []Span{{0, 1}}) {
		t.Fatalf("old=%v new=%v", oldSpans, newSpans)
	}
}

func TestWords_DoesNotPanic_WhenManyDistinctTokens(t *testing.T) {
	var oldLine, newLine strings.Builder
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&oldLine, "t%d ", i)
		fmt.Fprintf(&newLine, "u%d ", i)
	}
	oldSpans, newSpans := Words(oldLine.String(), newLine.String())
	if len(oldSpans) == 0 || len(newSpans) == 0 {
		t.Fatalf("expected changes, got old=%d new=%d", len(oldSpans), len(newSpans))
	}
}

func TestTokenID_SkipsSurrogates_WhenIndexReachesRange(t *testing.T) {
	if id := tokenID(firstSurrogate - 1); id >= firstSurrogate && id < firstSurrogate+surrogateCount {
		t.Fatalf("id %U in surrogate range", id)
	}
}

func TestLines_TracksStartIndexes_WhenDeletionFollowedByInsertion(t *testing.T) {
	blocks := Lines([]string{"a", "b", "c", "d"}, []string{"a", "X", "Y", "d"})
	change := blocks[1]
	if change.OldStart != 1 || change.NewStart != 1 || len(change.Old) != 2 || len(change.New) != 2 {
		t.Fatalf("unexpected change block: %+v", change)
	}
	if tail := blocks[2]; tail.OldStart != 3 || tail.NewStart != 3 {
		t.Fatalf("unexpected tail block: %+v", tail)
	}
}

func TestSplitLines_KeepsBlankLines_WhenTextHasEmptyLines(t *testing.T) {
	got := SplitLines("a\r\n\r\nb")
	if !reflect.DeepEqual(got, []string{"a", "", "b"}) {
		t.Fatalf("got %v", got)
	}
}
