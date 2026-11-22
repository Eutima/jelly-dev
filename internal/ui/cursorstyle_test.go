package ui

import "testing"

func TestCursorStyleIsVisibleWhenSelectionIsReverseVideo(t *testing.T) {
	for _, name := range []string{"mono", "ansi", "contrast"} {
		theme, ok := NamedTheme(name)
		if !ok {
			t.Fatalf("theme %q missing", name)
		}
		cursor := theme.CursorStyle()
		if !cursor.GetReverse() {
			t.Errorf("%s: the cursor cell is not reverse video, it would be invisible", name)
		}
		if !cursor.GetUnderline() || !cursor.GetBold() {
			t.Errorf("%s: the cursor cannot be told apart from the selection", name)
		}
	}
}

func TestCursorStyleInvertsTheColourSelection(t *testing.T) {
	theme, _ := NamedTheme("dark")
	if !theme.CursorStyle().GetReverse() {
		t.Error("dark: the cursor should be the inverted selection")
	}
}
