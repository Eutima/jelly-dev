// Package vimbuf is a small vim-like text buffer: normal, insert, visual and command-line mode on a list of
// lines. It knows nothing about the terminal: callers feed it key names (the strings bubbletea uses, such
// as "esc", "enter", "ctrl+r" or a single character) and draw the state they read back.
//
// Every line carries an origin number (the merge chunk it came from). Lines created by editing inherit the
// origin of a neighbour, so the caller can map the edited text back to its parts.
package vimbuf

import "strconv"

// Mode is the editing mode.
type Mode int

const (
	// Normal mode: keys are commands.
	Normal Mode = iota
	// Insert mode: keys are typed into the text.
	Insert
	// Visual mode: characterwise selection.
	Visual
	// VisualLine mode: linewise selection.
	VisualLine
	// Command mode: a ":" command is being typed.
	Command
)

// String is the text vim shows in its status line for the mode.
func (m Mode) String() string {
	switch m {
	case Insert:
		return "-- INSERT --"
	case Visual:
		return "-- VISUAL --"
	case VisualLine:
		return "-- VISUAL LINE --"
	case Command:
		return "COMMAND"
	}
	return "-- NORMAL --"
}

// Result is what the caller has to do after a key press.
type Result int

const (
	// None: nothing for the caller to do.
	None Result = iota
	// Write: apply the text (:w); the caller then calls MarkWritten.
	Write
	// WriteQuit: apply the text and leave (:wq, :x).
	WriteQuit
	// Quit: leave, the text is unchanged since the last write (:q).
	Quit
	// ForceQuit: leave and discard changes (:q!).
	ForceQuit
)

type snapshot struct {
	lines    [][]rune
	origin   []int
	row, col int
}

// Buffer is the editor state.
type Buffer struct {
	lines  [][]rune
	origin []int
	row    int
	col    int
	mode   Mode

	anchorRow, anchorCol int // start of the visual selection

	count   string // digits typed before a command
	opCount int    // count typed before an operator, 0 when none
	op      rune   // pending operator: d, c or y
	prefix  rune   // pending first key of a two key command: g or r
	cmd     string // text of the command line

	register     []string // unnamed register; one entry per line
	registerLine bool     // the register holds whole lines

	undo     []snapshot
	redo     []snapshot
	modified bool
	noLines  bool // all lines were deleted: the buffer shows one empty line but holds no text
	message  string
	page     int
}

// New returns a buffer for lines; origin has one entry per line. An empty text gives one empty line.
func New(lines []string, origin []int) *Buffer {
	b := &Buffer{page: 20}
	for i, line := range lines {
		b.lines = append(b.lines, []rune(line))
		o := 0
		if i < len(origin) {
			o = origin[i]
		}
		b.origin = append(b.origin, o)
	}
	if len(b.lines) == 0 {
		b.lines, b.origin, b.noLines = [][]rune{{}}, []int{0}, true
		if len(origin) > 0 {
			b.origin[0] = origin[0]
		}
	}
	return b
}

// SetPage sets the number of lines a page holds (for pgup, pgdown, ctrl+d and ctrl+u).
func (b *Buffer) SetPage(lines int) { b.page = max(lines, 2) }

// Lines returns the text; nil when every line was deleted.
func (b *Buffer) Lines() []string {
	if b.noLines {
		return nil
	}
	out := make([]string, len(b.lines))
	for i, line := range b.lines {
		out[i] = string(line)
	}
	return out
}

// Line returns line i as text.
func (b *Buffer) Line(i int) string { return string(b.lines[i]) }

// Origins returns the origin of every line; with Lines it has the same length unless the text is empty.
func (b *Buffer) Origins() []int {
	if b.noLines {
		return nil
	}
	return append([]int(nil), b.origin...)
}

// LineCount returns the number of lines shown (at least one).
func (b *Buffer) LineCount() int { return len(b.lines) }

