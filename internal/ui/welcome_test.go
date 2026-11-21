package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/project"
)

func welcomeKeys(model tea.Model, keys ...string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, key := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+w":
			msg = tea.KeyMsg{Type: tea.KeyCtrlW}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		}
		model, cmd = model.Update(msg)
	}
	return model, cmd
}

func welcomeFixture(t *testing.T) (tea.Model, *WelcomeOutcome, *[]string, *[]string, []project.Entry) {
	t.Helper()
	base, _ := filepath.EvalSymlinks(t.TempDir())
	var entries []project.Entry
	for _, name := range []string{"alpha", "beta"} {
		path := filepath.Join(base, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, project.Entry{Path: path, LastOpened: time.Now().Add(-2 * time.Hour)})
	}
	var windows, removed []string
	model, outcome := NewWelcomeModel(entries, func(dir string) error { windows = append(windows, dir); return nil },
		func(path string) { removed = append(removed, path) })
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 14})
	return updated, outcome, &windows, &removed, entries
}

func TestWelcome_ListsProjectsAndOpensSelectedOne(t *testing.T) {
	model, outcome, _, _, entries := welcomeFixture(t)
	view := stripANSI(model.View())
	for _, want := range []string{"Recent projects", "alpha", "beta", "2 h ago", "[enter] open here", "[w] new window"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	_, cmd := welcomeKeys(model, "j", "enter")
	if outcome.Path != entries[1].Path || !isQuit(cmd) {
		t.Fatalf("outcome %q", outcome.Path)
	}
}

func TestWelcome_OpensInNewWindowAndStaysOpen(t *testing.T) {
	model, outcome, windows, _, entries := welcomeFixture(t)
	model, cmd := welcomeKeys(model, "w")
	if len(*windows) != 1 || (*windows)[0] != entries[0].Path || isQuit(cmd) || outcome.Path != "" {
		t.Fatalf("windows %v outcome %q", *windows, outcome.Path)
	}
	if !strings.Contains(stripANSI(model.View()), "new window") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestWelcome_ForgetsProjectButKeepsFolder(t *testing.T) {
	model, _, _, removed, entries := welcomeFixture(t)
	model, _ = welcomeKeys(model, "d")
	if len(*removed) != 1 || (*removed)[0] != entries[0].Path || strings.Contains(stripANSI(model.View()), "alpha") {
		t.Fatalf("removed %v\n%s", *removed, model.View())
	}
	if _, err := os.Stat(entries[0].Path); err != nil {
		t.Fatal("the folder must stay")
	}
}

func TestWelcome_OpensTypedFolder(t *testing.T) {
	model, outcome, windows, _, _ := welcomeFixture(t)
	target, _ := filepath.EvalSymlinks(t.TempDir())
	typed := append([]string{"o"}, strings.Split(target, "")...)
	model, _ = welcomeKeys(model, typed...)
	if !strings.Contains(stripANSI(model.View()), "Open folder: "+target) {
		t.Fatalf("prompt must show the path:\n%s", model.View())
	}
	model, _ = welcomeKeys(model, "ctrl+w") // in a new window
	if len(*windows) != 1 || (*windows)[0] != target {
		t.Fatalf("windows %v", *windows)
	}
	model, cmd := welcomeKeys(model, "o")
	model, cmd = welcomeKeys(model, append(strings.Split(target, ""), "enter")...)
	if outcome.Path != target || !isQuit(cmd) {
		t.Fatalf("outcome %q", outcome.Path)
	}
}

func TestWelcome_RefusesMissingFolder(t *testing.T) {
	model, outcome, _, _, _ := welcomeFixture(t)
	model, _ = welcomeKeys(model, "o", "/", "n", "o", "p", "e", "enter")
	if outcome.Path != "" || !strings.Contains(stripANSI(model.View()), "E344") {
		t.Fatalf("outcome %q\n%s", outcome.Path, model.View())
	}
}

func TestWelcome_ShowsHintWithoutProjectsAndFitsScreen(t *testing.T) {
	model, _ := NewWelcomeModel(nil, nil, nil)
	for _, size := range [][2]int{{30, 8}, {90, 14}} {
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(updated.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("size %v: %d lines", size, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("size %v: line %d is %d wide", size, i, lipgloss.Width(line))
			}
		}
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 14})
	if !strings.Contains(stripANSI(updated.View()), "No projects yet") {
		t.Fatalf("view:\n%s", updated.View())
	}
	if _, cmd := welcomeKeys(updated, "enter"); isQuit(cmd) {
		t.Fatal("enter without projects must not quit")
	}
}

func TestWelcome_ClickOpensProject(t *testing.T) {
	model, outcome, _, _, entries := welcomeFixture(t)
	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: 4})
	if outcome.Path != entries[1].Path || !isQuit(cmd) {
		t.Fatalf("outcome %q (%v)", outcome.Path, updated.View())
	}
}

