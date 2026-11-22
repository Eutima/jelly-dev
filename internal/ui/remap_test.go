package ui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/source"
)

// remap installs the mapping a .jdrc file with this text would give, for the rest of the test.
func remap(t *testing.T, text string) {
	t.Helper()
	assignments, problems := keymap.Parse(strings.NewReader(text), "test")
	if len(problems) != 0 {
		t.Fatalf("the test mapping has problems: %v", problems)
	}
	mapping, problems := keymap.Default().Apply(assignments, "test")
	if len(problems) != 0 {
		t.Fatalf("the test mapping has problems: %v", problems)
	}
	before := keys
	UseKeys(mapping)
	t.Cleanup(func() { UseKeys(before) })
}

func pressKeyName(model tea.Model, name string) tea.Model {
	updated, _ := model.Update(keyMsg(name))
	return updated
}

func TestKeyMsg_RoundTripsEveryKeyNameTheMappingAccepts(t *testing.T) {
	for _, name := range []string{"ctrl+j", "ctrl+t", "ctrl+z", "alt+x", "alt+X", "f1", "f5", "f12", "shift+tab", "ctrl+right", "ctrl+left", "ctrl+]", "alt+up",
		"alt+f", "esc", "tab", "enter", "backspace", "delete", "pgup", "pgdown", "home", "end", "up", "down", "left", "right", "n", "G", ":", " ", "ctrl+home", "shift+up"} {
		if got := keyMsg(name).String(); got != name {
			t.Errorf("keyMsg(%q).String() = %q", name, got)
		}
	}
}

func TestProject_RemappedCommandsRunOnTheirNewKeysOnly(t *testing.T) {
	remap(t, "tabs = ctrl+j\npalette = f2\nterminal = alt+x\n")
	model, _ := newProjectFixture(t, 160, sampleFiles)
	model.hooks.Terminal = func(string) *exec.Cmd { return exec.Command("/bin/sh", "-c", "sleep 30") }

	for _, old := range []string{"shift+tab", "alt+t"} {
		if after := pressKeyName(model, old).(ProjectModel); after.tabs != nil {
			t.Fatalf("the old key %s of the tab list must do nothing", old)
		}
	}
	opened := pressKeyName(model, "ctrl+j").(ProjectModel)
	if opened.tabs == nil || !strings.Contains(stripANSI(opened.View()), "Tabs (ctrl+j)") {
		t.Fatalf("the new key opens the tab list, titled with it:\n%s", opened.View())
	}
	if after := pressKeyName(model, "ctrl+a").(ProjectModel); after.palette != nil {
		t.Fatal("the old palette key must do nothing")
	}
	palette := pressKeyName(model, "f2").(ProjectModel)
	view := stripANSI(palette.View())
	if palette.palette == nil || !strings.Contains(view, "Commands (f2)") || !strings.Contains(view, "ctrl+j") {
		t.Fatalf("the palette shows the new keys:\n%s", view)
	}
	started := pressKeyName(model, "alt+x").(ProjectModel)
	t.Cleanup(started.closeTerminal)
	if len(started.terms) != 1 || started.focus != focusTerminal {
		t.Fatal("the new terminal key starts a shell")
	}
	footer := stripANSI(started.View())
	if !strings.Contains(footer, "alt+x hide") || !strings.Contains(footer, "ctrl+j tabs") {
		t.Fatalf("the terminal panel footer names the new keys:\n%s", footer)
	}
	if hidden := pressKeyName(started, "alt+x").(ProjectModel); hidden.termShown {
		t.Fatal("the terminal key hides the panel again")
	}
}

func TestProject_TheTopBarShowsAndClicksTheRemappedPaletteKey(t *testing.T) {
	remap(t, "palette = f2\n")
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if first := strings.Split(stripANSI(model.View()), "\n")[0]; !strings.Contains(first, "(f2)") {
		t.Fatalf("the top bar names the palette key: %q", first)
	}
	model, _ = clickBar(model, buttonX(t, model, paletteClick))
	if model.palette == nil {
		t.Fatal("a click on the button opens the palette")
	}
}

func TestProject_UnboundCommandsHaveNoKeyAndNoHint(t *testing.T) {
	remap(t, "palette = none\ndiff = none\n")
	model, _ := newProjectFixture(t, 100, sampleFiles)
	if after := pressKeyName(model, "ctrl+a").(ProjectModel); after.palette != nil {
		t.Fatal("an unbound command does nothing")
	}
	if first := strings.Split(stripANSI(model.View()), "\n")[0]; strings.Contains(first, "(") {
		t.Fatalf("the top bar shows no key for an unbound palette: %q", first)
	}
}

