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

// switchFixture is a project window whose hooks record what the window asks for.
type switchFixture struct {
	model    ProjectModel
	root     string
	other    string
	third    string
	opened   []string
	sessions map[string]project.Session
	forgot   []string
	windows  []string
}

func newSwitchFixture(t *testing.T) *switchFixture {
	t.Helper()
	fx := &switchFixture{sessions: map[string]project.Session{}}
	model, base := newProjectFixture(t, 100, sampleFiles)
	fx.root = base.root
	mk := func(name string, files map[string]string) string {
		dir, _ := filepath.EvalSymlinks(t.TempDir())
		for file, text := range files {
			path := filepath.Join(dir, file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	fx.other = mk("other", map[string]string{"app.py": "print('hi')\n", "lib/mod.py": "x = 1\n"})
	fx.third = mk("third", map[string]string{"index.js": "let a\n"})
	model.hooks = ProjectHooks{
		Recent: func() []project.Entry {
			return []project.Entry{
				{Path: fx.root, LastOpened: time.Now()}, {Path: fx.other, LastOpened: time.Now().Add(-time.Hour)},
				{Path: fx.third, LastOpened: time.Now().Add(-48 * time.Hour)},
			}
		},
		Forget:      func(path string) { fx.forgot = append(fx.forgot, path) },
		NewWindow:   func(dir string) error { fx.windows = append(fx.windows, dir); return nil },
		SaveSession: func(root string, s project.Session) { fx.sessions[root] = s },
		Open: func(root string) project.Session {
			fx.opened = append(fx.opened, root)
			return fx.sessions[root]
		},
	}
	fx.model = model
	return fx
}

// inEditor gives the editor the focus, where ":" commands are typed.
func inEditor(model ProjectModel) ProjectModel {
	model, _ = projectKeys(model, "ctrl+e")
	return model
}

func TestProject_SwitchesToOtherFolderWithProjectCommand(t *testing.T) {
	fx := newSwitchFixture(t)
	model, _ := projectKeys(fx.model, "G", "enter") // README.md of the first project is open
	model, _ = typeCommand(model, "project "+fx.other)
	if model.Root() != fx.other || len(fx.opened) != 1 || fx.opened[0] != fx.other {
		t.Fatalf("root %q opened %v", model.Root(), fx.opened)
	}
	if session := fx.sessions[fx.root]; session.Active != "README.md" {
		t.Fatalf("the old project's session must be stored first: %+v", session)
	}
	view := stripANSI(model.View())
	if !strings.Contains(view, "app.py") || strings.Contains(view, "README.md") || !strings.Contains(view, "Opened /tmp") {
		t.Fatalf("the window must show the new project:\n%s", view)
	}
}

func TestProject_ProjectCommandResolvesRelativeAndHomePaths(t *testing.T) {
	fx := newSwitchFixture(t)
	rel, _ := filepath.Rel(fx.root, fx.other)
	model, _ := typeCommand(inEditor(fx.model), "project "+rel)
	if model.Root() != fx.other {
		t.Fatalf("a path relative to the project must work: %q", model.Root())
	}
	t.Setenv("HOME", fx.third)
	t.Setenv("USERPROFILE", fx.third)
	model, _ = typeCommand(inEditor(fx.model), "project ~")
	if model.Root() != fx.third {
		t.Fatalf("~ must be the home folder: %q", model.Root())
	}
}

func TestProject_RestoresTheSessionOfTheProjectSwitchedTo(t *testing.T) {
	fx := newSwitchFixture(t)
	fx.sessions[fx.other] = project.Session{Active: "lib/mod.py", Cursors: map[string]project.Cursor{"lib/mod.py": {Row: 0, Col: 2}}}
	model, _ := typeCommand(inEditor(fx.model), "project "+fx.other)
	if filepath.Base(model.Editor().Path()) != "mod.py" || model.focus != focusProjectEdit {
		t.Fatalf("path %q focus %d", model.Editor().Path(), model.focus)
	}
	if _, col := model.Editor().Cursor(); col != 2 {
		t.Fatalf("cursor column %d", col)
	}
}

func TestProject_RefusesSwitchWithUnsavedChangesUnlessForced(t *testing.T) {
	fx := newSwitchFixture(t)
	model, _ := projectKeys(fx.model, "G", "enter", "x") // modify README.md
	model, _ = typeCommand(model, "project "+fx.other)
	if model.Root() != fx.root || model.dlg == nil || len(fx.opened) != 0 {
		t.Fatalf("unsaved changes must ask before the switch:\n%s", model.View())
	}
	if model, _ = projectKeys(model, "esc"); model.Root() != fx.root || model.dlg != nil || !model.Editor().Modified() {
		t.Fatal("cancel must stay in the project with the changes")
	}
	model, _ = typeCommand(model, "project "+fx.other)
	if saved, _ := projectKeys(model, "d"); saved.Root() != fx.other {
		t.Fatalf("discard must switch: %q", saved.Root())
	}
	model, _ = projectKeys(model, "esc")
	model, _ = typeCommand(model, "project! "+fx.other)
	if model.Root() != fx.other || len(fx.opened) != 2 { // the discard above switched once already
		t.Fatalf("project! must switch anyway: %q", model.Root())
	}
}

func TestProject_ExplainsSwitchProblems(t *testing.T) {
	fx := newSwitchFixture(t)
	model, _ := typeCommand(inEditor(fx.model), "project /definitely/not/here")
	if model.Root() != fx.root || !strings.Contains(stripANSI(model.View()), "E344") {
		t.Fatalf("a missing folder must be refused:\n%s", model.View())
	}
	model, _ = typeCommand(inEditor(fx.model), "project "+fx.root)
	if model.Root() != fx.root || !strings.Contains(stripANSI(model.View()), "Already in this project") || len(fx.opened) != 0 {
		t.Fatalf("switching to the same project must say so:\n%s", model.View())
	}
}

func TestProject_ListsRecentProjectsAndSwitchesToChosenOne(t *testing.T) {
	fx := newSwitchFixture(t)
	for name, open := range map[string]func(ProjectModel) ProjectModel{
		"command ":      func(m ProjectModel) ProjectModel { m, _ = typeCommand(inEditor(m), "project"); return m },
		"projects cmd":  func(m ProjectModel) ProjectModel { m, _ = typeCommand(inEditor(m), "projects"); return m },
		"recent cmd":    func(m ProjectModel) ProjectModel { m, _ = typeCommand(inEditor(m), "recent"); return m },
		"P in the tree": func(m ProjectModel) ProjectModel { m, _ = projectKeys(m, "P"); return m },
	} {
		model := open(fx.model)
		if model.picker == nil {
			t.Fatalf("%s must open the project list", name)
		}
		view := stripANSI(model.View())
		for _, want := range []string{"Recent projects", filepath.Base(fx.other), filepath.Base(fx.third), "[enter] new window"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%s: list lacks %q:\n%s", name, want, view)
			}
		}
	}
	model, _ := projectKeys(fx.model, "P")
	model, _ = projectKeys(model, "j", "enter") // the second entry
	if model.picker != nil || model.Root() != fx.root || len(fx.windows) != 1 || fx.windows[0] != fx.other {
		t.Fatalf("enter must open the chosen project in a new window: root %q windows %v picker %v", model.Root(), fx.windows, model.picker)
	}
}

func TestProject_PickerEscapeClosesAndKeepsProject(t *testing.T) {
	fx := newSwitchFixture(t)
	model, _ := projectKeys(fx.model, "P")
	model, quit := projectKeys(model, "esc")
	if model.picker != nil || model.Root() != fx.root || quit != nil {
		t.Fatalf("esc must close the list only (picker %v, quit %v)", model.picker, quit != nil)
	}
	model, _ = projectKeys(model, "P")
	model, quit = projectKeys(model, "ctrl+e") // not a picker key: ignored
	_ = quit
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlD})
	if model.picker != nil {
		t.Fatal("ctrl+d must close the list like ctrl+c")
	}
	model, _ = projectKeys(model, "P", "enter") // the current project is first: nothing changes
	if model.Root() != fx.root || len(fx.opened) != 0 {
		t.Fatalf("choosing the current project changes nothing: %q %v", model.Root(), fx.opened)
	}
}

