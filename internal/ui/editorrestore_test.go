package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

// restoredProject opens a project the way jd does at start: with a session that has a cursor for every file, before the window size is known.
func restoredProject(t *testing.T, files map[string]string, session func(root string) project.Session) ProjectModel {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	model, err := NewProjectModel(root, "", session(root), ProjectHooks{})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return updated.(ProjectModel)
}

func longLines(count, width int) string {
	var text strings.Builder
	for i := 0; i < count; i++ {
		text.WriteString(strings.Repeat("x", width) + "\n")
	}
	return text.String()
}

func TestRestore_ShowsTheFileFromColumnZero_WhenTheCursorFitsTheWidth(t *testing.T) {
	model := restoredProject(t, map[string]string{"a.txt": longLines(10, 100), "b.txt": longLines(10, 100)}, func(root string) project.Session {
		return project.Session{Files: []string{"a.txt", "b.txt"}, Active: "a.txt", Cursors: map[string]project.Cursor{
			"a.txt": {Row: 3, Col: 40}, "b.txt": {Row: 5, Col: 60},
		}}
	})
	if row, col := model.Editor().Cursor(); row != 3 || col != 40 || model.Editor().left != 0 {
		t.Fatalf("shown file: cursor %d,%d offset %d: the cursor is kept, the view starts at column 0", row, col, model.Editor().left)
	}
	model.selectTab(1 - model.tab)
	if row, col := model.Editor().Cursor(); row != 5 || col != 60 || model.Editor().left != 0 {
		t.Fatalf("second tab: cursor %d,%d offset %d", row, col, model.Editor().left)
	}
}

func TestRestore_ScrollsOnlyAsFarAsTheCursorNeeds(t *testing.T) {
	model := restoredProject(t, map[string]string{"a.txt": longLines(100, 200)}, func(root string) project.Session {
		return project.Session{Files: []string{"a.txt"}, Active: "a.txt", Cursors: map[string]project.Cursor{"a.txt": {Row: 80, Col: 150}}}
	})
	e := model.Editor()
	if e.left != 150-e.textWidth()+1 {
		t.Errorf("a cursor beyond the width gets the smallest scroll that shows it: offset %d, text width %d", e.left, e.textWidth())
	}
	body := e.textHeight()
	margin := min(editorScrollOff, (body-1)/2)
	if e.top != 80-(body-1-margin) {
		t.Errorf("the cursor line is near the bottom of the view, not on its first line: top %d, body %d", e.top, body)
	}
}

func TestAt_ScrollsAtOnce_WhenTheEditorHasASize(t *testing.T) {
	model, fx := newProjectFixture(t, 120, sampleFiles)
	path := filepath.Join(fx.root, "long.txt")
	if err := os.WriteFile(path, []byte(longLines(100, 300)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := model.openFile(path, false); err != nil {
		t.Fatal(err)
	}
	e := model.Editor().At(90, 250)
	if e.fit || e.top == 0 || e.left == 0 {
		t.Fatalf("an editor with a size scrolls to the cursor at once: fit %v, top %d, left %d", e.fit, e.top, e.left)
	}
}
