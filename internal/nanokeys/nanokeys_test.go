package nanokeys

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// session is a session and a buffer with the default nano keys.
func session(lines ...string) (*Session, *vimbuf.Buffer) {
	m := keymap.Default()
	b := vimbuf.New(lines, nil)
	b.EnterModeless()
	return New(func(key string) keymap.Command {
		command, _ := m.Lookup(keymap.ScopeNano, key)
		return command
	}), b
}

func press(s *Session, b *vimbuf.Buffer, keys ...string) Outcome {
	var out Outcome
	for _, key := range keys {
		out = s.Handle(b, key)
	}
	return out
}

func typed(text string) []string { return strings.Split(text, "") }

func TestTyping_EditsTheTextAndAUndoStepIsARun(t *testing.T) {
	s, b := session("")
	press(s, b, typed("hello")...)
	press(s, b, "enter")
	press(s, b, typed("world")...)
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"hello", "world"}) {
		t.Fatalf("text %q", got)
	}
	press(s, b, "alt+u")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"hello", ""}) {
		t.Fatalf("alt+u undoes the last run: %q", got)
	}
	press(s, b, "alt+e")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"hello", "world"}) {
		t.Fatalf("alt+e redoes it: %q", got)
	}
	press(s, b, "backspace", "backspace", "left", "delete")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"hello", "wo"}) {
		t.Fatalf("backspace and delete: %q", got)
	}
}

func TestCutAndPaste_UseCtrlKAndCtrlU(t *testing.T) {
	s, b := session("one", "two", "three")
	press(s, b, "ctrl+k", "ctrl+k")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"three"}) {
		t.Fatalf("two cuts: %q", got)
	}
	press(s, b, "ctrl+u")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"one", "two", "three"}) {
		t.Fatalf("paste gives both lines back: %q", got)
	}
}

func TestMarkCopyAndCut(t *testing.T) {
	s, b := session("abc def")
	press(s, b, "alt+a")
	if s.Status() != "Mark set" || !b.HasMark() {
		t.Fatalf("status %q", s.Status())
	}
	press(s, b, "ctrl+right", "alt+6")
	if s.Status() != "Copied" || b.HasMark() {
		t.Fatalf("status %q", s.Status())
	}
	press(s, b, "alt+6")
	if !strings.Contains(s.Status(), "Nothing marked") {
		t.Fatalf("status %q", s.Status())
	}
}

func TestExit_AsksOnlyWhenThereAreChanges(t *testing.T) {
	s, b := session("x")
	if out := press(s, b, "ctrl+x"); out.Result != vimbuf.Quit || !out.Handled {
		t.Fatalf("an unchanged file is left at once: %+v", out)
	}
	press(s, b, "a")
	if out := press(s, b, "ctrl+x"); out.Result != vimbuf.None {
		t.Fatalf("a changed file asks first: %+v", out)
	}
	if label, _, open := s.Prompt(); !open || !strings.Contains(label, "Save modified buffer?") {
		t.Fatalf("prompt %q %v", label, open)
	}
	if out := press(s, b, "esc"); out.Result != vimbuf.None {
		t.Fatalf("esc cancels: %+v", out)
	}
	if _, _, open := s.Prompt(); open {
		t.Fatal("the prompt is closed")
	}
	press(s, b, "ctrl+x")
	if out := press(s, b, "y"); out.Result != vimbuf.WriteQuit {
		t.Fatalf("y saves and leaves: %+v", out)
	}
	press(s, b, "ctrl+x")
	if out := press(s, b, "n"); out.Result != vimbuf.ForceQuit {
		t.Fatalf("n leaves without saving: %+v", out)
	}
}

func TestSearch_PromptFindsAndReportsWhenThereIsNoMatch(t *testing.T) {
	s, b := session("alpha", "beta gamma", "delta")
	press(s, b, "ctrl+w")
	if label, _, open := s.Prompt(); !open || label != "Search" {
		t.Fatalf("prompt %q %v", label, open)
	}
	press(s, b, typed("gam")...)
	if _, input, _ := s.Prompt(); input != "gam" {
		t.Fatalf("input %q", input)
	}
	press(s, b, "backspace", "m", "enter")
	if row, col := b.Cursor(); row != 1 || col != 5 {
		t.Fatalf("cursor %d,%d", row, col)
	}
	press(s, b, "ctrl+w")
	press(s, b, typed("zzz")...)
	press(s, b, "enter")
	if !strings.Contains(s.Status(), "not found") {
		t.Fatalf("status %q", s.Status())
	}
	press(s, b, "alt+w") // the next match of the last search
	if _, _, open := s.Prompt(); open {
		t.Fatal("no prompt for alt+w")
	}
}

func TestGoto_AcceptsLineAndColumn(t *testing.T) {
	s, b := session("abc", "defgh", "ij")
	press(s, b, "alt+g")
	press(s, b, "2", ",", "3", "enter")
	if row, col := b.Cursor(); row != 1 || col != 2 {
		t.Fatalf("cursor %d,%d", row, col)
	}
	press(s, b, "ctrl+_", "x", "enter")
	if !strings.Contains(s.Status(), "Not a line number") {
		t.Fatalf("status %q", s.Status())
	}
}

