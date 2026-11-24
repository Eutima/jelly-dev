package vimbuf

import (
	"strings"
	"testing"
)

func typed(b *Buffer, keys ...string) {
	for _, key := range keys {
		b.Handle(key)
	}
}

func TestInsert_TabTypesSpacesUpToTheNextLevelWhenTheFileUsesSpaces(t *testing.T) {
	b := New([]string{"ab"}, nil)
	b.SetIndent("  ")
	typed(b, "A", "tab", "tab")
	if got := b.Line(0); got != "ab    " { // 2 to column 4, then 2 more
		t.Fatalf("tab fills up to the next multiple of 2: %q", got)
	}
	tabs := New([]string{""}, nil)
	typed(tabs, "i", "tab")
	if tabs.Line(0) != "\t" {
		t.Fatalf("without SetIndent the tab stays a tab: %q", tabs.Line(0))
	}
}

func TestInsert_EnterKeepsTheIndentOfThePreviousLine(t *testing.T) {
	b := New([]string{"\t  foo() {"}, nil)
	typed(b, "A", "enter", "x")
	if got := b.Line(1); got != "\t  x" {
		t.Fatalf("the new line has the indent of the line above: %q", got)
	}
	typed(b, "enter", "enter")
	if b.Line(2) != "" {
		t.Fatalf("a line of only its indent is emptied by enter: %q", b.Line(2))
	}
	if b.Line(3) != "\t  " {
		t.Fatalf("the next line still keeps the indent: %q", b.Line(3))
	}
}

func TestNormal_OpenLineKeepsTheIndent(t *testing.T) {
	b := New([]string{"    a", "b"}, nil)
	typed(b, "o", "x")
	if b.Line(1) != "    x" {
		t.Fatalf("o: %q", b.Line(1))
	}
	typed(b, "esc", "k", "O", "y")
	if b.Line(0) != "    y" {
		t.Fatalf("O: %q", b.Lines())
	}
}

func TestPaste_KeepsTheTextAsItIs(t *testing.T) {
	b := New([]string{"  x"}, nil)
	b.SetIndent("  ")
	typed(b, "A")
	b.PasteText("\n\ta\n\t\tb")
	if got := strings.Join(b.Lines(), "|"); got != "  x|\ta|\t\tb" {
		t.Fatalf("pasted lines get no extra indent and keep their tabs: %q", got)
	}
}