func TestEditor_RemappedProjectKeysWorkInNormalMode(t *testing.T) {
	remap(t, "grep = ctrl+y\nback = ctrl+b\n")
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	model = projectSend(model, keyMsg("ctrl+g"))
	if model.search != nil {
		t.Fatal("the old key of the text search must do nothing in the editor")
	}
	model = projectSend(model, keyMsg("ctrl+y"))
	if model.search == nil {
		t.Fatal("the new key opens the text search from the editor")
	}
}

func TestMerge_RemappedKeysRunTheirCommandsAndAreShown(t *testing.T) {
	remap(t, "merge.ours = alt+o\nmerge.save = ctrl+k\nmerge.quit = ctrl+x\nmerge.palette = f2\n")
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n", "n") // the conflict
	view := stripANSI(model.View())
	for _, want := range []string{"[alt+o] ours", "[f2] commands", "[←] theirs"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the footer lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "[→]") {
		t.Fatalf("the old key is not shown any more:\n%s", view)
	}
	if after := pressKeyName(model, "right").(MergeModel); after.Unresolved() != 1 {
		t.Fatal("the old key of ours must do nothing")
	}
	chosen := pressKeyName(model, "alt+o").(MergeModel)
	if chosen.Unresolved() != 0 {
		t.Fatal("the new key adds ours")
	}
	if notice := stripANSI(chosen.View()); !strings.Contains(notice, "press ctrl+k to save") {
		t.Fatalf("the message names the new save key:\n%s", notice)
	}
	if got := strings.Join(chosen.Result(), ","); !strings.Contains(got, "OURS7") {
		t.Fatalf("ours is in the result: %s", got)
	}
	clicked := clickOn(t, model, model.View(), "[alt+o] ours").(MergeModel)
	if clicked.Unresolved() != 0 {
		t.Fatal("a click on the footer hint sends the new key")
	}
	armed := pressKeyName(chosen, "q").(MergeModel)
	if armed.quitArmed {
		t.Fatal("the old quit key must do nothing")
	}
	armed = pressKeyName(chosen, "ctrl+x").(MergeModel)
	if !armed.quitArmed || !strings.Contains(stripANSI(armed.View()), "press ctrl+x again") {
		t.Fatalf("the new quit key asks again, naming itself:\n%s", armed.View())
	}
	palette := pressKeyName(model, "f2").(MergeModel)
	paletteView := stripANSI(palette.View())
	if palette.palette == nil || !strings.Contains(paletteView, "Commands (f2)") || !strings.Contains(paletteView, "alt+o") {
		t.Fatalf("the palette shows the new keys:\n%s", paletteView)
	}
}

func TestMerge_AnUnboundCommandHasNoHintAndItsOldKeyDoesNothing(t *testing.T) {
	remap(t, "merge.edit = none\n")
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n")
	if view := stripANSI(model.View()); strings.Contains(view, "[e] edit") {
		t.Fatalf("an unbound command is not offered:\n%s", view)
	}
	if after := pressKeyName(model, "e").(MergeModel); after.editing {
		t.Fatal("the old key must not start the editor")
	}
}

func TestMerge_ASwappedPairOfKeysWorksBothWays(t *testing.T) {
	remap(t, "merge.ours = left\nmerge.theirs = right\n")
	model, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	model = mergePress(model, "n", "n")
	view := stripANSI(model.View())
	if !strings.Contains(view, "[←] ours") || !strings.Contains(view, "[→] theirs") {
		t.Fatalf("the footer shows the swapped keys:\n%s", view)
	}
	if got := strings.Join(pressKeyName(model, "left").(MergeModel).Result(), ","); !strings.Contains(got, "OURS7") {
		t.Fatalf("left now adds ours: %s", got)
	}
}

