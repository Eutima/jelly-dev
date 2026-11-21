package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eutima/jelly-dev/internal/source"
)

func TestCommandKeys_MapsShownTextToKeys(t *testing.T) {
	for shown, want := range map[string][]string{
		"→": {"right"}, "⌫": {"backspace"}, "n/p": {"n"}, "s": {"s"}, "ctrl+o": {"ctrl+o"},
		":wq": {":", "w", "q", "enter"}, ":q!": {":", "q", "!", "enter"}, "esc": {"esc"},
	} {
		if got := commandKeys(shown); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", shown, got, want)
		}
	}
}

func TestKeyMsg_RoundTripsWithString(t *testing.T) {
	for _, name := range []string{"enter", "esc", "backspace", "left", "right", "down", "tab", "ctrl+o", "ctrl+d", "q", "w", "!", ":"} {
		if got := keyMsg(name).String(); got != name {
			t.Errorf("%q came back as %q", name, got)
		}
	}
}

func TestBar_ClickRegionsFollowDisplayWidth(t *testing.T) {
	theme := DarkTheme()
	b := &bar{}
	b.add(barPart{" ", theme.Footer})
	b.command("a", "first", true, theme)   // " [a] first": columns 1..10
	b.command("b", "second", false, theme) // disabled: inert
	b.command("日", "wide", true, theme)    // a wide rune takes two columns
	if keys := b.keysAt(0); keys != nil {
		t.Errorf("leading space must not be clickable: %v", keys)
	}
	if keys := b.keysAt(1); !reflect.DeepEqual(keys, []string{"a"}) {
		t.Errorf("start of the first command: %v", keys)
	}
	if keys := b.keysAt(10); keys != nil {
		t.Errorf("the gap between commands must not be clickable: %v", keys)
	}
	for x := 12; x < 12+len("[b] second"); x++ {
		if keys := b.keysAt(x); keys != nil {
			t.Fatalf("a disabled command must be inert at %d: %v", x, keys)
		}
	}
	if width := lipgloss.Width(b.render(200, theme.Footer)); width != 200 {
		t.Errorf("width %d", width)
	}
	if keys := b.visibleKeysAt(5, 3); keys != nil {
		t.Errorf("columns beyond the drawn width must not be clickable: %v", keys)
	}
}

// clickOn clicks the first column of the footer command whose "[key] label" text is in line y of the plain view.
func clickOn(t *testing.T, model tea.Model, view, text string) tea.Model {
	t.Helper()
	lines := strings.Split(plainText(view), "\n")
	for y, line := range lines {
		if index := strings.Index(line, text); index >= 0 {
			x := len([]rune(line[:index]))
			updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
			return updated
		}
	}
	t.Fatalf("%q not found in the view:\n%s", text, view)
	return nil
}

func plainText(view string) string {
	return stripANSI(view)
}

func TestMergeFooter_ClicksRunTheCommands(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	merged := clickOn(t, model, model.View(), "[n/p] change").(MergeModel)
	if merged.current < 0 {
		t.Fatal("[n/p] must select the first change")
	}
	merged = clickOn(t, merged, merged.View(), "[→] ours").(MergeModel)
	if merged.Result()[1] != "X" {
		t.Fatalf("[→] must apply ours: %v", merged.Result())
	}
	merged = clickOn(t, merged, merged.View(), "[u] undo").(MergeModel)
	if merged.Unresolved() != 2 {
		t.Fatalf("[u] must undo, unresolved %d", merged.Unresolved())
	}
	merged = clickOn(t, merged, merged.View(), "[b] both").(MergeModel)
	if got := merged.Result(); merged.Unresolved() != 1 || got[1] != "X" || got[2] != "Y" {
		t.Fatalf("[b] must take both sides: %v unresolved %d", got, merged.Unresolved())
	}
}

func TestMergeFooter_DisabledCommandsDoNothing(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	for _, command := range []string{"[→] ours", "[s] save", "[u] undo"} { // nothing selected, unresolved, nothing to undo
		after := clickOn(t, model, model.View(), command).(MergeModel)
		if after.Unresolved() != 2 || len(after.history) != 0 || after.outcome.Saved {
			t.Errorf("%s must be inert", command)
		}
	}
}