func TestProject_PickerOpensNewWindowForgetsAndTypesFolder(t *testing.T) {
	fx := newSwitchFixture(t)
	model, _ := projectKeys(fx.model, "P", "j", "w")
	if len(fx.windows) != 1 || fx.windows[0] != fx.other || model.picker == nil || model.Root() != fx.root {
		t.Fatalf("w must open the project in a new window and keep the list: %v", fx.windows)
	}
	model, _ = projectKeys(model, "d")
	if len(fx.forgot) != 1 || fx.forgot[0] != fx.other {
		t.Fatalf("d must forget the project: %v", fx.forgot)
	}
	model, _ = projectKeys(model, "o")
	for _, r := range fx.third {
		model, _ = projectKeys(model, string(r))
	}
	model, _ = projectKeys(model, "enter")
	if model.Root() != fx.root || model.picker != nil || len(fx.windows) != 2 || fx.windows[1] != fx.third {
		t.Fatalf("a typed folder must open in a new window: %q %v", model.Root(), fx.windows)
	}
}

func TestProject_PickerWithoutRecentHookShowsHintAndFitsScreen(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles) // no hooks at all
	model, _ = projectKeys(model, "P")
	if !strings.Contains(stripANSI(model.View()), "No projects yet") {
		t.Fatalf("view:\n%s", model.View())
	}
	for _, size := range [][2]int{{30, 8}, {100, 14}} {
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
}
