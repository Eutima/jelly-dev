package vimbuf

import (
	"reflect"
	"testing"
)

func commentBuffer(lines ...string) *Buffer {
	b := newBuffer(lines...)
	b.SetCommentPrefix("//")
	return b
}

func TestComment_TogglesLine_WhenGcc(t *testing.T) {
	b := commentBuffer("a := 1", "b := 2")
	typeKeys(b, "gcc")
	if got, want := b.Lines(), []string{"// a := 1", "b := 2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commented: %q, want %q", got, want)
	}
	typeKeys(b, "gcc")
	if got, want := b.Lines(), []string{"a := 1", "b := 2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("uncommented: %q, want %q", got, want)
	}
}

func TestComment_CoversCountedLines_WhenCountGcc(t *testing.T) {
	b := commentBuffer("a", "b", "c")
	typeKeys(b, "2gcc")
	if got, want := b.Lines(), []string{"// a", "// b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComment_CoversMotionLines_WhenGcWithMotion(t *testing.T) {
	b := commentBuffer("a", "b", "c")
	typeKeys(b, "gcj")
	if got, want := b.Lines(), []string{"// a", "// b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComment_UsesSelectedLines_WhenVisual(t *testing.T) {
	b := commentBuffer("a", "b", "c")
	typeKeys(b, "Vjgc")
	if got, want := b.Lines(), []string{"// a", "// b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if b.Mode() != Normal {
		t.Fatalf("mode %v, want normal", b.Mode())
	}
}

func TestComment_KeepsIndentAndSkipsBlankLines_WhenRangeMixed(t *testing.T) {
	b := commentBuffer("\tif x {", "", "\t\ty()", "\t}")
	typeKeys(b, "4gcc")
	if got, want := b.Lines(), []string{"\t// if x {", "", "\t// \ty()", "\t// }"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commented: %q, want %q", got, want)
	}
	typeKeys(b, "4gcc")
	if got, want := b.Lines(), []string{"\tif x {", "", "\t\ty()", "\t}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("uncommented: %q, want %q", got, want)
	}
}

func TestComment_CommentsAll_WhenOnlySomeLinesAreCommented(t *testing.T) {
	b := commentBuffer("// a", "b")
	typeKeys(b, "2gcc")
	if got, want := b.Lines(), []string{"// // a", "// b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComment_IsOneUndoStep(t *testing.T) {
	b := commentBuffer("a", "b")
	typeKeys(b, "2gccu")
	if got, want := b.Lines(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComment_Reports_WhenFileTypeHasNoComment(t *testing.T) {
	b := newBuffer("a")
	typeKeys(b, "gcc")
	if b.Message() == "" || !reflect.DeepEqual(b.Lines(), []string{"a"}) {
		t.Fatalf("message %q, lines %q", b.Message(), b.Lines())
	}
}

func TestComment_ShowsPending_WhenGcTyped(t *testing.T) {
	b := commentBuffer("a")
	typeKeys(b, "2gc")
	if b.Pending() != "2gc" {
		t.Fatalf("pending %q", b.Pending())
	}
}

func TestComment_TogglesCursorLine_WhenCommentCommand(t *testing.T) {
	b := commentBuffer("a", "b")
	typeKeys(b, ":comment<cr>")
	if got, want := b.Lines(), []string{"// a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