func TestMergeFooter_EditorCommandsAreClickable(t *testing.T) {
	model, _, _ := editModel(t, editBase, editOurs, editTheirs)
	model = vimKeys(mergePress(model, "e"), "x")
	model = clickOn(t, model, model.View(), "[:w] apply").(MergeModel)
	if !model.editing || model.Result()[0] != "" {
		t.Fatalf("[:w] must apply and stay: editing %v result %v", model.editing, model.Result())
	}
	model = clickOn(t, model, model.View(), "[:q] leave").(MergeModel)
	if model.editing {
		t.Fatal("[:q] must leave the editor")
	}
}

func TestMergeFooter_CompactFooterAndOtherRowsIgnoreClicks(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	small, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	for y := 0; y < 8; y++ {
		updated, _ := small.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: y})
		if after := updated.(MergeModel); after.Unresolved() != 2 || len(after.history) != 0 {
			t.Fatalf("row %d: the compact footer must not react", y)
		}
	}
}

func TestDiffFooter_ClickTogglesUnified(t *testing.T) {
	diffModel := NewDiffModel("a", "b", []string{"x"}, []string{"y"})
	updated, _ := diffModel.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	model := clickOn(t, updated, updated.View(), "[t] split/unified").(DiffModel)
	if !model.showUnified {
		t.Fatal("[t] must switch to the unified view")
	}
}

