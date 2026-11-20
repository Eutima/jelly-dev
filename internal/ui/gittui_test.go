package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-diff/internal/source"
)

type fakeSource struct {
	texts map[string][2]string
	errs  map[string]error
	loads []string
}

func (f *fakeSource) Labels() (string, string) { return "HEAD", "working tree" }

func (f *fakeSource) Load(file source.ChangedFile) (string, string, error) {
	f.loads = append(f.loads, file.Path)
	if err := f.errs[file.Path]; err != nil {
		return "", "", err
	}
	pair := f.texts[file.Path]
	return pair[0], pair[1], nil
}

func newGitTestModel(width, height int) (GitModel, *fakeSource) {
	src := &fakeSource{
		texts: map[string][2]string{"a.txt": {"alpha\n", "ALPHA\n"}, "b.txt": {"one\n", "two\n"}, "c.bin": {}},
		errs:  map[string]error{"c.bin": source.ErrBinary},
	}
	files := []source.ChangedFile{
		{Status: source.StatusModified, Path: "a.txt"},
		{Status: source.StatusModified, Path: "b.txt"},
		{Status: source.StatusAdded, Path: "c.bin"},
	}
	updated, _ := quietGit(files, src).Update(tea.WindowSizeMsg{Width: width, Height: height})
	return settle(updated.(GitModel)), src
}

// runLoads executes returned commands (including batches) and feeds finished background loads back into
// the model. Animation ticks are ignored, so the loop always ends.
func runLoads(model GitModel, cmd tea.Cmd) GitModel {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case loadedMsg:
			updated, more := model.Update(msg)
			model = updated.(GitModel)
			queue = append(queue, more)
		}
	}
	return model
}

// settle runs the initial background load.
func settle(model GitModel) GitModel { return runLoads(model, model.Init()) }

// quietGit builds a model whose animation ticks fire immediately, so tests never wait for timers.
func quietGit(files []source.ChangedFile, src ChangeSource) GitModel {
	model := NewGitModel(files, src)
	model.interval = 0
	return model
}

func gitPress(model GitModel, key string) GitModel {
	var msg tea.KeyMsg
	switch key {
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	updated, cmd := model.Update(msg)
	return runLoads(updated.(GitModel), cmd)
}

func TestGitView_ShowsFileListAndFirstDiff_WhenOpened(t *testing.T) {
	view, _ := newGitTestModel(100, 12)
	text := view.View()
	for _, want := range []string{"Changes (3)", "a.txt", "b.txt", "c.bin", "alpha", "ALPHA", "HEAD: a.txt", "working tree: a.txt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("view lacks %q:\n%s", want, text)
		}
	}
}

func TestGitUpdate_LoadsNextFile_WhenJPressed(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	model = gitPress(model, "j")
	if model.selected != 1 || src.loads[len(src.loads)-1] != "b.txt" || !strings.Contains(model.View(), "two") {
		t.Fatalf("selected %d loads %v", model.selected, src.loads)
	}
}

func TestGitUpdate_ClampsSelection_WhenMovingPastEnds(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	model = gitPress(model, "k")
	if model.selected != 0 {
		t.Fatalf("selected %d", model.selected)
	}
	model = gitPress(model, "G")
	model = gitPress(model, "j")
	if model.selected != 2 {
		t.Fatalf("selected %d", model.selected)
	}
}

