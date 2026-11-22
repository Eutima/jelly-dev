package vimbuf

import (
	"reflect"
	"testing"
)

func modelessBuffer(lines ...string) *Buffer {
	b := New(lines, nil)
	b.EnterModeless()
	return b
}

func text(b *Buffer) []string { return b.Lines() }

func TestModeless_TypingIsOneUndoStep_PerRun(t *testing.T) {
	b := modelessBuffer("")
	b.TypeText("hello")
	b.Move("left")
	b.TypeText("X")
	if got := text(b); !reflect.DeepEqual(got, []string{"hellXo"}) {
		t.Fatalf("text %q", got)
	}
	b.Undo() // the run after the cursor move
	if got := text(b); !reflect.DeepEqual(got, []string{"hello"}) {
		t.Fatalf("after one undo %q", got)
	}
	b.Undo() // the first run
	if got := text(b); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("after two undos %q", got)
	}
	b.Redo()
	if got := text(b); !reflect.DeepEqual(got, []string{"hello"}) {
		t.Fatalf("redo %q", got)
	}
	if !b.Modified() || b.Mode() != Insert {
		t.Fatalf("modified %v mode %v: a modeless buffer stays in insert mode", b.Modified(), b.Mode())
	}
}

func TestModeless_NewlineBackspaceDeleteAndEdges(t *testing.T) {
	b := modelessBuffer("ab")
	b.Move("end")
	b.Newline()
	b.TypeText("cd")
	if got := text(b); !reflect.DeepEqual(got, []string{"ab", "cd"}) {
		t.Fatalf("text %q", got)
	}
	b.Move("home")
	b.Backspace() // joins the line to the one above
	if got := text(b); !reflect.DeepEqual(got, []string{"abcd"}) {
		t.Fatalf("backspace at the start of a line joins: %q", got)
	}
	b.DeleteForward()
	if got := text(b); !reflect.DeepEqual(got, []string{"abd"}) {
		t.Fatalf("delete %q", got)
	}
	b.Move("ctrl+home")
	b.Backspace() // nothing before the first character
	b.Move("ctrl+end")
	b.DeleteForward() // nothing behind the last
	if got := text(b); !reflect.DeepEqual(got, []string{"abd"}) {
		t.Fatalf("the edges change nothing: %q", got)
	}
}

func TestModeless_MovesWrapAndJumpByWordAndPage(t *testing.T) {
	b := modelessBuffer("one two", "three")
	b.SetPage(5)
	b.Move("end")
	b.Move("right")
	if row, col := b.Cursor(); row != 1 || col != 0 {
		t.Fatalf("right at the end of a line goes to the next: %d,%d", row, col)
	}
	b.Move("left")
	if row, col := b.Cursor(); row != 0 || col != 7 {
		t.Fatalf("left at the start of a line goes to the end of the one above: %d,%d", row, col)
	}
	b.Move("ctrl+home")
	b.Move("ctrl+right")
	if _, col := b.Cursor(); col != 4 {
		t.Fatalf("a word to the right: column %d", col)
	}
	b.Move("ctrl+left")
	if _, col := b.Cursor(); col != 0 {
		t.Fatalf("a word to the left: column %d", col)
	}
	b.Move("pgdown")
	if row, _ := b.Cursor(); row != 1 {
		t.Fatalf("page down stops at the last line: %d", row)
	}
}

func TestModeless_CutLineAndUncut_AddUpConsecutiveCuts(t *testing.T) {
	b := modelessBuffer("a", "b", "c", "d")
	b.CutLine()
	b.CutLine()
	if got := text(b); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Fatalf("two cuts: %q", got)
	}
	b.Move("down")
	b.Uncut() // both lines come back above the cursor line
	if got := text(b); !reflect.DeepEqual(got, []string{"c", "a", "b", "d"}) {
		t.Fatalf("uncut %q", got)
	}
	b.Undo()
	if got := text(b); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Fatalf("one undo takes the uncut back: %q", got)
	}
	b.Move("up")
	b.CutLine() // a new series: only this line
	b.Move("down")
	b.CutLine()
	if got := text(b); !reflect.DeepEqual(got, []string{""}) && len(got) != 0 {
		t.Fatalf("text %q", got)
	}
}

