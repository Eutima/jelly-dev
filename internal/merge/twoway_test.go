package merge

import (
	"reflect"
	"testing"
)

func TestTwoWay_HasNoConflict_WhenTextsAreIdentical(t *testing.T) {
	chunks := TwoWay(lines("a\nb\n"), lines("a\nb\n"))
	if HasConflicts(chunks) || !reflect.DeepEqual(Assemble(chunks, nil), lines("a\nb\n")) {
		t.Fatalf("chunks %+v", chunks)
	}
}

func TestTwoWay_MakesEveryDifferenceAConflict_WhenReplacedInsertedOrDeleted(t *testing.T) {
	a := lines("1\nold\n3\n4\n5\ngone\n7\n")
	b := lines("1\nnew\n3\n4\n5\n7\nadded\n")
	chunks := TwoWay(a, b)
	if got := ConflictCount(chunks); got != 3 {
		t.Fatalf("%d conflicts: %+v", got, chunks)
	}
	if first := chunks[1]; first.Kind != Conflict || !reflect.DeepEqual(first.Ours, lines("old\n")) ||
		!reflect.DeepEqual(first.Theirs, lines("new\n")) || len(first.Base) != 0 {
		t.Fatalf("replacement chunk %+v", first)
	}
	if got := Assemble(chunks, nil); !reflect.DeepEqual(got, a) {
		t.Fatalf("ours must give a again: %v", got)
	}
	if got := Assemble(chunks, func(c Chunk) []string { return c.Theirs }); !reflect.DeepEqual(got, b) {
		t.Fatalf("theirs must give b: %v", got)
	}
}

func TestTwoWay_IsOneConflict_WhenOneSideIsEmpty(t *testing.T) {
	chunks := TwoWay(nil, lines("x\ny\n"))
	if len(chunks) != 1 || chunks[0].Kind != Conflict || len(chunks[0].Ours) != 0 || len(chunks[0].Theirs) != 2 {
		t.Fatalf("chunks %+v", chunks)
	}
}