func TestDirMergeFooter_ClickQuits(t *testing.T) {
	files := []source.ChangedFile{{Path: "a.txt"}}
	model := NewDirMergeModel("left", "right", files, nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	view := updated.View()
	lines := strings.Split(plainText(view), "\n")
	y := len(lines) - 1
	x := strings.Index(lines[y], "[q] quit")
	if x < 0 {
		t.Fatalf("no [q] quit in the footer: %q", lines[y])
	}
	_, cmd := updated.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
}

func TestGitFooter_ClicksReachListAndDiffCommands(t *testing.T) {
	model, _ := newGitTestModel(140, 12)
	model = settle(model)
	view := model.View()
	// the list footer (left) and the diff footer (right) are both clickable
	for _, command := range []string{"[tab]", "[t] split/unified"} {
		lines := strings.Split(plainText(view), "\n")
		y, x := -1, -1
		for row := len(lines) - footerMax; row < len(lines); row++ { // the footer may take more than one line
			if at := strings.Index(lines[row], command); at >= 0 {
				y, x = row, len([]rune(lines[row][:at]))
			}
		}
		if y < 0 {
			t.Fatalf("%q missing in the footer: %q", command, lines[len(lines)-footerMax:])
		}
		updated, _ := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
		after := updated.(GitModel)
		switch command {
		case "[tab]":
			if after.focus == model.focus {
				t.Errorf("[tab] must switch the focus")
			}
		default:
			if after.diff.showUnified == model.diff.showUnified {
				t.Errorf("[t] must toggle the unified view")
			}
		}
	}
}

func TestBar_ShrinksToTheTerminalWidth(t *testing.T) {
	theme := DarkTheme()
	build := func() *bar {
		b := &bar{}
		b.add(barPart{" NORMAL ", theme.Header})
		b.command(":w", "save", true, theme)
		b.command(":q", "quit", true, theme)
		b.command("ctrl+f", "files", true, theme)
		b.hint(barPart{"hint", theme.Footer})
		return b
	}
	full := build()
	shown := func(width int) string { return stripANSI(full.render(width, theme.Footer)) }
	if got := shown(full.width); !strings.Contains(got, "[ctrl+f] files") || !strings.Contains(got, "hint") {
		t.Fatalf("a wide terminal shows everything: %q", got)
	}
	got := shown(full.width - 6) // too narrow by a little: the hint goes first, then the last labels lose their text
	if strings.Contains(got, "hint") || !strings.Contains(got, "[:w] save") || strings.Contains(got, "files") || !strings.Contains(got, "[ctrl+f]") {
		t.Fatalf("narrower: %q", got)
	}
	if got = shown(20); !strings.Contains(got, "[:w]") || strings.Contains(got, "ctrl+f") || len([]rune(got)) != 20 {
		t.Fatalf("very narrow: %q", got)
	}
	for width := 1; width <= full.width; width++ {
		if w := len([]rune(shown(width))); w != width {
			t.Fatalf("width %d rendered %d columns", width, w)
		}
	}
	// what is shown is what a click reaches
	if keys := full.visibleKeysAt(11, 20); len(keys) == 0 || keys[0] != ":" { // "[:w]" starts at column 9
		t.Fatalf("keys at the shortened command: %v", keys)
	}
}

func TestProject_BarsFitSmallTerminals(t *testing.T) {
	for _, width := range []int{20, 30, 45, 60, 80} {
		model, _ := newProjectFixture(t, width, sampleFiles)
		model, _ = projectKeys(model, "G", "enter")
		lines := strings.Split(stripANSI(model.View()), "\n")
		if len(lines) != model.height {
			t.Fatalf("width %d: %d lines for a window of %d: the footers must not push the window out", width, len(lines), model.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d line %d is %d wide: %q", width, i, w, line)
			}
		}
	}
	model, _ := newProjectFixture(t, 45, sampleFiles)
	model, _ = projectKeys(model, "G", "enter")
	lines := strings.Split(stripANSI(model.View()), "\n")
	if !strings.Contains(lines[0], "Project") {
		t.Fatalf("the top bar keeps its first button: %q", lines[0])
	}
}

// A narrow editor does not cut the commands off at the edge of the window: they wrap onto a second line, with their labels while
// that fits, and every command is still there (and clickable).
func TestEditor_FooterWrapsInsteadOfCuttingTheCommands(t *testing.T) {
	model, _ := newEditorForTest(t, "alpha\nbeta\n")
	model = model.WithForwardedCommands()
	for _, width := range []int{60, 50, 40} {
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		narrow := updated.(EditorModel)
		lines := strings.Split(stripANSI(narrow.View()), "\n")
		if len(lines) != 12 {
			t.Fatalf("width %d: %d lines", width, len(lines))
		}
		footer := strings.Join(lines[len(lines)-2:], "\n")
		for _, command := range []string{"[:w]", "[:q]", "[ctrl+f]", "[ctrl+g]"} {
			if !strings.Contains(footer, command) {
				t.Errorf("width %d: %s was cut off:\n%s", width, command, footer)
			}
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w != width {
				t.Fatalf("width %d: a line is %d wide: %q", width, w, line)
			}
		}
	}
	// at 60 columns the labels fit on two lines: nothing was shortened
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	view := stripANSI(updated.(EditorModel).View())
	for _, label := range []string{"[:w] save", "[:q] quit", "[ctrl+f] files", "[ctrl+g] text"} {
		if !strings.Contains(view, label) {
			t.Errorf("at 60 columns %q still fits and must not be shortened:\n%s", label, view)
		}
	}
}

// A click on a command of the second footer line runs it.
func TestEditor_ClickOnTheSecondFooterLineRunsTheCommand(t *testing.T) {
	model, _ := newEditorForTest(t, "alpha\n")
	model = model.WithForwardedCommands().WithNavigator(&fakeNav{}, t.TempDir()) // gd, gr and :fmt are enabled: they can be clicked
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	model = updated.(EditorModel)
	rows := model.footerRows()
	if len(rows) != 2 {
		t.Fatalf("at 40 columns the footer has two lines: %d", len(rows))
	}
	if len(rows[1].regions) == 0 {
		t.Fatal("the second line has a command")
	}
	x, keys := rows[1].regions[0].from+1, rows[1].regions[0].keys
	if got := model.footerKeys(x, model.height-1); !reflect.DeepEqual(got, keys) {
		t.Fatalf("a click on the second line gives its command: %v, want %v", got, keys)
	}
	if got := model.footerKeys(x, model.height-2); reflect.DeepEqual(got, keys) {
		t.Fatalf("the first line has other commands there: %v", got)
	}
}

func TestWrapWords_BreaksAtSpacesAndCutsLongWordsAndEndsWithEllipsis(t *testing.T) {
	tests := []struct {
		text       string
		width, max int
		want       []string
	}{
		{"hello world", 20, 3, []string{"hello world"}},
		{"hello big world", 9, 3, []string{"hello big", "world"}},
		{"abcdefghij", 4, 5, []string{"abcd", "efgh", "ij"}},
		{"one two three four five six", 8, 2, []string{"one two", "three…"}},
		{"", 10, 3, []string{""}},
		{"日本語の テスト", 6, 3, []string{"日本語", "の", "テスト"}},
		{"日本", 1, 3, []string{"日", "本"}}, // a rune wider than the line is taken whole: the cut makes progress
		{"日本語", 1, 2, []string{"日", "…"}},
		{"a b c d e f g", 1, 3, []string{"a", "b", "…"}},
		{"abc", 1, 1, []string{"…"}},
	}
	for _, tt := range tests {
		if got := wrapWords(tt.text, tt.width, tt.max); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("wrapWords(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.max, got, tt.want)
		}
	}
}

// Nothing in wrapWords may take long or loop: not with a window of one column, not with a text that is a megabyte long.
func TestWrapWords_AlwaysFinishesQuickly(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for width := 1; width <= 3; width++ {
			for _, text := range []string{"日本", "😀😀😀", "a日b本c", strings.Repeat("日本語 ", 50), strings.Repeat("x", 10000)} {
				if lines := wrapWords(text, width, 3); len(lines) < 1 || len(lines) > 3 {
					t.Errorf("wrapWords(%q, %d, 3) gave %d lines", text, width, len(lines))
				}
			}
		}
		start := time.Now()
		wrapWords(strings.Repeat("word ", 400000), 20, 3) // two megabytes of words
		if took := time.Since(start); took > time.Second {
			t.Errorf("a long text took %v: the work must not grow with the text", took)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("wrapWords does not finish")
	}
}

// A bar that cannot fit in two lines loses its last commands, it is never cut in the middle of a command.
func TestBar_WrapDropsTheLastCommandsWhenTwoLinesAreNotEnough(t *testing.T) {
	theme := DarkTheme()
	b := &bar{}
	b.add(barPart{" -- NORMAL --  some-long-file-name.go  120:45   ", theme.Header})
	for _, key := range []string{":w", ":q", "ctrl+f", "ctrl+g", "gd", "gr", ":fmt"} {
		b.command(key, "label", true, theme)
	}
	lines := b.renderLines(24, 2, theme.Footer)
	if len(lines) != 2 {
		t.Fatalf("at most two lines: %d", len(lines))
	}
	text := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(text, "[:fmt]") || !strings.Contains(text, "[:w]") {
		t.Fatalf("the first commands stay, the last ones go:\n%s", text)
	}
	for _, line := range lines {
		if lipgloss.Width(line) != 24 {
			t.Fatalf("every line has the width of the window: %q", line)
		}
	}
	if got := b.keysAtLine(1, 5, 24, 2); got != nil { // a line that does not exist
		t.Fatalf("no keys below the last line: %v", got)
	}
}

// The hints under the tree wrap onto a second line in a narrow pane, and a click on that line runs its command.
func TestProject_TreeHintsWrapAndTheSecondLineIsClickable(t *testing.T) {
	model, _ := newProjectFixture(t, 72, sampleFiles) // the tree pane is 18 columns wide
	rows := model.treeFooterRows(model.treeFooterWidth())
	if len(rows) != 2 {
		t.Fatalf("the hints take two lines in a narrow pane: %d", len(rows))
	}
	bar := model.treeFooterBar()
	lines := bar.wrap(model.treeFooterWidth(), treeFooterBars)
	if len(lines[1].regions) == 0 {
		t.Fatal("the second line has a command")
	}
	region := lines[1].regions[0]
	y := topBarHeight + treeHeaderLines + model.treeBody() + 1 // the second footer line, on the screen
	updated, cmd := model.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: region.from + 1, Y: y})
	got := settleProject(updated.(ProjectModel), cmd)
	switch region.keys[0] {
	case "ctrl+f":
		if got.search == nil {
			t.Fatal("a click on [ctrl+f] on the second line opens the search")
		}
	default:
		t.Logf("the second line starts with %v", region.keys)
		if got.search == nil && got.picker == nil && got.focus == model.focus && !got.termShown {
			t.Fatalf("a click on %v on the second line did nothing", region.keys)
		}
	}
}

