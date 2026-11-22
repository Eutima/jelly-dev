package ui

import "testing"

func TestTermSelection_TextCoversFromAnchorToEnd_InReadingOrder(t *testing.T) {
	cells := [][]string{{"a", "b", "c", " "}, {"d", "e", "f", " "}, {"g", "h", "i", " "}}
	sel := termSelection{anchorX: 1, anchorY: 0, curX: 1, curY: 2, moved: true}
	if got := sel.text(cells); got != "bc\ndef\ngh" {
		t.Fatalf("forward = %q", got)
	}
	back := termSelection{anchorX: 1, anchorY: 2, curX: 1, curY: 0, moved: true}
	if got := back.text(cells); got != "bc\ndef\ngh" {
		t.Fatalf("backward = %q", got)
	}
	if got := (termSelection{anchorX: 0, anchorY: 0, curX: 0, curY: 0}).text(cells); got != "" {
		t.Fatalf("a click selects nothing: %q", got)
	}
}