// Cursor returns the cursor line and the rune column in it (zero-based).
func (b *Buffer) Cursor() (row, col int) { return b.row, b.col }

// Mode returns the current mode.
func (b *Buffer) Mode() Mode { return b.mode }

// Modified reports whether the text changed since New or the last MarkWritten.
func (b *Buffer) Modified() bool { return b.modified }

// MarkWritten records that the caller applied the text.
func (b *Buffer) MarkWritten() { b.modified = false }

// Message returns the last message (an error such as E37), cleared by the next key press.
func (b *Buffer) Message() string { return b.message }

// CommandLine returns the text after ":" while in command mode.
func (b *Buffer) CommandLine() string { return b.cmd }

// Pending returns the keys typed so far for an unfinished command, e.g. "2d".
func (b *Buffer) Pending() string {
	pending := b.count
	if b.opCount > 0 {
		pending = strconv.Itoa(b.opCount) + string(b.op) + pending
	} else if b.op != 0 {
		pending += string(b.op)
	}
	if b.prefix != 0 {
		pending += string(b.prefix)
	}
	return pending
}

// Selected returns the selected rune columns [from, to) of line row in visual mode.
func (b *Buffer) Selected(row int) (from, to int, ok bool) {
	if b.mode != Visual && b.mode != VisualLine {
		return 0, 0, false
	}
	r1, c1, r2, c2 := b.anchorRow, b.anchorCol, b.row, b.col
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	if row < r1 || row > r2 {
		return 0, 0, false
	}
	if b.mode == VisualLine {
		return 0, len(b.lines[row]) + 1, true
	}
	from, to = 0, len(b.lines[row])+1
	if row == r1 {
		from = c1
	}
	if row == r2 {
		to = c2 + 1
	}
	return from, to, true
}

// save pushes the state on the undo stack before a change.
func (b *Buffer) save() {
	b.undo = append(b.undo, b.snapshot())
	b.redo = nil
	b.modified = true
}

func (b *Buffer) snapshot() snapshot {
	s := snapshot{origin: append([]int(nil), b.origin...), row: b.row, col: b.col}
	s.lines = make([][]rune, len(b.lines))
	for i, line := range b.lines {
		s.lines[i] = append([]rune(nil), line...)
	}
	return s
}

func (b *Buffer) restore(s snapshot) {
	b.lines = make([][]rune, len(s.lines))
	for i, line := range s.lines {
		b.lines[i] = append([]rune(nil), line...)
	}
	b.origin = append([]int(nil), s.origin...)
	b.row, b.col = s.row, s.col
	b.noLines = false
	b.modified = true
}

func (b *Buffer) doUndo() {
	if len(b.undo) == 0 {
		b.message = "Already at oldest change"
		return
	}
	b.redo = append(b.redo, b.snapshot())
	b.restore(b.undo[len(b.undo)-1])
	b.undo = b.undo[:len(b.undo)-1]
	b.clampCursor()
}

func (b *Buffer) doRedo() {
	if len(b.redo) == 0 {
		b.message = "Already at newest change"
		return
	}
	b.undo = append(b.undo, b.snapshot())
	b.restore(b.redo[len(b.redo)-1])
	b.redo = b.redo[:len(b.redo)-1]
	b.clampCursor()
}

// clampCursor keeps the cursor inside the text: on a character in normal and visual mode, behind the last one in insert mode.
func (b *Buffer) clampCursor() {
	b.row = min(max(b.row, 0), len(b.lines)-1)
	limit := max(len(b.lines[b.row])-1, 0)
	if b.mode == Insert {
		limit = len(b.lines[b.row])
	}
	b.col = min(max(b.col, 0), limit)
}

func (b *Buffer) resetPending() {
	b.count, b.opCount, b.op, b.prefix = "", 0, 0, 0
}

func firstNonBlank(line []rune) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return max(len(line)-1, 0)
}
