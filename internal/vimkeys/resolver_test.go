package vimkeys

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

func mapping(t *testing.T, text string) keymap.Map {
	t.Helper()
	assignments, problems := keymap.Parse(strings.NewReader(text), "test")
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	m, problems := keymap.Default().Apply(assignments, "test")
	if len(problems) != 0 || len(m.Conflicts()) != 0 {
		t.Fatalf("problems: %v %v", problems, m.Conflicts())
	}
	return m
}

func modeOf(b *vimbuf.Buffer) Mode {
	if b.Mode() == vimbuf.Insert {
		return Insert
	}
	return Normal
}

// type feeds keys to a buffer through a resolver, the way the editors do.
func typeKeys(b *vimbuf.Buffer, r *Resolver, keys ...string) {
	for _, key := range keys {
		out := r.Feed(key, modeOf(b), b.WantsLiteral())
		if out.Cancel {
			b.CancelPending()
		}
		for _, k := range out.Keys {
			b.Handle(k)
		}
	}
}

func state(b *vimbuf.Buffer) (lines []string, row, col int, mode vimbuf.Mode) {
	row, col = b.Cursor()
	return b.Lines(), row, col, b.Mode()
}

const sample = "alpha beta gamma\n    delta epsilon\n\nzeta eta theta\niota kappa\n"

func newBuffer() *vimbuf.Buffer {
	return vimbuf.New(strings.Split(strings.TrimSuffix(sample, "\n"), "\n"), nil)
}

func TestFeed_TheDefaultMappingPassesEveryKeyOnToTheEngineUnchanged(t *testing.T) {
	scripts := [][]string{
		strings.Fields("d w j . x 3 j 2 d d u ctrl+r"),
		strings.Fields("g g G k k $ 0 ^ w b e l h"),
		strings.Fields("i h e l l o esc a ! esc A ? esc I > esc o n e w esc O u p esc"),
		strings.Fields("v l l d p P y y p Y P J ~ x X D u u"),
		strings.Fields("V j d u c w X esc C Z esc"),
		strings.Fields("r x l r y 3 r z / g a m m a enter n N : 3 enter g g"),
		strings.Fields("v j j o y G p ctrl+u pgdown pgup home end left right up down backspace"),
		append(strings.Fields("i a b c"), "tab", "ctrl+u", "x", "ctrl+k", "enter", "backspace", "delete", "esc"),
	}
	for i, script := range scripts {
		direct, through := newBuffer(), newBuffer()
		resolver := New(keymap.Default())
		typeKeys(through, resolver, script...)
		for _, key := range script {
			direct.Handle(key)
		}
		dl, dr, dc, dm := state(direct)
		tl, tr, tc, tm := state(through)
		if !reflect.DeepEqual(dl, tl) || dr != tr || dc != tc || dm != tm {
			t.Errorf("script %d: through the resolver %q %d,%d %v; direct %q %d,%d %v", i, tl, tr, tc, tm, dl, dr, dc, dm)
		}
	}
}

func TestFeed_ARebuiltCommandRunsOnItsNewKeyAndTheOldOneDoesNothing(t *testing.T) {
	r := New(mapping(t, "vim.insert = f3\nvim.delete = x\nvim.deletechar = f4\nvim.left = ctrl+l\n"))
	b := newBuffer()
	typeKeys(b, r, "i") // the old key: nothing
	if b.Mode() != vimbuf.Normal {
		t.Fatal("i must do nothing")
	}
	typeKeys(b, r, "f3", "Z", "esc")
	if lines, _, _, _ := state(b); lines[0] != "Zalpha beta gamma" {
		t.Fatalf("f3 inserts: %q", lines[0])
	}
	typeKeys(b, r, "x", "x") // vim.delete twice: the line
	if lines, _, _, _ := state(b); len(lines) != 4 || lines[0] != "    delta epsilon" {
		t.Fatalf("xx deletes the line: %q", lines)
	}
	typeKeys(b, r, "d", "d") // d is nothing now, and cancels
	if lines, _, _, _ := state(b); len(lines) != 4 {
		t.Fatalf("dd does nothing: %q", lines)
	}
	typeKeys(b, r, "l", "l")                // l is still the default of vim.right
	if _, _, col, _ := state(b); col != 6 { // from the first non blank (4) two to the right
		t.Fatalf("col %d", col)
	}
	typeKeys(b, r, "ctrl+l", "h") // ctrl+l is left now, h is nothing
	if _, _, col, _ := state(b); col != 5 {
		t.Fatalf("col %d", col)
	}
}

