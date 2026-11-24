package vimbuf

import (
	"reflect"
	"strings"
	"testing"
)

// typeKeys feeds keys written like vim input: plain characters, and <esc> <cr> <bs> <del> <tab> <c-r> <left> ... in angle brackets.
func typeKeys(b *Buffer, keys string) (last Result) {
	for len(keys) > 0 {
		key := keys[:1]
		if keys[0] == '<' {
			end := strings.IndexByte(keys, '>')
			name := keys[1:end]
			key = map[string]string{"esc": "esc", "cr": "enter", "bs": "backspace", "del": "delete", "tab": "tab",
				"c-r": "ctrl+r", "c-v": "ctrl+v", "left": "left", "right": "right", "up": "up", "down": "down"}[name]
			keys = keys[end+1:]
		} else {
			r := []rune(keys)
			key = string(r[0])
			keys = string(r[1:])
		}
		if result := b.Handle(key); result != None {
			last = result
		}
	}
	return last
}

func newBuffer(lines ...string) *Buffer {
	origin := make([]int, len(lines))
	for i := range origin {
		origin[i] = i
	}
	return New(lines, origin)
}

func TestKeys(t *testing.T) {
	tests := []struct {
		name  string
		start []string
		keys  string
		want  []string
		row   int
		col   int
	}{
		{"x deletes a character", []string{"abc"}, "x", []string{"bc"}, 0, 0},
		{"3x deletes three", []string{"abcdef"}, "3x", []string{"def"}, 0, 0},
		{"x at the end moves the cursor back", []string{"ab"}, "$x", []string{"a"}, 0, 0},
		{"X deletes before the cursor", []string{"abc"}, "lX", []string{"bc"}, 0, 0},
		{"dd deletes the line", []string{"a", "b", "c"}, "jdd", []string{"a", "c"}, 1, 0},
		{"2dd deletes two lines", []string{"a", "b", "c"}, "2dd", []string{"c"}, 0, 0},
		{"dd then p puts the line below", []string{"a", "b", "c"}, "ddp", []string{"b", "a", "c"}, 1, 0},
		{"yy then P puts the line above", []string{"a", "b"}, "jyyP", []string{"a", "b", "b"}, 1, 0},
		{"dw deletes a word", []string{"foo bar baz"}, "dw", []string{"bar baz"}, 0, 0},
		{"d2w deletes two words", []string{"foo bar baz"}, "d2w", []string{"baz"}, 0, 0},
		{"dw on the last word keeps the line", []string{"foo bar", "next"}, "wdw", []string{"foo ", "next"}, 0, 3},
		{"cw changes to the end of the word", []string{"foo bar"}, "cwxy<esc>", []string{"xy bar"}, 0, 1},
		{"cw on the last character of a word", []string{"a b"}, "cwx<esc>", []string{"x b"}, 0, 0},
		{"d$ deletes to the end", []string{"foo bar"}, "wd$", []string{"foo "}, 0, 3},
		{"D deletes to the end", []string{"foo bar"}, "wD", []string{"foo "}, 0, 3},
		{"d0 deletes to the start", []string{"foo bar"}, "wd0", []string{"bar"}, 0, 0},
		{"C changes to the end", []string{"foo bar"}, "wCbaz<esc>", []string{"foo baz"}, 0, 6},
		{"cc changes the line", []string{"a", "b"}, "ccx<esc>", []string{"x", "b"}, 0, 0},
		{"s substitutes a character", []string{"abc"}, "lsX<esc>", []string{"aXc"}, 0, 1},
		{"3s substitutes three", []string{"abcdef"}, "3sX<esc>", []string{"Xdef"}, 0, 0},
		{"s at the end of a line appends", []string{"ab"}, "$sX<esc>", []string{"aX"}, 0, 1},
		{"s on an empty line inserts", []string{"", "b"}, "sX<esc>", []string{"X", "b"}, 0, 0},
		{"s then p pastes what it removed", []string{"abc"}, "s<esc>$p", []string{"bca"}, 0, 2},
		{"S changes the line", []string{"a", "foo", "c"}, "jSx<esc>", []string{"a", "x", "c"}, 1, 0},
		{"2S changes two lines into one", []string{"a", "b", "c"}, "2Sx<esc>", []string{"x", "c"}, 0, 0},
		{"visual s changes the selection", []string{"abcdef"}, "lvlsX<esc>", []string{"aXdef"}, 0, 1},
		{"visual S changes the lines", []string{"a", "b", "c"}, "vjSx<esc>", []string{"x", "c"}, 0, 0},
		{"dj deletes two lines", []string{"a", "b", "c"}, "dj", []string{"c"}, 0, 0},
		{"dG deletes to the end", []string{"a", "b", "c"}, "jdG", []string{"a"}, 0, 0},
		{"dgg deletes to the start", []string{"a", "b", "c"}, "jdgg", []string{"c"}, 0, 0},
		{"G goes to the last line", []string{"a", "b", "c"}, "Gx", []string{"a", "b", ""}, 2, 0},
		{"gg goes to the first line", []string{"a", "b"}, "Gggx", []string{"", "b"}, 0, 0},
		{"2G goes to line two", []string{"a", "b", "c"}, "2Gx", []string{"a", "", "c"}, 1, 0},
		{"w moves over words and lines", []string{"a b", "c"}, "www", []string{"a b", "c"}, 1, 0},
		{"b moves back", []string{"foo bar"}, "$bx", []string{"foo ar"}, 0, 4},
		{"e moves to the end of the word", []string{"foo bar"}, "ex", []string{"fo bar"}, 0, 2},
		{"i inserts before", []string{"ac"}, "ib<esc>", []string{"bac"}, 0, 0},
		{"a appends after", []string{"ac"}, "ab<esc>", []string{"abc"}, 0, 1},
		{"I inserts at the first word", []string{"  ac"}, "Ib<esc>", []string{"  bac"}, 0, 2},
		{"A appends at the end", []string{"ab"}, "Ac<esc>", []string{"abc"}, 0, 2},
		{"o opens a line below", []string{"a", "c"}, "ob<esc>", []string{"a", "b", "c"}, 1, 0},
		{"O opens a line above", []string{"b"}, "Oa<esc>", []string{"a", "b"}, 0, 0},
		{"enter splits the line", []string{"abcd"}, "lli<cr><esc>", []string{"ab", "cd"}, 1, 0},
		{"backspace deletes before the cursor", []string{"abc"}, "A<bs><bs><esc>", []string{"a"}, 0, 0},
		{"backspace at line start joins lines", []string{"a", "b"}, "ji<bs><esc>", []string{"ab"}, 0, 0},
		{"delete removes under the cursor", []string{"abc"}, "i<del><esc>", []string{"bc"}, 0, 0},
		{"arrows move in insert mode", []string{"ac"}, "i<right>b<esc>", []string{"abc"}, 0, 1},
		{"r replaces a character", []string{"abc"}, "rx", []string{"xbc"}, 0, 0},
		{"3r replaces three characters", []string{"abcd"}, "3rx", []string{"xxxd"}, 0, 2},
		{"J joins lines", []string{"a", "  b", "c"}, "J", []string{"a b", "c"}, 0, 1},
		{"~ toggles case", []string{"aBc"}, "~~", []string{"Abc"}, 0, 2},
		{"p pastes characters after", []string{"ab"}, "ylp", []string{"aab"}, 0, 1},
		{"yw then P pastes a word", []string{"foo bar"}, "ywwP", []string{"foo foo bar"}, 0, 7},
		{"yy 2p pastes twice", []string{"a"}, "yy2p", []string{"a", "a", "a"}, 1, 0},
		{"u undoes", []string{"abc"}, "xxu", []string{"bc"}, 0, 0},
		{"ctrl+r redoes", []string{"abc"}, "xuu<c-r>", []string{"bc"}, 0, 0},
		{"one undo reverts a whole insert", []string{"a"}, "Abcd<esc>u", []string{"a"}, 0, 0},
		{"visual d deletes the selection", []string{"abcdef"}, "lvlld", []string{"aef"}, 0, 1},
		{"visual y and p", []string{"abc"}, "vly$p", []string{"abcab"}, 0, 4},
		{"visual c changes", []string{"abcd"}, "vlcx<esc>", []string{"xcd"}, 0, 0},
		{"visual line d", []string{"a", "b", "c"}, "Vjd", []string{"c"}, 0, 0},
		{"visual line y and p", []string{"a", "b"}, "VjyGp", []string{"a", "b", "a", "b"}, 2, 0},
		{"visual across lines", []string{"ab", "cd"}, "lvjd", []string{"a"}, 0, 0},
		{"visual esc leaves", []string{"abc"}, "vl<esc>x", []string{"ac"}, 0, 1},
		{"deleting all lines leaves one empty line", []string{"a", "b"}, "dG", nil, 0, 0},
		{"count with 0 motion", []string{"abc"}, "$0x", []string{"bc"}, 0, 0},
		{"unknown keys are ignored", []string{"abc"}, "Qzx", []string{"bc"}, 0, 0},
		{"operator with no motion is cancelled", []string{"abc"}, "dQx", []string{"bc"}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBuffer(tt.start...)
			typeKeys(b, tt.keys)
			if got := b.Lines(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("lines = %q, want %q", got, tt.want)
			}
			if row, col := b.Cursor(); row != tt.row || col != tt.col {
				t.Errorf("cursor = %d,%d, want %d,%d", row, col, tt.row, tt.col)
			}
		})
	}
}