func TestGitView_ShowsMessage_WhenFileIsBinary(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	model = gitPress(model, "G")
	if !strings.Contains(model.View(), "Binary file - not shown") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestGitView_ShowsErrorText_WhenLoadFailsUnexpectedly(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	src.errs["b.txt"] = errors.New("boom")
	model = gitPress(model, "j")
	if !strings.Contains(model.View(), "boom") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestGitUpdate_MovesFocusBetweenListAndDiff_WhenEnterAndEscPressed(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	if model = gitPress(model, "enter"); model.focus != focusDiff {
		t.Fatal("expected diff focus")
	}
	if model = gitPress(model, "esc"); model.focus != focusList {
		t.Fatal("expected list focus")
	}
}

func TestGitUpdate_DoesNotChangeFile_WhenDiffHasFocus(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	model = gitPress(gitPress(model, "enter"), "j")
	if model.selected != 0 {
		t.Fatalf("selected %d", model.selected)
	}
}

func TestGitUpdate_NavigatesChangesInDiff_WhenNPressedInListFocus(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	if model = gitPress(model, "n"); model.diff.current != 0 {
		t.Fatalf("current %d", model.diff.current)
	}
}

func TestGitUpdate_Quits_WhenQPressed(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit")
	}
}

func TestGitView_FitsTerminal_WhenVariousSizes(t *testing.T) {
	for _, size := range [][2]int{{100, 12}, {60, 8}, {41, 6}, {20, 5}} {
		model, _ := newGitTestModel(size[0], size[1])
		for i, line := range strings.Split(model.View(), "\n") {
			if got := lipgloss.Width(line); got > size[0] {
				t.Fatalf("size %v line %d is %d wide: %q", size, i, got, line)
			}
		}
		if got := len(strings.Split(model.View(), "\n")); got != size[1] {
			t.Fatalf("size %v: %d lines", size, got)
		}
	}
}

func TestGitView_ScrollsList_WhenSelectionLeavesVisibleRows(t *testing.T) {
	files := make([]source.ChangedFile, 30)
	for i := range files {
		files[i] = source.ChangedFile{Status: source.StatusModified, Path: strings.Repeat("f", i+1)}
	}
	model := quietGit(files, &fakeSource{texts: map[string][2]string{}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	model = gitPress(settle(updated.(GitModel)), "G")
	if model.listTop != 30-model.listBodyHeight() || !strings.Contains(model.View(), strings.Repeat("f", 30)) {
		t.Fatalf("listTop %d", model.listTop)
	}
}

func TestGitView_StripsControlCharacters_WhenPathContainsEscapes(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "evil\x1b]0;x\x07.txt"}}
	model := quietGit(files, &fakeSource{texts: map[string][2]string{}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	view := settle(updated.(GitModel)).View()
	if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x07") {
		t.Fatalf("escape in view: %q", view)
	}
}

func TestGitView_SanitizesAndFitsMessage_WhenLoadErrorContainsEscapesAndIsLong(t *testing.T) {
	src := &fakeSource{
		texts: map[string][2]string{},
		errs:  map[string]error{"evil\x1b]0;x\x07.txt": errors.New("boom \x1b[31m" + strings.Repeat("long ", 80))},
	}
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "evil\x1b]0;x\x07.txt"}}
	updated, _ := quietGit(files, src).Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	view := settle(updated.(GitModel)).View()
	if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x07") || strings.Contains(view, "\x1b[31m") {
		t.Fatalf("escape in view: %q", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("line %d is %d wide", i, got)
		}
	}
	if !strings.Contains(view, "boom") {
		t.Fatalf("message missing:\n%s", view)
	}
}

func TestGitView_ExplainsUnmergedAndSubmodule_WhenThoseErrorsOccur(t *testing.T) {
	for err, want := range map[error]string{source.ErrUnmerged: "Unmerged", source.ErrSubmodule: "Submodule"} {
		if got := describeLoadError(err); !strings.Contains(got, want) {
			t.Fatalf("%v: %q", err, got)
		}
	}
}

func TestGitView_ShowsLoadingHint_WhenLoadNotFinished(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}
	updated, _ := quietGit(files, &fakeSource{texts: map[string][2]string{"a.txt": {"x\n", "y\n"}}}).
		Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	if view := updated.(GitModel).View(); !strings.Contains(view, "Loading a.txt") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestGitUpdate_LoadsOnlyLatestSelection_WhenKeysPressedDuringLoad(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	src.loads = nil
	// Start a load of file 1, then move on twice while it is still running.
	updated, firstLoad := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = updated.(GitModel)
	if firstLoad == nil {
		t.Fatal("expected a load command")
	}
	for _, key := range []string{"k", "j", "j"} {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		model = updated.(GitModel)
		if cmd != nil {
			t.Fatalf("key %q must not start a second load while one is running", key)
		}
	}
	model = runLoads(model, firstLoad)
	if model.selected != 2 || model.loadedFile != 2 || strings.Join(src.loads, ",") != "b.txt,c.bin" {
		t.Fatalf("selected %d loaded %d loads %v", model.selected, model.loadedFile, src.loads)
	}
	if !strings.Contains(model.View(), "Binary file") {
		t.Fatalf("view:\n%s", model.View())
	}
}

func TestGitUpdate_DoesNotReload_WhenSelectionUnchanged(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	before := len(src.loads)
	model = gitPress(model, "k")
	if len(src.loads) != before {
		t.Fatalf("reloaded: %v", src.loads)
	}
}

func TestGitUpdate_KeepsShownFile_WhenUserReturnsToItDuringLoad(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	src.loads = nil
	updated, load := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // start loading b.txt
	model = updated.(GitModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}) // back to the shown a.txt
	model = updated.(GitModel)
	if view := model.View(); strings.Contains(view, "Loading") || !strings.Contains(view, "ALPHA") {
		t.Fatalf("shown file must stay visible:\n%s", view)
	}
	model = runLoads(model, load)
	if model.loadedFile != 0 || strings.Join(src.loads, ",") != "b.txt" || model.loading {
		t.Fatalf("loaded %d loads %v loading %v", model.loadedFile, src.loads, model.loading)
	}
}
