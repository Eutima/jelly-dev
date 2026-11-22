package vimbuf

import (
	"regexp"
	"strings"
	"unicode"
)

// The modeless use of the buffer: the text is edited the way nano does it, without normal, visual and command mode. The buffer stays in
// insert mode (EnterModeless); the caller maps its keys to the methods below. The text, the undo stack, the register and the system
// clipboard, the search highlight and the rendering are the ones of the vim use, so one editor can show both.

// modeless is the state that only the modeless use needs.
type modeless struct {
	on      bool   // EnterModeless was called
	run     string // the kind of the run of typed text in progress ("type", "backspace", "delete"), "" when none: it has its undo step already
	cutting bool   // the last operation cut a line: the next cut adds to the register
	marked  bool   // a mark is set (the text from the mark to the cursor is selected)

	markRow, markCol int
}

// EnterModeless puts the buffer in modeless use: insert mode for good, the cursor may stand behind the last character.
func (b *Buffer) EnterModeless() {
	b.ml.on = true
	b.mode = Insert
	b.resetPending()
	b.clampCursor()
}

// ExitModeless returns to vim use: normal mode.
func (b *Buffer) ExitModeless() {
	b.ml = modeless{}
	b.mode = Normal
	b.resetPending()
	b.cmd = ""
	b.clampCursor()
}

// Modeless reports whether the buffer is in modeless use.
func (b *Buffer) Modeless() bool { return b.ml.on }

// endRun ends the run of typed text (the next typing starts a new undo step) and the series of cuts.
func (b *Buffer) endRun() { b.ml.run, b.ml.cutting = "", false }

// begin starts an undo step for a run of the given kind unless that run is in progress.
func (b *Buffer) begin(kind string) {
	if b.ml.run != kind {
		b.save()
		b.ml.run = kind
	}
	b.ml.cutting = false
}

// TypeText inserts text at the cursor as typed: line breaks make new lines, tabs stay tabs. A run of typing is one undo step.
func (b *Buffer) TypeText(text string) {
	b.ClearMark()
	for _, r := range strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n") {
		switch {
		case r == '\n':
			b.Newline()
		case r == '\t' || r >= ' ':
			b.begin("type")
			b.revision++
			b.typeRunes([]rune{r})
		}
	}
}

// Newline breaks the line at the cursor.
func (b *Buffer) Newline() {
	b.ClearMark()
	b.begin("type")
	b.handleInsert("enter")
	b.ml.run = "" // the next line is a run of its own
}

// Backspace deletes the character before the cursor (at the start of a line it joins the line to the one above).
func (b *Buffer) Backspace() {
	b.ClearMark()
	if b.row == 0 && b.col == 0 {
		return
	}
	b.begin("backspace")
	b.handleInsert("backspace")
}

// DeleteForward deletes the character under the cursor (at the end of a line it joins the next line).
func (b *Buffer) DeleteForward() {
	b.ClearMark()
	if b.row == len(b.lines)-1 && b.col >= len(b.lines[b.row]) {
		return
	}
	b.begin("delete")
	b.handleInsert("delete")
}

// Move moves the cursor: left, right (they wrap to the neighbour line), up, down, home, end, pgup, pgdown, ctrl+left, ctrl+right (by
// words), ctrl+home, ctrl+end. It ends the run of typing. An unknown key moves nothing.
func (b *Buffer) Move(key string) {
	b.endRun()
	line := b.lines[b.row]
	switch key {
	case "left":
		switch {
		case b.col > 0:
			b.col--
		case b.row > 0:
			b.row--
			b.col = len(b.lines[b.row])
		}
	case "right":
		switch {
		case b.col < len(line):
			b.col++
		case b.row < len(b.lines)-1:
			b.row++
			b.col = 0
		}
	case "up":
		if b.row > 0 {
			b.row--
		}
	case "down":
		if b.row < len(b.lines)-1 {
			b.row++
		}
	case "home":
		b.col = 0
	case "end":
		b.col = len(line)
	case "pgup":
		b.row = max(b.row-b.page, 0)
	case "pgdown":
		b.row = min(b.row+b.page, len(b.lines)-1)
	case "ctrl+left":
		b.wordLeft()
	case "ctrl+right":
		b.wordRight()
	case "ctrl+home":
		b.row, b.col = 0, 0
	case "ctrl+end":
		b.row = len(b.lines) - 1
		b.col = len(b.lines[b.row])
	}
	b.col = min(b.col, len(b.lines[b.row]))
}