func TestModes(t *testing.T) {
	b := newBuffer("abc")
	steps := []struct {
		keys string
		want Mode
	}{{"i", Insert}, {"<esc>", Normal}, {"v", Visual}, {"V", VisualLine}, {"V", Normal}, {"v", Visual}, {"<esc>", Normal}, {":", Command}, {"<esc>", Normal}}
	for _, step := range steps {
		typeKeys(b, step.keys)
		if b.Mode() != step.want {
			t.Fatalf("after %q mode = %v, want %v", step.keys, b.Mode(), step.want)
		}
	}
}

func TestOriginsFollowLines(t *testing.T) {
	b := New([]string{"a", "b", "c"}, []int{10, 20, 30})
	typeKeys(b, "jddpOx<esc>")
	// after dd on b: [a c] origins 10 30; p puts b below c with c's origin; O above b with the same origin
	if got, want := b.Lines(), []string{"a", "c", "x", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if got, want := b.Origins(), []int{10, 30, 30, 30}; !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
}

func TestOriginOfSplitAndJoin(t *testing.T) {
	b := New([]string{"ab", "cd"}, []int{1, 2})
	typeKeys(b, "li<cr><esc>")
	if got, want := b.Origins(), []int{1, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after split origins = %v, want %v", got, want)
	}
	typeKeys(b, "J")
	if got, want := b.Origins(), []int{1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after join origins = %v, want %v", got, want)
	}
}

func TestCommands(t *testing.T) {
	tests := []struct {
		name     string
		keys     string
		want     Result
		message  string
		modified bool
	}{
		{":w writes", "x:w<cr>", Write, "", true},
		{":wq writes and quits", ":wq<cr>", WriteQuit, "", false},
		{":x writes and quits", ":x<cr>", WriteQuit, "", false},
		{":q quits an unmodified buffer", ":q<cr>", Quit, "", false},
		{":q refuses a modified buffer", "x:q<cr>", None, "E37", true},
		{":q! discards", "x:q!<cr>", ForceQuit, "", true},
		{"unknown command", ":foo<cr>", None, "E492", false},
		{"backspace edits the command line", ":wx<bs><cr>", Write, "", false},
		{"esc cancels the command", ":q<esc>", None, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBuffer("abc")
			if got := typeKeys(b, tt.keys); got != tt.want {
				t.Fatalf("result = %v, want %v", got, tt.want)
			}
			if tt.message == "" && b.Message() != "" || !strings.HasPrefix(b.Message(), tt.message) {
				t.Errorf("message = %q, want prefix %q", b.Message(), tt.message)
			}
			if b.Modified() != tt.modified {
				t.Errorf("modified = %v, want %v", b.Modified(), tt.modified)
			}
		})
	}
}