func TestModeless_MarkCutCopyAndUncut(t *testing.T) {
	b := modelessBuffer("hello world", "second")
	b.SetMark()
	b.Move("ctrl+right")
	if from, to, ok := b.Selected(0); !ok || from != 0 || to != 6 {
		t.Fatalf("the marked text is highlighted: %d %d %v", from, to, ok)
	}
	if !b.CopyMarked() || b.HasMark() {
		t.Fatal("copy removes the mark")
	}
	if got := text(b); !reflect.DeepEqual(got, []string{"hello world", "second"}) {
		t.Fatalf("copy leaves the text: %q", got)
	}
	b.Move("ctrl+end")
	b.Uncut()
	if got := text(b); !reflect.DeepEqual(got, []string{"hello world", "secondhello "}) {
		t.Fatalf("uncut at the end: %q", got)
	}
	b.Move("ctrl+home")
	b.SetMark()
	b.Move("ctrl+right")
	if !b.CutMarked() {
		t.Fatal("something was marked")
	}
	if got := text(b); !reflect.DeepEqual(got, []string{"world", "secondhello "}) {
		t.Fatalf("cut: %q", got)
	}
	if b.CutMarked() {
		t.Fatal("nothing is marked any more")
	}
}

func TestModeless_FindIsPlainTextAndCaseInsensitive_AndReplace(t *testing.T) {
	b := modelessBuffer("foo.bar", "Foo bar foo")
	if !b.Find("foo", false, false) {
		t.Fatal("foo is there")
	}
	if row, col := b.Cursor(); row != 1 || col != 0 {
		t.Fatalf("the next match after the cursor, any case: %d,%d", row, col)
	}
	if !b.Find("o.b", false, true) {
		t.Fatal("a dot is a dot, not a pattern")
	}
	if b.Find("zzz", false, false) {
		t.Fatal("zzz is not there")
	}
	if n := func() int { b.Find("foo", false, false); return b.ReplaceAll("X") }(); n != 3 {
		t.Fatalf("replaced %d", n)
	}
	if got := text(b); !reflect.DeepEqual(got, []string{"X.bar", "X bar X"}) {
		t.Fatalf("text %q", got)
	}
	b.Undo() // all replacements are one step
	if got := text(b); !reflect.DeepEqual(got, []string{"foo.bar", "Foo bar foo"}) {
		t.Fatalf("one undo: %q", got)
	}
}

func TestModeless_ReplaceHere_ReplacesOnlyAtTheCursor(t *testing.T) {
	b := modelessBuffer("aXa X")
	b.Move("ctrl+home")
	b.Find("x", false, false) // first match after the cursor: column 1
	if !b.ReplaceHere("--") {
		t.Fatal("the cursor is on a match")
	}
	if got := text(b); !reflect.DeepEqual(got, []string{"a--a X"}) {
		t.Fatalf("text %q", got)
	}
	if b.ReplaceHere("!") {
		t.Fatal("the cursor is no longer on a match")
	}
}

func TestModeless_ReplaceLinesKeepsTheMode_AndPasteIsTyping(t *testing.T) {
	b := modelessBuffer("x")
	b.ReplaceLines([]string{"formatted"})
	if b.Mode() != Insert {
		t.Fatal("formatting must not leave insert mode")
	}
	b.PasteText("ab\ncd")
	if got := text(b); len(got) != 2 {
		t.Fatalf("pasted text is typed: %q", got)
	}
	b.ExitModeless()
	if b.Mode() != Normal || b.Modeless() {
		t.Fatal("back to normal mode")
	}
}

func TestModeless_GotoPositionClamps(t *testing.T) {
	b := modelessBuffer("abc", "de")
	b.GotoPosition(2, 9)
	if row, col := b.Cursor(); row != 1 || col != 2 {
		t.Fatalf("%d,%d", row, col)
	}
	b.GotoPosition(99, 1)
	if row, _ := b.Cursor(); row != 1 {
		t.Fatalf("row %d", row)
	}
}

func TestModeless_FindHere_AcceptsAMatchAtTheCursor(t *testing.T) {
	b := modelessBuffer("foo bar", "x foo")
	if !b.FindHere("foo", false) {
		t.Fatal("foo is there")
	}
	if row, col := b.Cursor(); row != 0 || col != 0 {
		t.Fatalf("the match at the cursor counts: %d,%d", row, col)
	}
	b.Move("right")
	if !b.FindHere("foo", false) {
		t.Fatal("foo is there")
	}
	if row, col := b.Cursor(); row != 1 || col != 2 {
		t.Fatalf("else the next one: %d,%d", row, col)
	}
	if b.FindHere("zzz", false) || b.FindHere("", false) {
		t.Fatal("no match")
	}
}
