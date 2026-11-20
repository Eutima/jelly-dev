package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-diff/internal/source"
)

func TestJellyFrame_KeepsSizeAndRepeats_WhenAnimationAdvances(t *testing.T) {
	first := JellyFrame(0)
	for i := 0; i < 20; i++ {
		frame := JellyFrame(i)
		if len(frame) != jellyHeight {
			t.Fatalf("frame %d has %d rows", i, len(frame))
		}
		for _, row := range frame {
			if runewidth.StringWidth(row) != runewidth.StringWidth(first[0]) {
				t.Fatalf("frame %d row %q has another width", i, row)
			}
		}
	}
	if strings.Join(JellyFrame(0), "") != strings.Join(JellyFrame(len(jellyFrames())), "") {
		t.Fatal("animation must repeat")
	}
	if strings.Join(JellyFrame(0), "") == strings.Join(JellyFrame(1), "") {
		t.Fatal("consecutive frames must differ (wobble)")
	}
	if len(JellyFrame(-3)) != jellyHeight {
		t.Fatal("negative index must work")
	}
}

func TestJellyLine_ShiftsWave_WhenFrameChanges(t *testing.T) {
	if JellyLine(0) == JellyLine(1) || runewidth.StringWidth(JellyLine(5)) != runewidth.StringWidth(JellyLine(0)) {
		t.Fatalf("%q %q", JellyLine(0), JellyLine(1))
	}
}

func TestGitView_ShowsJellyAndMessage_WhenLoading(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}
	updated, _ := quietGit(files, &fakeSource{}).Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	view := updated.(GitModel).View()
	if !strings.Contains(view, "██") || !strings.Contains(view, "Loading a.txt") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestGitView_OmitsJelly_WhenPaneTooSmall(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}
	updated, _ := quietGit(files, &fakeSource{}).Update(tea.WindowSizeMsg{Width: 100, Height: 6})
	view := updated.(GitModel).View()
	if strings.Contains(view, "██") || !strings.Contains(view, "Loading a.txt") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestGitUpdate_AdvancesFrameAndKeepsTicking_WhileLoading(t *testing.T) {
	files := []source.ChangedFile{{Status: source.StatusModified, Path: "a.txt"}}
	model := quietGit(files, &fakeSource{})
	updated, cmd := model.Update(jellyTickMsg{})
	next := updated.(GitModel)
	if next.frame != 1 || cmd == nil || !next.ticking {
		t.Fatalf("frame %d cmd %v ticking %v", next.frame, cmd, next.ticking)
	}
}

func TestGitUpdate_StopsTicking_WhenLoadingFinished(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	updated, cmd := model.Update(jellyTickMsg{})
	if cmd != nil || updated.(GitModel).ticking {
		t.Fatalf("animation must stop: cmd %v ticking %v", cmd, updated.(GitModel).ticking)
	}
}

func TestGitUpdate_RestartsAnimation_WhenNewLoadStartsAfterIdle(t *testing.T) {
	model, _ := newGitTestModel(100, 12)
	updated, _ := model.Update(jellyTickMsg{}) // idle: ticking becomes false
	_, cmd := updated.(GitModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	restarted := false
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				if _, isTick := c().(jellyTickMsg); isTick {
					restarted = true
				}
			}
		}
	}
	if !restarted {
		t.Fatal("a new load after idle must restart the animation")
	}
}

func TestMonoTheme_DrawsJellyWithoutColor_WhenRendered(t *testing.T) {
	jelly := MonoTheme().Jelly
	if jelly.GetForeground() != (DarkTheme().Jelly.UnsetForeground().GetForeground()) || !jelly.GetBold() {
		t.Fatal("mono jelly must be bold and set no color")
	}
}

// flatten runs a command and returns all messages it produces (batches are expanded).
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, flatten(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func TestGitUpdate_DiscardsCancelledLoadAndStopsAnimation_WhenUserReturnsToShownFile(t *testing.T) {
	model, src := newGitTestModel(100, 12)
	src.ctxs = nil
	press := func(key string) tea.Cmd {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		model = updated.(GitModel)
		return cmd
	}
	load := press("j") // start loading file 1
	press("k")         // back on the shown file 0: the load is cancelled
	if model.loading || strings.Contains(model.View(), "Loading") {
		t.Fatal("no load and no loading hint expected after returning to the shown file")
	}
	model = runLoads(model, load) // the cancelled load reports back late
	if model.loadedFile != 0 || model.selected != 0 {
		t.Fatalf("loaded %d selected %d", model.loadedFile, model.selected)
	}
	updated, _ := model.Update(jellyTickMsg{}) // idle: the animation stops
	model = updated.(GitModel)
	if model.ticking {
		t.Fatal("expected the animation to be idle")
	}
	restarted := false
	for _, msg := range flatten(press("j")) { // a new load must restart the animation
		if _, ok := msg.(jellyTickMsg); ok {
			restarted = true
		}
	}
	if !restarted || !model.ticking {
		t.Fatalf("a new load after idle must restart the animation: restarted %v ticking %v", restarted, model.ticking)
	}
}