func TestFeed_ASequenceWaitsForItsKeysAndRunsTheCommand(t *testing.T) {
	r := New(mapping(t, "vim.bottom = space e\nvim.right = l\n"))
	b := newBuffer()
	typeKeys(b, r, " ")
	if r.Pending() != "space" {
		t.Fatalf("pending %q", r.Pending())
	}
	if _, row, _, _ := state(b); row != 0 {
		t.Fatal("nothing happens while the sequence is incomplete")
	}
	typeKeys(b, r, "e")
	if _, row, _, _ := state(b); row != 4 || r.Pending() != "" {
		t.Fatalf("space e goes to the last line: row %d pending %q", row, r.Pending())
	}
	typeKeys(b, r, "G") // the old key of vim.bottom does nothing: still on the last line
	if _, row, _, _ := state(b); row != 4 {
		t.Fatalf("G does nothing: row %d", row)
	}
	typeKeys(b, r, "g", "g") // vim.top is untouched
	if _, row, _, _ := state(b); row != 0 {
		t.Fatalf("g g is still the top: row %d", row)
	}
}

func TestFeed_AMismatchAfterAPartialSequenceCancelsEverythingPending(t *testing.T) {
	r := New(mapping(t, "vim.top = space t\nvim.right = l\n"))
	b := newBuffer()
	typeKeys(b, r, "G")
	typeKeys(b, r, "3", "d", " ", "x") // d space x: the sequence space x is no command: the operator is dropped as well
	if lines, row, _, _ := state(b); len(lines) != 5 || row != 4 {
		t.Fatalf("nothing was deleted: %q row %d", lines, row)
	}
	typeKeys(b, r, " ", "t")
	if _, row, _, _ := state(b); row != 0 {
		t.Fatalf("space t is the top: row %d", row)
	}
}

func TestFeed_TheKeyAfterRAndTheCommandLineAreLiteral(t *testing.T) {
	r := New(mapping(t, "vim.replace = f5\nvim.delete = x\nvim.deletechar = f6\n"))
	b := newBuffer()
	typeKeys(b, r, "f5", "d") // replace the character with a d, although d is no command any more
	if lines, _, _, _ := state(b); lines[0] != "dlpha beta gamma" {
		t.Fatalf("r then d: %q", lines[0])
	}
	typeKeys(b, r, "/", "x", "enter") // x is typed into the search line, not a command
	typeKeys(b, r, ":", "3", "enter")
	if _, row, _, _ := state(b); row != 2 {
		t.Fatalf(":3 went to line %d", row+1)
	}
}

func TestFeed_CountsAndEscapePassThroughAndDigitsStartNoSequence(t *testing.T) {
	r := New(mapping(t, "vim.down = f7\n"))
	b := newBuffer()
	typeKeys(b, r, "2", "f7")
	if _, row, _, _ := state(b); row != 2 {
		t.Fatalf("2 and the key of vim.down: row %d", row)
	}
	typeKeys(b, r, "2", "esc", "f7")
	if _, row, _, _ := state(b); row != 3 {
		t.Fatalf("esc drops the count: row %d", row)
	}
}

func TestFeed_InsertModeKeysAreRemappedAndTextIsNever(t *testing.T) {
	r := New(mapping(t, "insert.deletetostart = ctrl+x\ninsert.deletetoend = alt+k\ninsert.tab = alt+i\n"))
	b := newBuffer()
	typeKeys(b, r, "A", "a", "b", "c") // typed as text
	typeKeys(b, r, "ctrl+u")           // the old key is nothing now
	if lines, _, _, _ := state(b); lines[0] != "alpha beta gammaabc" {
		t.Fatalf("ctrl+u does nothing: %q", lines[0])
	}
	typeKeys(b, r, "alt+i", "ctrl+x")
	if lines, _, _, _ := state(b); lines[0] != "" {
		t.Fatalf("alt+i types a tab and ctrl+x deletes everything before the cursor: %q", lines[0])
	}
	typeKeys(b, r, "k", "j", "esc")
	if lines, _, _, _ := state(b); lines[0] != "kj" {
		t.Fatalf("printable keys are text: %q", lines[0])
	}
}

func TestFeed_AModeChangeDropsAHalfTypedSequence(t *testing.T) {
	r := New(mapping(t, "vim.top = space t\nvim.right = l, right\n"))
	r.Feed(" ", Normal, false)
	if r.Pending() == "" {
		t.Fatal("setup")
	}
	r.Feed("x", Insert, false)
	if r.Pending() != "" {
		t.Fatal("the sequence is dropped when the mode changes")
	}
}

func TestNilResolverPassesKeysOn(t *testing.T) {
	var r *Resolver
	if out := r.Feed("d", Normal, false); !reflect.DeepEqual(out.Keys, []string{"d"}) || out.Cancel {
		t.Fatalf("%+v", out)
	}
	if r.Pending() != "" {
		t.Fatal("no pending")
	}
	r.Reset()
}

func TestWants_OnlyTheKeysABindingStartsWith(t *testing.T) {
	r := New(mapping(t, "vim.insert = alt+i\ninsert.tab = alt+j\n"))
	if !r.Wants("alt+i") || r.Wants("alt+x") || r.Wants("alt+z") {
		t.Fatal("alt+i is bound, other alt keys are not")
	}
	var nilResolver *Resolver
	if nilResolver.Wants("alt+i") {
		t.Fatal("a nil resolver wants nothing")
	}
}
