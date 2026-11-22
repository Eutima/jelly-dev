package ui

import (
	"path/filepath"
	"strings"
	"testing"
)

// focusFixture is a project window with a file open in the editor.
func focusFixture(t *testing.T) ProjectModel {
	t.Helper()
	model, fx := newProjectFixture(t, 120, sampleFiles)
	if err := model.openFile(filepath.Join(fx.root, "src", "main.go"), false); err != nil {
		t.Fatal(err)
	}
	return model
}

func TestFocus_MarksTheTitleOfTheFocusedPaneOnly(t *testing.T) {
	model := focusFixture(t)
	titleLine := func(focus int) (line string, treePart, editorPart string) {
		model.focus = focus
		line = strings.Split(stripANSI(model.View()), "\n")[1] // under the top bar: the tree title and the tab bar side by side
		tree := model.treeWidth()
		runes := []rune(line)
		return line, string(runes[:tree]), string(runes[tree+1:]) // the column between them is the divider
	}
	line, tree, editor := titleLine(focusTree)
	treeFocusWidth := len([]rune(line))
	if !strings.HasPrefix(tree, "> "+filepath.Base(model.root)) || strings.Contains(editor, ">") {
		t.Errorf("tree focus: the marker belongs to the tree title only: tree %q editor %q", tree, editor)
	}
	line, tree, editor = titleLine(focusProjectEdit)
	if strings.Contains(tree, ">") || !strings.HasPrefix(editor, ">") {
		t.Errorf("editor focus: the marker belongs to the tab bar only: tree %q editor %q", tree, editor)
	}
	if len([]rune(line)) != treeFocusWidth {
		t.Errorf("the title line must not change width when the focus moves: %d and %d", treeFocusWidth, len([]rune(line)))
	}
	_, tree, editor = titleLine(focusTerminal)
	if strings.Contains(tree, ">") || strings.Contains(editor, ">") {
		t.Errorf("terminal focus: neither the tree title nor the tab bar is marked: tree %q editor %q", tree, editor)
	}
}

func TestFocus_TerminalTitleCarriesTheMarkerWhenFocused(t *testing.T) {
	model := focusFixture(t)
	model.focus = focusTerminal
	focused := stripANSI(model.termHeader().render(60, model.theme.Footer))
	model.focus = focusTree
	other := stripANSI(model.termHeader().render(60, model.theme.Footer))
	if !strings.HasPrefix(focused, "> TERMINAL") || !strings.HasPrefix(other, "  TERMINAL") {
		t.Fatalf("focused %q, other %q", focused, other)
	}
}

func TestFocus_FooterNamesThePane(t *testing.T) {
	model := focusFixture(t)
	for focus, want := range map[int]string{focusTree: "[tree]", focusProjectEdit: "[editor]", focusTerminal: "[terminal]"} {
		model.focus = focus
		if view := stripANSI(model.View()); !strings.Contains(view, want) {
			t.Errorf("focus %d: the footer lacks %s:\n%s", focus, want, view)
		}
	}
}

func TestFocus_DividerPointsAtTheFocusedPane(t *testing.T) {
	model := focusFixture(t)
	for focus, want := range map[int]string{focusTree: "\u258c", focusProjectEdit: "\u2590", focusTerminal: "│"} {
		model.focus = focus
		if got, _ := model.dividerLook(); got != want {
			t.Errorf("focus %d: divider %q, want %q", focus, got, want)
		}
		if view := stripANSI(model.View()); !strings.Contains(view, want) {
			t.Errorf("focus %d: the screen lacks the divider %q", focus, want)
		}
	}
}

func TestDim_KeepsTheTextAndSurvivesResets(t *testing.T) {
	line := "\x1b[31mred\x1b[0m plain \x1b[1mbold\x1b[m end"
	got := dim(line)
	if stripANSI(got) != stripANSI(line) {
		t.Fatalf("the text must not change: %q", stripANSI(got))
	}
	if strings.Count(got, "\x1b[2m") != 3 || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("faint must be switched on again after every reset: %q", got)
	}
	if dim("") != "" {
		t.Fatal("an empty line stays empty")
	}
}

func TestFocus_OtherPanesAreFaint(t *testing.T) {
	model := focusFixture(t)
	view := model.editor.View()
	model.focus = focusProjectEdit
	if model.dimEditor(view) != view {
		t.Error("the focused editor is not dimmed")
	}
	model.focus = focusTree
	dimmed := model.dimEditor(view)
	if dimmed == view || stripANSI(dimmed) != stripANSI(view) || !strings.Contains(dimmed, "\x1b[2m") {
		t.Error("an editor without the focus is drawn faint, with the same text")
	}
	if first := strings.SplitN(dimmed, "\n", 2)[0]; first != strings.SplitN(view, "\n", 2)[0] {
		t.Error("the title line stays as it is")
	}
	treeFocused, editorFocused := model.treeView(30), ""
	model.focus = focusProjectEdit
	editorFocused = model.treeView(30)
	if strings.Contains(treeFocused, "\x1b[2m") || !strings.Contains(editorFocused, "\x1b[2m") {
		t.Error("the tree rows are faint exactly when the tree has no focus")
	}
}

func TestFocus_CursorIsDrawnOnlyInTheFocusedEditor(t *testing.T) {
	model := focusFixture(t)
	model.editor = model.editor.WithTheme(trueColorTheme(t, "dark")) // a theme with colours: the cursor is a style
	model.editor.blurred = false
	focused := model.editor.View()
	model.editor.blurred = true
	blurred := model.editor.View()
	if focused == blurred || stripANSI(focused) != stripANSI(blurred) {
		t.Error("only the cursor (its style) differs between a focused and an unfocused editor")
	}
}