// footerText returns the last lines of a view as plain text.
func footerText(view string, lines int) string {
	all := strings.Split(plainText(view), "\n")
	return strings.Join(all[max(len(all)-lines, 0):], "\n")
}

func TestViewFooters_WrapInNarrowTerminalsInsteadOfDroppingCommands(t *testing.T) {
	const height = 14
	for _, width := range []int{34, 44, 60} {
		views := map[string]struct {
			view     string
			commands []string
		}{}
		diffUpdated, _ := NewDiffModel("a", "b", []string{"x"}, []string{"y"}).Update(tea.WindowSizeMsg{Width: width, Height: height})
		views["diff"] = struct {
			view     string
			commands []string
		}{diffUpdated.View(), []string{"[↑↓] next/prev change", "[j/k] scroll", "[t] split/unified", "[q] quit"}}
		dirUpdated, _ := NewDirMergeModel("left", "right", []source.ChangedFile{{Path: "a.txt"}}, nil).
			Update(tea.WindowSizeMsg{Width: width, Height: height})
		views["dirmerge"] = struct {
			view     string
			commands []string
		}{dirUpdated.View(), []string{"[↑↓] files", "[enter] merge into left", "[q] quit"}}
		for name, tc := range views {
			footer := footerText(tc.view, footerMax)
			if got := len(strings.Split(tc.view, "\n")); got != height {
				t.Errorf("%s at %d columns: the view has %d lines, want %d", name, width, got, height)
			}
			for _, command := range tc.commands {
				// a command may have lost its label to fit; its key must still be there
				key := command[:strings.Index(command, "]")+1]
				if !strings.Contains(footer, key) {
					t.Errorf("%s at %d columns: %s is gone from the footer:\n%s", name, width, key, footer)
				}
			}
		}
	}
}

