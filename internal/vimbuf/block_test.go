package vimbuf

import (
	"reflect"
	"testing"
)

func TestVisualBlock_SelectsRectangle_WhenCtrlV(t *testing.T) {
	b := newBuffer("abcd", "efgh", "ijkl")
	typeKeys(b, "l<c-v>jl")
	if b.Mode() != VisualBlock || b.Mode().String() != "-- VISUAL BLOCK --" {
		t.Fatalf("mode %v", b.Mode())
	}
	for row, want := range [][2]int{{1, 3}, {1, 3}} {
		if from, to, ok := b.Selected(row); !ok || from != want[0] || to != want[1] {
			t.Fatalf("row %d: %d %d %v", row, from, to, ok)
		}
	}
	if _, _, ok := b.Selected(2); ok {
		t.Fatal("row 2 must not be selected")
	}
	typeKeys(b, "<c-v>")
	if b.Mode() != Normal {
		t.Fatalf("ctrl+v again leaves the mode, got %v", b.Mode())
	}
}

func TestVisualBlock_DeletesColumns_WhenD(t *testing.T) {
	b := newBuffer("abcd", "efgh", "ijkl")
	typeKeys(b, "l<c-v>jjld")
	if got, want := b.Lines(), []string{"ad", "eh", "il"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	typeKeys(b, "u")
	if got, want := b.Lines(), []string{"abcd", "efgh", "ijkl"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("undo: %q, want %q", got, want)
	}
}

func TestVisualBlock_RepeatsText_WhenI(t *testing.T) {
	b := newBuffer("abc", "def", "gh")
	typeKeys(b, "l<c-v>jjIX<esc>")
	if got, want := b.Lines(), []string{"aXbc", "dXef", "gXh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	typeKeys(b, "u")
	if got, want := b.Lines(), []string{"abc", "def", "gh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("undo: %q, want %q", got, want)
	}
}

func TestVisualBlock_AppendsAfterBlock_WhenA(t *testing.T) {
	b := newBuffer("abc", "d", "ghi")
	typeKeys(b, "<c-v>jjAX<esc>")
	if got, want := b.Lines(), []string{"aXbc", "dX", "gXhi"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVisualBlock_ReplacesColumns_WhenC(t *testing.T) {
	b := newBuffer("abc", "def")
	typeKeys(b, "l<c-v>jcX<esc>")
	if got, want := b.Lines(), []string{"aXc", "dXf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVisualBlock_YanksRows_WhenY(t *testing.T) {
	b := newBuffer("abc", "def")
	typeKeys(b, "<c-v>jly")
	if b.Mode() != Normal {
		t.Fatalf("mode %v", b.Mode())
	}
	if want := []string{"ab", "de"}; !reflect.DeepEqual(b.register, want) {
		t.Fatalf("register %q, want %q", b.register, want)
	}
}

func TestVisualBlock_SwapsCorners_WhenO(t *testing.T) {
	b := newBuffer("abc", "def")
	typeKeys(b, "<c-v>jlO")
	if row, col := b.Cursor(); row != 1 || col != 0 {
		t.Fatalf("cursor %d,%d after O, want 1,0", row, col)
	}
}

func TestVisualBlock_CommentsRows_WhenGc(t *testing.T) {
	b := commentBuffer("a", "b", "c")
	typeKeys(b, "<c-v>jgc")
	if got, want := b.Lines(), []string{"// a", "// b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