func (b *Buffer) wordLeft() {
	for {
		if b.col == 0 {
			if b.row == 0 {
				return
			}
			b.row--
			b.col = len(b.lines[b.row])
			continue
		}
		line := b.lines[b.row]
		for b.col > 0 && unicode.IsSpace(line[b.col-1]) {
			b.col--
		}
		if b.col == 0 {
			continue
		}
		for b.col > 0 && !unicode.IsSpace(line[b.col-1]) {
			b.col--
		}
		return
	}
}

func (b *Buffer) wordRight() {
	for {
		line := b.lines[b.row]
		if b.col >= len(line) {
			if b.row == len(b.lines)-1 {
				return
			}
			b.row++
			b.col = 0
			continue
		}
		for b.col < len(line) && !unicode.IsSpace(line[b.col]) {
			b.col++
		}
		for b.col < len(line) && unicode.IsSpace(line[b.col]) {
			b.col++
		}
		return
	}
}

// SetMark sets the mark at the cursor, or removes it when there is one: the text between the mark and the cursor is selected.
func (b *Buffer) SetMark() {
	b.endRun()
	if b.ml.marked {
		b.ml.marked = false
		return
	}
	b.ml.marked, b.ml.markRow, b.ml.markCol = true, b.row, b.col
}

// ClearMark removes the mark.
func (b *Buffer) ClearMark() { b.ml.marked = false }

// HasMark reports whether a mark is set.
func (b *Buffer) HasMark() bool { return b.ml.marked }

// markRange returns the marked text as start and end positions (the end is not included); ok is false without a mark or when the mark is at
// the cursor.
func (b *Buffer) markRange() (r1, c1, r2, c2 int, ok bool) {
	if !b.ml.marked {
		return 0, 0, 0, 0, false
	}
	r1, c1, r2, c2 = b.ml.markRow, b.ml.markCol, b.row, b.col
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	r1, r2 = min(r1, len(b.lines)-1), min(r2, len(b.lines)-1)
	return r1, c1, r2, c2, r1 != r2 || c1 != c2
}

// markedColumns is Selected for a marked text: the columns [from, to) of line row.
func (b *Buffer) markedColumns(row int) (from, to int, ok bool) {
	r1, c1, r2, c2, any := b.markRange()
	if !any || row < r1 || row > r2 {
		return 0, 0, false
	}
	from, to = 0, len(b.lines[row])+1
	if row == r1 {
		from = c1
	}
	if row == r2 {
		to = c2
	}
	return from, to, from < to
}

// CutLine cuts the marked text, or else the cursor line, into the register (and the system clipboard). Cuts that follow each other
// add up: pasting them gives all the lines back.
func (b *Buffer) CutLine() {
	adding := b.ml.cutting // read before endRun clears it
	b.endRun()
	if b.ml.marked {
		if _, _, _, _, any := b.markRange(); any {
			b.CutMarked()
			return
		}
		b.ml.marked = false
	}
	previous := append([]string(nil), b.register...)
	line := string(b.lines[b.row])
	b.linewiseOperator('d', b.row, b.row)
	b.col = 0
	if adding && b.registerLine {
		b.setRegister(append(previous, line), true)
	}
	b.ml.cutting = true
}

// CutMarked cuts the marked text into the register. It reports whether there was any.
func (b *Buffer) CutMarked() bool {
	b.endRun()
	r1, c1, r2, c2, any := b.markRange()
	b.ml.marked = false
	if !any {
		return false
	}
	b.charwiseOperator('d', r1, c1, r2, c2)
	return true
}

// CopyMarked copies the marked text into the register, leaving the text and the cursor as they are. It reports whether there was any.
func (b *Buffer) CopyMarked() bool {
	b.endRun()
	r1, c1, r2, c2, any := b.markRange()
	b.ml.marked = false
	if !any {
		return false
	}
	row, col := b.row, b.col
	b.charwiseOperator('y', r1, c1, r2, c2)
	b.row, b.col = row, col
	return true
}

// Uncut puts the register (what was cut or copied last, here or in another program) in the text: whole lines above the cursor line,
// other text at the cursor, which ends up behind it.
func (b *Buffer) Uncut() {
	b.endRun()
	b.ml.marked = false
	b.syncRegisterFromClipboard()
	if len(b.register) == 0 {
		b.message = "The cut buffer is empty"
		return
	}
	if b.registerLine {
		b.pasteRegister(false, 1)
		b.col = 0
		return
	}
	b.save()
	b.noLines = false
	text := strings.Join(b.register, "\n")
	row, col := b.row, b.col
	b.insertText(col, text)
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		b.row, b.col = row, col+len([]rune(parts[0]))
	} else {
		b.row, b.col = row+len(parts)-1, len([]rune(parts[len(parts)-1]))
	}
}