func TestMarkWrittenAllowsQuit(t *testing.T) {
	b := newBuffer("abc")
	typeKeys(b, "x")
	b.MarkWritten()
	if got := typeKeys(b, ":q<cr>"); got != Quit {
		t.Fatalf("result = %v, want Quit", got)
	}
}

func TestGotoLineCommand(t *testing.T) {
	b := newBuffer("a", "b", "c")
	typeKeys(b, ":3<cr>")
	if row, _ := b.Cursor(); row != 2 {
		t.Fatalf("row = %d, want 2", row)
	}
	typeKeys(b, ":99<cr>")
	if row, _ := b.Cursor(); row != 2 {
		t.Fatalf("row = %d, want 2 (clamped)", row)
	}
}

func TestSelectedReportsVisualRange(t *testing.T) {
	b := newBuffer("abcd", "efgh")
	typeKeys(b, "lvj")
	from, to, ok := b.Selected(0)
	if !ok || from != 1 || to != 5 {
		t.Errorf("row 0 = %d,%d,%v", from, to, ok)
	}
	from, to, ok = b.Selected(1)
	if !ok || from != 0 || to != 2 {
		t.Errorf("row 1 = %d,%d,%v", from, to, ok)
	}
	typeKeys(b, "<esc>")
	if _, _, ok := b.Selected(0); ok {
		t.Error("no selection outside visual mode")
	}
}

func TestEmptyBufferHasNoLines(t *testing.T) {
	b := New(nil, nil)
	if b.Lines() != nil || b.LineCount() != 1 {
		t.Fatalf("lines = %q, count = %d", b.Lines(), b.LineCount())
	}
	typeKeys(b, "ia<esc>")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("lines = %q", got)
	}
}

func TestUnicodeText(t *testing.T) {
	b := newBuffer("äöü x")
	typeKeys(b, "lx")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"äü x"}) {
		t.Fatalf("lines = %q", got)
	}
}