func TestGitView_FootersWrapAndKeepTheViewHeight(t *testing.T) {
	for _, width := range []int{40, 70, 140} {
		model, _ := newGitTestModel(width, 14)
		model = settle(model)
		view := model.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 14 {
			t.Fatalf("width %d: %d lines, want 14", width, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("width %d line %d is %d columns wide", width, i, got)
			}
		}
		footer := footerText(view, footerMax)
		for _, key := range []string{"[↑↓]", "[enter]", "[tab]", "[q]", "[t]"} {
			if !strings.Contains(footer, key) {
				t.Errorf("width %d: %s is gone from the footer:\n%s", width, key, footer)
			}
		}
	}
}

func TestMergeFooter_WrapsInANarrowHighTerminal(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	for _, width := range []int{40, 60, 90} {
		sized, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := sized.View()
		if got := len(strings.Split(view, "\n")); got != 24 {
			t.Fatalf("width %d: the view has %d lines, want 24", width, got)
		}
		footer := footerText(view, 4)
		for _, key := range []string{"[n/p]", "[m]", "[e]", "[u]", "[s]", "[q]"} {
			if !strings.Contains(footer, key) {
				t.Errorf("width %d: %s is gone from the footer:\n%s", width, key, footer)
			}
		}
	}
}

func TestMergeFooter_CommandOnTheSecondLineIsClickable(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	view := sized.View()
	lines := strings.Split(plainText(view), "\n")
	// the quit command is the last one: it sits on the last line when the shortcuts wrap
	y := len(lines) - 1
	x := strings.Index(lines[y], "[q]")
	if x < 0 {
		t.Fatalf("[q] is not on the last footer line: %q", lines[y])
	}
	if keys := sized.(MergeModel).footerKeys(len([]rune(lines[y][:x])), y); len(keys) == 0 {
		t.Fatal("[q] on the wrapped line must be clickable")
	}
}

func TestWelcomeFooter_WrapsInANarrowTerminal(t *testing.T) {
	model, _ := NewWelcomeModel(nil, nil, nil)
	for _, width := range []int{36, 50} {
		sized, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 12})
		view := sized.View()
		if got := len(strings.Split(view, "\n")); got != 12 {
			t.Fatalf("width %d: %d lines, want 12", width, got)
		}
		footer := footerText(view, footerMax)
		for _, key := range []string{"[enter]", "[o]", "[d]", "[q]"} {
			if !strings.Contains(footer, key) {
				t.Errorf("width %d: %s is gone from the footer:\n%s", width, key, footer)
			}
		}
	}
}