// Undo takes back the last change.
func (b *Buffer) Undo() {
	b.endRun()
	b.ml.marked = false
	b.doUndo()
}

// Redo does the last undone change again.
func (b *Buffer) Redo() {
	b.endRun()
	b.ml.marked = false
	b.doRedo()
}

// literal compiles a text that is searched as it is (nano's default); without caseSensitive capital letters do not matter.
func literal(pattern string, caseSensitive bool) *regexp.Regexp {
	prefix := "(?i)"
	if caseSensitive {
		prefix = ""
	}
	return regexp.MustCompile(prefix + regexp.QuoteMeta(pattern))
}

// Find searches for the text from the cursor on, forwards or backwards, wrapping around the end of the text, and highlights the matches.
// It reports whether there is a match; the cursor is on it then.
func (b *Buffer) Find(pattern string, backwards, caseSensitive bool) bool {
	b.endRun()
	if pattern == "" {
		if b.find.re == nil {
			b.message = "No current search pattern"
			return false
		}
		return b.FindNext(false)
	}
	b.find = search{pattern: pattern, re: literal(pattern, caseSensitive), backwards: backwards, highlight: true}
	return b.jump()
}

// FindHere is Find that also accepts a match that starts at the cursor (a replace begins with it); the search is forwards.
func (b *Buffer) FindHere(pattern string, caseSensitive bool) bool {
	b.endRun()
	if pattern == "" {
		return false
	}
	b.find = search{pattern: pattern, re: literal(pattern, caseSensitive), highlight: true}
	for _, m := range matchesIn(b.find.re, string(b.lines[b.row])) {
		if m[0] == b.col {
			return true
		}
	}
	return b.jump()
}

// FindNext goes to the next match of the last search (against its direction when reverse is set).
func (b *Buffer) FindNext(reverse bool) bool {
	b.endRun()
	if b.find.re == nil {
		b.message = "No current search pattern"
		return false
	}
	b.find.highlight = true
	if reverse {
		return b.jumpReverse()
	}
	return b.jump()
}

func (b *Buffer) jump() bool {
	b.message = ""
	b.jumpToMatch(false)
	return !strings.HasPrefix(b.message, "E486")
}

func (b *Buffer) jumpReverse() bool {
	b.message = ""
	b.jumpToMatch(true)
	return !strings.HasPrefix(b.message, "E486")
}

// LastSearch returns the text of the last search, "" when there was none.
func (b *Buffer) LastSearch() string { return b.find.pattern }

// ReplaceHere replaces the match of the last search that starts at the cursor with text and puts the cursor behind it. It reports whether
// the cursor was on a match.
func (b *Buffer) ReplaceHere(text string) bool {
	b.endRun()
	if b.find.re == nil {
		return false
	}
	line := string(b.lines[b.row])
	for _, m := range matchesIn(b.find.re, line) {
		if m[0] == b.col {
			b.save()
			runes := b.lines[b.row]
			replaced := append(append(append([]rune(nil), runes[:m[0]]...), []rune(text)...), runes[m[1]:]...)
			b.lines[b.row] = replaced
			b.col = m[0] + len([]rune(text))
			return true
		}
	}
	return false
}

// ReplaceAll replaces every match of the last search in the whole text with text, as one change, and returns how many there were.
func (b *Buffer) ReplaceAll(text string) int {
	b.endRun()
	if b.find.re == nil {
		return 0
	}
	count := 0
	for _, line := range b.lines {
		count += len(matchesIn(b.find.re, string(line)))
	}
	if count == 0 {
		return 0
	}
	b.save()
	for i, line := range b.lines {
		b.lines[i] = []rune(b.find.re.ReplaceAllLiteralString(string(line), text))
	}
	b.clampCursor()
	return count
}

// GotoPosition moves the cursor to the one-based line and column (clamped to the text).
func (b *Buffer) GotoPosition(line, column int) {
	b.endRun()
	b.row = min(max(line-1, 0), len(b.lines)-1)
	b.col = min(max(column-1, 0), len(b.lines[b.row]))
}