func folderTree(t *testing.T, names ...string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, name := range names {
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFolderMatches_OffersOnlyMatchingFolders(t *testing.T) {
	root := folderTree(t, "alpha/", "alpine/", "beta/", ".hidden/", "file.txt")
	tests := []struct {
		input string
		want  []string
	}{
		{root + "/al", []string{root + "/alpha/", root + "/alpine/"}},
		{root + "/", []string{root + "/alpha/", root + "/alpine/", root + "/beta/"}},
		{root + "/.", []string{root + "/.hidden/"}},
		{root + "/BE", []string{root + "/beta/"}}, // case-insensitive when nothing matches exactly
		{root + "/file", nil},
		{root + "/nothing", nil},
	}
	for _, tt := range tests {
		got := folderMatches(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("%q: got %q, want %q", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%q: got %q, want %q", tt.input, got, tt.want)
			}
		}
	}
}

func TestFolderMatches_KeepsHomeShortcut(t *testing.T) {
	home := folderTree(t, "projects/", "photos/")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got := folderMatches("~/pro")
	if len(got) != 1 || got[0] != "~/projects/" {
		t.Fatalf("got %q", got)
	}
	if got := folderMatches("~"); len(got) != 2 || got[0] != "~/photos/" {
		t.Fatalf("a lone ~ lists the home folder: %q", got)
	}
}

func typeText(model tea.Model, text string) tea.Model {
	for _, r := range text {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

func pressKey(model tea.Model, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	return model.Update(tea.KeyMsg{Type: keyType})
}

func TestWelcome_CompletesFolderWithTab(t *testing.T) {
	root := folderTree(t, "alpha/sub/", "alpine/", "beta/")
	model, outcome, _, _, _ := welcomeFixture(t)
	model, _ = welcomeKeys(model, "o")
	model = typeText(model, root+"/b")
	model, _ = pressKey(model, tea.KeyTab) // a single match completes the whole folder
	if got := stripANSI(model.View()); !strings.Contains(got, "Open folder: "+root+"/beta/█") {
		t.Fatalf("view:\n%s", got)
	}
	model, _ = welcomeKeys(model, "esc")

	model, _ = welcomeKeys(model, "o")
	model = typeText(model, root+"/a")
	model, _ = pressKey(model, tea.KeyTab) // several matches complete the common part: both start with "alp"
	if got := stripANSI(model.View()); !strings.Contains(got, "Open folder: "+root+"/alp█") || !strings.Contains(got, "alpha/") || !strings.Contains(got, "alpine/") {
		t.Fatalf("view:\n%s", got)
	}
	model, _ = pressKey(model, tea.KeyTab) // the next presses walk through the matches
	if got := stripANSI(model.View()); !strings.Contains(got, "Open folder: "+root+"/alpha/█") {
		t.Fatalf("first match expected:\n%s", got)
	}
	model, _ = pressKey(model, tea.KeyTab)
	if got := stripANSI(model.View()); !strings.Contains(got, "Open folder: "+root+"/alpine/█") {
		t.Fatalf("second match expected:\n%s", got)
	}
	model, _ = pressKey(model, tea.KeyShiftTab)
	model, _ = pressKey(model, tea.KeyShiftTab)
	if got := stripANSI(model.View()); !strings.Contains(got, "Open folder: "+root+"/alpine/█") {
		t.Fatalf("shift+tab walks backwards and wraps:\n%s", got)
	}
	model, cmd := pressKey(model, tea.KeyEnter)
	if outcome.Path != root+"/alpine" || !isQuit(cmd) {
		t.Fatalf("outcome %q", outcome.Path)
	}
}

func TestWelcome_ListsFoldersWhileTypingAndChoosesWithArrows(t *testing.T) {
	root := folderTree(t, "one/", "two/")
	model, outcome, _, _, _ := welcomeFixture(t)
	model, _ = welcomeKeys(model, "o")
	model = typeText(model, root+"/")
	view := stripANSI(model.View())
	if !strings.Contains(view, "Folders (2)") || !strings.Contains(view, "one/") || strings.Contains(view, "Recent projects") {
		t.Fatalf("matching folders must replace the project list:\n%s", view)
	}
	model, _ = pressKey(model, tea.KeyDown)
	model, _ = pressKey(model, tea.KeyDown)
	model, cmd := pressKey(model, tea.KeyEnter)
	if outcome.Path != root+"/two" || !isQuit(cmd) {
		t.Fatalf("outcome %q", outcome.Path)
	}
}

func TestWelcome_TypingAgainRefreshesMatches(t *testing.T) {
	root := folderTree(t, "alpha/", "beta/")
	model, _, _, _, _ := welcomeFixture(t)
	model, _ = welcomeKeys(model, "o")
	model = typeText(model, root+"/al")
	model, _ = pressKey(model, tea.KeyBackspace)
	model, _ = pressKey(model, tea.KeyBackspace)
	if view := stripANSI(model.View()); !strings.Contains(view, "beta/") || !strings.Contains(view, "alpha/") {
		t.Fatalf("matches must follow the typed text:\n%s", view)
	}
	model = typeText(model, "zzz")
	if view := stripANSI(model.View()); !strings.Contains(view, "No folder matches") {
		t.Fatalf("view:\n%s", view)
	}
	model, _ = pressKey(model, tea.KeyTab)
	if view := stripANSI(model.View()); !strings.Contains(view, "! No fold") {
		t.Fatalf("tab without matches must say so:\n%s", view)
	}
}
