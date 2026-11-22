package ui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctrlT presses the key that opens the tab list everywhere (alt+t); the editor forwards it as a command, which is fed back like the
// runtime does.
func ctrlT(model ProjectModel) ProjectModel {
	return projectSend(model, keyMsg("alt+t"))
}

func typeInto(model ProjectModel, text string) ProjectModel {
	for _, r := range text {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(ProjectModel)
	}
	return model
}

// twoFileTabs opens README.md and src/main.go in two tabs; main.go is the shown one.
func twoFileTabs(t *testing.T) ProjectModel {
	t.Helper()
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	model, _ = projectKeys(model, "ctrl+e", "k", "enter", "j", "j", "enter")
	if len(model.openEditors()) != 2 || model.Editor().Path() == "" {
		t.Fatalf("setup: %d tabs", len(model.openEditors()))
	}
	return model
}

func TestTabSwitcher_ListsTheFilesAndNewTerminal_WithTheShownTabSelected(t *testing.T) {
	model := ctrlT(twoFileTabs(t))
	if model.tabs == nil {
		t.Fatal("ctrl+t must open the tab list")
	}
	view := stripANSI(model.View())
	for _, want := range []string{"Tabs (shift+tab)", "README.md", "main.go", "New terminal"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the list lacks %q:\n%s", want, view)
		}
	}
	if model.tabs.picker.Chosen() != model.tab {
		t.Fatalf("the shown tab %d must be selected, got %d", model.tab, model.tabs.picker.Chosen())
	}
}

func TestTabSwitcher_EnterShowsTheChosenFile(t *testing.T) {
	model := twoFileTabs(t)
	shown := model.Editor().Path()
	model = ctrlT(model)
	model = typeInto(model, "readme")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ProjectModel)
	if model.tabs != nil || model.Editor().Path() == shown || !strings.HasSuffix(model.Editor().Path(), "README.md") {
		t.Fatalf("enter must show README.md: %q", model.Editor().Path())
	}
	if model.focus != focusProjectEdit {
		t.Fatal("the editor gets the focus")
	}
}

func TestTabSwitcher_TypingNarrowsTheList(t *testing.T) {
	model := typeInto(ctrlT(twoFileTabs(t)), "main")
	picker := model.tabs.picker
	if len(picker.shown) != 1 || picker.items[picker.Chosen()].Label != "main.go" {
		t.Fatalf("only main.go matches: %d entries shown", len(picker.shown))
	}
}

func TestTabSwitcher_MarksFilesWithUnsavedChanges(t *testing.T) {
	model := twoFileTabs(t)
	model, _ = projectKeys(model, "x") // a change in the shown tab
	model = ctrlT(model)
	if view := stripANSI(model.View()); !strings.Contains(view, "[+]") {
		t.Fatalf("an unsaved file must be marked:\n%s", view)
	}
}

func TestTabSwitcher_CtrlTAndEscCloseIt(t *testing.T) {
	model := ctrlT(twoFileTabs(t))
	shown := model.Editor().Path()
	if model = ctrlT(model); model.tabs != nil {
		t.Fatal("a second ctrl+t closes the list")
	}
	model = ctrlT(model)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model = updated.(ProjectModel); model.tabs != nil || model.Editor().Path() != shown {
		t.Fatal("esc closes the list and changes nothing")
	}
}

func TestTabSwitcher_ShowsAndFocusesAShell_AndStartsANewOne(t *testing.T) {
	model := twoFileTabs(t)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	t.Cleanup(model.closeTerminal)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT}) // starts a shell
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	if len(model.terms) != 1 || model.focus != focusTerminal {
		t.Fatalf("setup: %d shells", len(model.terms))
	}
	model = ctrlT(model) // from inside the shell panel
	if model.tabs == nil || !strings.Contains(stripANSI(model.View()), "Terminal 1") {
		t.Fatalf("ctrl+t in the terminal must list the shells:\n%s", model.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = typeInto(updated.(ProjectModel), "")
	model.focus = focusProjectEdit // back in the editor, the panel still shown
	model = typeInto(ctrlT(model), "terminal 1")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model = updated.(ProjectModel); model.focus != focusTerminal || !model.termShown {
		t.Fatal("choosing a shell must show the panel and focus the shell")
	}
	model = typeInto(ctrlT(model), "new terminal")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model = updated.(ProjectModel); len(model.terms) != 2 {
		t.Fatalf("New terminal must start another shell: %d", len(model.terms))
	}
}

func TestTabSwitcher_OpensFromTheTreeTheEditorAndAsAnExCommand(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if ctrlT(model).tabs == nil {
		t.Fatal("the tab list key in the tree")
	}
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	if ctrlT(model).tabs == nil {
		t.Fatal("the tab list key in the editor")
	}
	for _, name := range []string{"tabs", "buffers", "ls"} {
		updated, _ := model.runProjectCommand(name)
		if updated.(ProjectModel).tabs == nil {
			t.Fatalf(":%s must open the tab list", name)
		}
	}
}

func TestCtrlT_ShowsHidesAndStartsTheTerminalFromTheTreeAndTheEditor(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	altT := tea.KeyMsg{Type: tea.KeyCtrlT}
	updated, _ := model.Update(altT)
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	if len(model.terms) != 1 || model.focus != focusTerminal {
		t.Fatal("ctrl+t starts a shell")
	}
	updated, _ = model.Update(altT)
	if model = updated.(ProjectModel); model.termShown {
		t.Fatal("ctrl+t hides the panel")
	}
	model.focus = focusProjectEdit
	updated, _ = model.Update(altT)
	if model = updated.(ProjectModel); !model.termShown || model.focus != focusTerminal || len(model.terms) != 1 {
		t.Fatal("ctrl+t from the editor shows the running shell again")
	}
}

func TestTabSwitcher_ShiftTabOpensItInTheTreeAndTheEditorsNormalModeOnly(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if projectSend(model, keyMsg("shift+tab")).tabs == nil {
		t.Fatal("shift+tab in the tree opens the tab list")
	}
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	if projectSend(model, keyMsg("shift+tab")).tabs == nil {
		t.Fatal("shift+tab in the editor's normal mode opens the tab list")
	}
	model, _ = projectKeys(model, "i") // insert mode: the key stays with the editor
	if projectSend(model, keyMsg("shift+tab")).tabs != nil {
		t.Fatal("shift+tab in insert mode must not open the tab list")
	}
}

func TestTabSwitcher_ShiftTabInTheTerminalPanelGoesToTheShell(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(ProjectModel)
	t.Cleanup(model.closeTerminal)
	updated, _ = model.Update(keyMsg("shift+tab"))
	if got := updated.(ProjectModel); got.tabs != nil || got.focus != focusTerminal {
		t.Fatal("the shell gets shift+tab, the tab list stays closed")
	}
	if ctrlT(model).tabs == nil {
		t.Fatal("alt+t opens the list from the panel")
	}
}
