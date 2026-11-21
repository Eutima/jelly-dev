package vimbuf

import (
	"reflect"
	"strings"
	"testing"
)

func at(t *testing.T, b *Buffer, row, col int) {
	t.Helper()
	if r, c := b.Cursor(); r != row || c != col {
		t.Fatalf("cursor %d,%d, want %d,%d (message %q)", r, c, row, col, b.Message())
	}
}

func TestSearch_ForwardNextAndPreviousWrapAround(t *testing.T) {
	b := newBuffer("foo bar", "baz foo", "none", "foo")
	typeKeys(b, "/foo<cr>")
	at(t, b, 1, 4)
	typeKeys(b, "n")
	at(t, b, 3, 0)
	typeKeys(b, "n")
	at(t, b, 0, 0)
	if !strings.Contains(b.Message(), "BOTTOM") {
		t.Fatalf("wrapping must be reported: %q", b.Message())
	}
	typeKeys(b, "N")
	at(t, b, 3, 0)
}

func TestSearch_BackwardAndRepeatWithEmptyPattern(t *testing.T) {
	b := newBuffer("ab", "ab", "ab")
	typeKeys(b, "G?b<cr>") // G puts the cursor on column 0: the b of that line is after it
	at(t, b, 1, 1)
	typeKeys(b, "?<cr>") // empty: the last pattern, backwards
	at(t, b, 0, 1)
	typeKeys(b, "n") // wraps to the end
	at(t, b, 2, 1)
	typeKeys(b, "N")
	at(t, b, 0, 1)
}

func TestSearch_OnlyMatchIsFoundAgainAndMissingPatternReports(t *testing.T) {
	b := newBuffer("one", "two", "three")
	typeKeys(b, "/two<cr>n")
	at(t, b, 1, 0)
	typeKeys(b, "/zzz<cr>")
	at(t, b, 1, 0)
	if !strings.HasPrefix(b.Message(), "E486") {
		t.Fatalf("message %q", b.Message())
	}
	c := newBuffer("x")
	typeKeys(c, "n")
	if !strings.HasPrefix(c.Message(), "E35") {
		t.Fatalf("message %q", c.Message())
	}
}

func TestSearch_RegexLiteralFallbackAndCaseFlag(t *testing.T) {
	b := newBuffer("a.c abc", "A(")
	typeKeys(b, `/a.c<cr>`)
	at(t, b, 0, 4)           // a regular expression: "abc" matches after the cursor
	typeKeys(b, `/\ca(<cr>`) // invalid expression is searched literally, ignoring case
	at(t, b, 1, 0)
}

func TestSearch_EscapeCancelsAndHighlightsAreClearable(t *testing.T) {
	b := newBuffer("foo foo")
	typeKeys(b, "/fo<esc>")
	if b.Mode() != Normal || b.Matches(0) != nil {
		t.Fatal("esc must cancel the search without a highlight")
	}
	typeKeys(b, "/foo<cr>")
	if want := [][2]int{{0, 3}, {4, 7}}; !reflect.DeepEqual(b.Matches(0), want) {
		t.Fatalf("matches %v", b.Matches(0))
	}
	typeKeys(b, ":noh<cr>")
	if b.Matches(0) != nil {
		t.Fatal(":noh must clear the highlight")
	}
	typeKeys(b, "/")
	if b.CommandPrefix() != "/" || b.Mode() != Command {
		t.Fatal("the command line of a search starts with /")
	}
}

func TestSearch_CountsRunesNotBytes(t *testing.T) {
	b := newBuffer("ééx")
	typeKeys(b, "/x<cr>")
	at(t, b, 0, 2)
}