func TestReplace_AsksPerMatch_AndAllAtOnce(t *testing.T) {
	s, b := session("a1 a2 a3", "a4")
	press(s, b, "alt+r")
	press(s, b, "a", "enter")
	press(s, b, "b", "enter") // replace with
	if label, _, open := s.Prompt(); !open || !strings.HasPrefix(label, "Replace this instance?") {
		t.Fatalf("prompt %q %v", label, open)
	}
	press(s, b, "y") // the match at the cursor: a1 becomes b1
	press(s, b, "n") // a2 stays
	press(s, b, "y") // a3 becomes b3
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"b1 a2 b3", "a4"}) {
		t.Fatalf("text %q (status %q)", got, s.Status())
	}
	press(s, b, "esc") // a4 is asked about: stop here
	if s.Status() != "Replaced 2 occurrences" {
		t.Fatalf("status %q", s.Status())
	}
	if _, _, open := s.Prompt(); open {
		t.Fatal("esc stops the replace")
	}
	// all at once
	s, b = session("x x x", "x")
	press(s, b, "alt+r", "x", "enter", "o", "enter")
	press(s, b, "a")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"o o o", "o"}) || s.Status() != "Replaced 4 occurrences" {
		t.Fatalf("text %q status %q", got, s.Status())
	}
	b.Undo()
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"x x x", "x"}) {
		t.Fatalf("one undo: %q", got)
	}
}

func TestReplace_ReportsNothingFound_AndEscStops(t *testing.T) {
	s, b := session("abc")
	press(s, b, "alt+r", "z", "enter", "y", "enter")
	if !strings.Contains(s.Status(), "not found") {
		t.Fatalf("status %q", s.Status())
	}
	press(s, b, "alt+r", "a", "enter", "A", "enter", "esc")
	if s.Status() != "Nothing replaced" {
		t.Fatalf("status %q", s.Status())
	}
}

func TestKeys_FollowTheBindingsOfTheMap(t *testing.T) {
	m, _ := keymap.Default().Apply(mustParse(t, "nano.exit = ctrl+q\n"), "test")
	b := vimbuf.New([]string{"x"}, nil)
	b.EnterModeless()
	s := New(func(key string) keymap.Command {
		command, _ := m.Lookup(keymap.ScopeNano, key)
		return command
	})
	if out := press(s, b, "ctrl+q"); out.Result != vimbuf.Quit {
		t.Fatalf("ctrl+q is exit now: %+v", out)
	}
	if out := press(s, b, "ctrl+x"); out.Handled {
		t.Fatalf("ctrl+x is no key of the editor any more: %+v", out)
	}
}

func TestUnknownKeysAreNotHandled(t *testing.T) {
	s, b := session("x")
	for _, key := range []string{"f5", "ctrl+z", "alt+q"} {
		if press(s, b, key).Handled {
			t.Errorf("%s must be left to the caller", key)
		}
	}
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("nothing typed: %q", got)
	}
}

func TestHintCommandsAreCommandsOfTheNanoScope(t *testing.T) {
	for _, h := range HintCommands {
		if keymap.ScopeOf(h.Command) != keymap.ScopeNano || h.Text == "" {
			t.Errorf("%+v", h)
		}
	}
}

func mustParse(t *testing.T, text string) []keymap.Assignment {
	t.Helper()
	assignments, problems := keymap.Parse(strings.NewReader(text), "test")
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	return assignments
}

func TestReplace_OffersAMatchAtTheCursorFirst(t *testing.T) {
	s, b := session("foo bar", "x foo")
	press(s, b, "alt+r", "f", "o", "o", "enter", "Z", "enter")
	if label, _, open := s.Prompt(); !open || !strings.HasPrefix(label, "Replace this instance?") {
		t.Fatalf("the match at the cursor is offered: %q %v", label, open)
	}
	press(s, b, "y", "y")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"Z bar", "x Z"}) {
		t.Fatalf("both matches are replaced: %q (status %q)", got, s.Status())
	}
	if s.Status() != "Replaced 2 occurrences" {
		t.Fatalf("status %q", s.Status())
	}
}

func TestPaste_GoesIntoTheOpenQuestionOnly(t *testing.T) {
	s, b := session("abc")
	if s.Paste("x") {
		t.Fatal("no question is open")
	}
	press(s, b, "ctrl+w")
	if !s.Paste("a\nb\x01c") {
		t.Fatal("the search question is open")
	}
	if _, input, _ := s.Prompt(); input != "abc" {
		t.Fatalf("control characters are left out: %q", input)
	}
	press(s, b, "esc", "ctrl+x", "y")
	if s.Paste("x") {
		t.Fatal("a yes/no question takes no text")
	}
}