func TestDiff_RemappedKeysAreShownAndWork(t *testing.T) {
	remap(t, "diff.split = alt+s\ndiff.quit = ctrl+x\ndiff.next = ctrl+n\ndiff.prev = ctrl+p\n")
	model := NewDiffModel("a", "b", []string{"a", "b", "c"}, []string{"a", "X", "c"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	view := stripANSI(updated.View())
	for _, want := range []string{"[ctrl+p/ctrl+n] next/prev change", "[alt+s] split/unified", "[ctrl+x] quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the footer lacks %q:\n%s", want, view)
		}
	}
	if after := pressKeyName(updated, "t").(DiffModel); after.showUnified {
		t.Fatal("the old split key must do nothing")
	}
	if after := pressKeyName(updated, "alt+s").(DiffModel); !after.showUnified {
		t.Fatal("the new key switches to the unified view")
	}
	if after := pressKeyName(updated, "ctrl+n").(DiffModel); after.current != 0 {
		t.Fatalf("the new key selects the first change: %d", after.current)
	}
	if _, cmd := updated.Update(keyMsg("q")); isQuit(cmd) {
		t.Fatal("the old quit key must do nothing")
	}
	if _, cmd := updated.Update(keyMsg("ctrl+x")); !isQuit(cmd) {
		t.Fatal("the new quit key quits")
	}
}

func gitKey(model GitModel, name string) GitModel {
	updated, cmd := model.Update(keyMsg(name))
	return runLoads(updated.(GitModel), cmd)
}

func TestGitView_BackspaceAndDeleteRevertTheChangeInTheDiffPane(t *testing.T) {
	for _, name := range []string{"backspace", "delete", "r"} {
		model, src := newEditModel(t, "a\nb\nc\nd\ne\n", "a\nB\nc\nd\nE\n")
		model = gitKey(model, name)
		if src.disk["a.txt"] != "a\nb\nc\nd\nE\n" {
			t.Errorf("%s must revert the selected change: %q", name, src.disk["a.txt"])
		}
	}
	// in the file list these keys do nothing: enter opens the diff there
	updated, _ := quietGit([]source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}, &writableSource{
		fakeSource: fakeSource{texts: map[string][2]string{"a.txt": {"a\n", "b\n"}}}, disk: map[string]string{"a.txt": "b\n"}}).Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	list := settle(updated.(GitModel))
	if after := gitKey(list, "delete"); after.focus != focusList {
		t.Fatal("delete in the list changes nothing")
	}
}

func TestEditor_TheTabListKeyOpensItInEveryEditorModeButShiftTabOnlyInNormalMode(t *testing.T) {
	model, _ := newProjectFixture(t, 100, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	if model.focus != focusProjectEdit {
		model, _ = projectKeys(model, "ctrl+e")
	}
	model, _ = projectKeys(model, "i") // insert mode
	if projectSend(model, keyMsg("alt+t")).tabs == nil {
		t.Fatal("alt+t opens the tab list in insert mode")
	}
	if projectSend(model, keyMsg("shift+tab")).tabs != nil {
		t.Fatal("shift+tab stays with the editor in insert mode")
	}
}

func TestCtrlC_IsTheWayOutOfEveryViewWhateverTheQuitCommandsAreBoundTo(t *testing.T) {
	remap(t, "merge.quit = none\ndiff.quit = none\n")
	merge, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	merge = mergePress(merge, "n")
	if _, cmd := merge.Update(keyMsg("q")); isQuit(cmd) {
		t.Fatal("q is unbound")
	}
	updated, _ := merge.Update(keyMsg("ctrl+c"))
	if got := updated.(MergeModel); !got.quitArmed { // unsaved: the first press asks
		t.Fatal("ctrl+c must still ask to quit the merge view")
	}
	diff := NewDiffModel("a", "b", []string{"a"}, []string{"b"})
	if _, cmd := diff.Update(keyMsg("q")); isQuit(cmd) {
		t.Fatal("q is unbound in the diff view")
	}
	if _, cmd := diff.Update(keyMsg("ctrl+c")); !isQuit(cmd) {
		t.Fatal("ctrl+c quits the diff view")
	}
}

func TestMerge_TheUnsavedQuitMessageNamesTheKeysInForce(t *testing.T) {
	merge, _ := newMergeTestModel(baseText, oursText, theirsText, &saver{})
	armed := pressKeyName(merge, "q").(MergeModel)
	if !strings.Contains(stripANSI(armed.View()), "press q again (or ctrl+c, ctrl+d)") {
		t.Fatalf("the default message:\n%s", armed.View())
	}
	remap(t, "merge.quit = ctrl+x\n")
	armed = pressKeyName(merge, "ctrl+x").(MergeModel)
	if !strings.Contains(stripANSI(armed.View()), "press ctrl+x again (or ctrl+c)") {
		t.Fatalf("the remapped message:\n%s", armed.View())
	}
}