func TestWelcomeFooter_LongMessageWrapsAndKeepsTheHeight(t *testing.T) {
	model, _ := NewWelcomeModel(nil, nil, nil)
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	welcome := sized.(WelcomeModel)
	welcome.status = "Cannot open /very/long/path/to/a/folder that does not exist anywhere"
	view := welcome.View()
	if got := len(strings.Split(view, "\n")); got != 12 {
		t.Fatalf("%d lines, want 12", got)
	}
	if !strings.Contains(plainText(view), "anywhere") {
		t.Fatalf("the message is cut off:\n%s", plainText(view))
	}
}

func TestSearchFooter_WrapsAndStaysClickable(t *testing.T) {
	model, _ := newSearch(DarkTheme(), t.TempDir(), searchFiles, "", 34, 12, nil)
	view := model.View()
	if got := len(strings.Split(view, "\n")); got != 12 {
		t.Fatalf("%d lines, want 12", got)
	}
	footer := footerText(view, footerMax)
	for _, key := range []string{"[tab]", "[esc]"} {
		if !strings.Contains(footer, key) {
			t.Fatalf("%s is gone from the footer:\n%s", key, footer)
		}
	}
	lines := strings.Split(plainText(view), "\n")
	for y := len(lines) - footerMax; y < len(lines); y++ {
		if x := strings.Index(lines[y], "[esc]"); x >= 0 {
			if keys := rowKeysAt(model.footerRows(), len([]rune(lines[y][:x])), y, model.width, model.height); len(keys) == 0 {
				t.Fatal("[esc] must be clickable on its wrapped line")
			}
			return
		}
	}
	t.Fatal("[esc] not found")
}

func TestMergeView_KeepsItsHeightInShortAndEditingStates(t *testing.T) {
	model, _ := newMergeTestModel(twoConflictsBase, twoConflictsOurs, twoConflictsTheirs, &saver{})
	for _, height := range []int{9, 10, 12, 13, 14, 30} {
		sized, _ := model.Update(tea.WindowSizeMsg{Width: 36, Height: height})
		if got := len(strings.Split(sized.View(), "\n")); got != height {
			t.Errorf("height %d: the view has %d lines", height, got)
		}
	}
	editing, _, _ := editModel(t, editBase, editOurs, editTheirs)
	narrow, _ := mergePress(editing, "e").Update(tea.WindowSizeMsg{Width: 36, Height: 24})
	view := narrow.View()
	if got := len(strings.Split(view, "\n")); got != 24 {
		t.Fatalf("editing: the view has %d lines, want 24", got)
	}
	if !strings.Contains(footerText(view, 2), "[:q!]") && !strings.Contains(footerText(view, 2), "[:w]") {
		t.Fatalf("the editor hints are gone:\n%s", footerText(view, 3))
	}
}

func TestDiffAndDirMergeFooters_SecondLineIsClickable(t *testing.T) {
	diffModel, _ := NewDiffModel("a", "b", []string{"x"}, []string{"y"}).Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	lines := strings.Split(plainText(diffModel.View()), "\n")
	y := len(lines) - 1
	x := strings.Index(lines[y], "[q]")
	if x < 0 {
		t.Fatalf("[q] is not on the last diff footer line: %q", lines[y])
	}
	if _, cmd := diffModel.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: len([]rune(lines[y][:x])), Y: y}); cmd == nil {
		t.Fatal("[q] on the wrapped diff footer must quit")
	}
	dir, _ := NewDirMergeModel("left", "right", []source.ChangedFile{{Path: "a.txt"}}, nil).Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	lines = strings.Split(plainText(dir.View()), "\n")
	for y := len(lines) - footerMax; y < len(lines); y++ {
		if x := strings.Index(lines[y], "[q]"); x >= 0 {
			if _, cmd := dir.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: len([]rune(lines[y][:x])), Y: y}); cmd == nil {
				t.Fatal("[q] on the wrapped dirmerge footer must quit")
			}
			return
		}
	}
	t.Fatal("[q] not found in the dirmerge footer")
}
