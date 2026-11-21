package vimbuf

import (
	"strconv"
	"strings"
	"unicode"
)

// Handle processes one key press and returns what the caller has to do.
func (b *Buffer) Handle(key string) Result {
	b.message = ""
	switch b.mode {
	case Insert:
		b.handleInsert(key)
	case Command:
		return b.handleCommand(key)
	default:
		return b.handleNormal(key)
	}
	return None
}

// motion is a cursor target. exclusive motions leave the target character out of an operator's range.
type motion struct {
	row, col  int
	linewise  bool
	inclusive bool
}

func (b *Buffer) counted() int {
	n := 0
	if b.count != "" {
		n, _ = strconv.Atoi(b.count)
	}
	if b.opCount > 0 {
		if n == 0 {
			return b.opCount
		}
		return n * b.opCount
	}
	return n
}

func (b *Buffer) handleNormal(key string) Result {
	visual := b.mode == Visual || b.mode == VisualLine
	if b.prefix == 'r' {
		b.prefix = 0
		if r := []rune(key); len(r) == 1 && !visual {
			b.replaceChars(r[0], max(b.counted(), 1))
		}
		b.resetPending()
		return None
	}
	if b.prefix == 'g' {
		b.prefix = 0
		if key == "g" {
			b.applyMotion(b.gotoLine(b.count != "" || b.opCount > 0, b.counted()-1, 0), key)
		}
		b.resetPending()
		return None
	}
	if len(key) == 1 && (key >= "1" && key <= "9" || key == "0" && b.count != "") {
		b.count += key
		return None
	}
	n := max(b.counted(), 1)
	switch key {
	case "esc", "ctrl+c":
		if visual {
			b.mode = Normal
			b.clampCursor()
		}
		b.resetPending()
		return None
	case ":":
		b.mode, b.cmd = Command, ""
		b.resetPending()
		return None
	case "g":
		b.prefix = 'g'
		return None
	case "r":
		if !visual {
			b.prefix = 'r'
		}
		return None
	}
	if visual {
		return b.handleVisualKey(key, n)
	}
	switch key {
	case "d", "c", "y":
		if b.op == 0 {
			b.op = rune(key[0])
			b.opCount, b.count = b.counted(), ""
			return None
		}
		if b.op == rune(key[0]) { // dd, cc, yy
			b.linewiseOperator(b.op, b.row, min(b.row+n-1, len(b.lines)-1))
		}
		b.resetPending()
		return None
	}
	if m, ok := b.motionFor(key, n); ok {
		b.applyMotion(m, key)
		b.resetPending()
		return None
	}
	op := b.op
	b.resetPending()
	if op != 0 {
		return None // an operator followed by something that is no motion cancels it
	}
	b.normalCommand(key, n)
	return None
}

// applyMotion moves the cursor, or runs the pending operator over the motion's range.
func (b *Buffer) applyMotion(m motion, key string) {
	if b.op == 0 {
		b.row, b.col = m.row, m.col
		b.clampCursor()
		if b.mode == Visual || b.mode == VisualLine {
			b.row = min(b.row, len(b.lines)-1)
		}
		return
	}
	op := b.op
	if m.linewise {
		b.linewiseOperator(op, min(b.row, m.row), max(b.row, m.row))
		return
	}
	r1, c1, r2, c2 := b.row, b.col, m.row, m.col
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	if m.inclusive {
		c2++
	}
	if key == "w" && r2 > r1 { // dw on the last word of a line stops at the end of the line
		r2, c2 = r1, len(b.lines[r1])
	}
	b.charwiseOperator(op, r1, c1, r2, c2)
}

// motionFor computes the target of a motion key; ok is false when key is no motion.
func (b *Buffer) motionFor(key string, n int) (motion, bool) {
	row, col, last := b.row, b.col, len(b.lines)-1
	line := b.lines[row]
	switch key {
	case "h", "left", "backspace":
		return motion{row: row, col: max(col-n, 0)}, true
	case "l", "right", " ":
		limit := max(len(line)-1, 0)
		if b.op != 0 {
			limit = len(line)
		}
		return motion{row: row, col: min(col+n, limit)}, true
	case "j", "down", "ctrl+n":
		return motion{row: min(row+n, last), col: col, linewise: true}, true
	case "k", "up", "ctrl+p":
		return motion{row: max(row-n, 0), col: col, linewise: true}, true
	case "0", "home":
		return motion{row: row, col: 0}, true
	case "^":
		return motion{row: row, col: firstNonBlank(line)}, true
	case "$", "end":
		r := min(row+n-1, last)
		return motion{row: r, col: max(len(b.lines[r])-1, 0), inclusive: true}, true
	case "w":
		if b.op == 'c' && b.classAt(row, col) != classSpace { // cw changes to the end of the word, like ce
			r, c := b.wordChangeEnd(row, col, n)
			return motion{row: r, col: c, inclusive: true}, true
		}
		r, c := b.wordForward(row, col, n)
		return motion{row: r, col: c}, true
	case "b":
		r, c := b.wordBack(row, col, n)
		return motion{row: r, col: c}, true
	case "e":
		r, c := b.wordEnd(row, col, n)
		return motion{row: r, col: c, inclusive: true}, true
	case "G":
		return b.gotoLine(b.count != "" || b.opCount > 0, b.counted()-1, last), true
	case "ctrl+d":
		return motion{row: min(row+max(b.page/2, 1), last), col: col, linewise: true}, true
	case "ctrl+u":
		return motion{row: max(row-max(b.page/2, 1), 0), col: col, linewise: true}, true
	case "pgdown", "ctrl+f":
		return motion{row: min(row+b.page, last), col: col, linewise: true}, true
	case "pgup", "ctrl+b":
		return motion{row: max(row-b.page, 0), col: col, linewise: true}, true
	}
	return motion{}, false
}

// gotoLine is the target of G and gg: the given line when counted, else def.
func (b *Buffer) gotoLine(counted bool, line, def int) motion {
	row := def
	if counted {
		row = min(max(line, 0), len(b.lines)-1)
	}
	return motion{row: row, col: firstNonBlank(b.lines[row]), linewise: true}
}

type charClass int

const (
	classSpace charClass = iota
	classWord
	classPunct
)

func classOf(r rune) charClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	}
	return classPunct
}

// classAt is the class of the character at (row, col); the end of a line counts as space.
func (b *Buffer) classAt(row, col int) charClass {
	if col >= len(b.lines[row]) {
		return classSpace
	}
	return classOf(b.lines[row][col])
}

func (b *Buffer) wordForward(row, col, n int) (int, int) {
	last := len(b.lines) - 1
	for ; n > 0; n-- {
		if cl := b.classAt(row, col); cl != classSpace {
			for col < len(b.lines[row]) && b.classAt(row, col) == cl {
				col++
			}
		}
		for {
			if col >= len(b.lines[row]) {
				if row == last {
					col = len(b.lines[row])
					break
				}
				row, col = row+1, 0
				if len(b.lines[row]) == 0 {
					break // an empty line is a word
				}
				continue
			}
			if b.classAt(row, col) == classSpace {
				col++
				continue
			}
			break
		}
	}
	return row, col
}

func (b *Buffer) wordBack(row, col, n int) (int, int) {
	for ; n > 0; n-- {
		col--
		for {
			if col < 0 {
				if row == 0 {
					col = 0
					break
				}
				row--
				col = len(b.lines[row]) - 1
				if len(b.lines[row]) == 0 {
					col = 0
					break
				}
				continue
			}
			if b.classAt(row, col) == classSpace {
				col--
				continue
			}
			break
		}
		cl := b.classAt(row, col)
		for col > 0 && cl != classSpace && b.classAt(row, col-1) == cl {
			col--
		}
	}
	return row, col
}

// wordChangeEnd is the end of the n-th word for cw: the current word counts even when the cursor is on its last character.
func (b *Buffer) wordChangeEnd(row, col, n int) (int, int) {
	for i := 0; i < n; i++ {
		if i > 0 {
			row, col = b.wordForward(row, col, 1)
		}
		cl := b.classAt(row, col)
		for col+1 < len(b.lines[row]) && b.classAt(row, col+1) == cl {
			col++
		}
	}
	return row, col
}

func (b *Buffer) wordEnd(row, col, n int) (int, int) {
	last := len(b.lines) - 1
	for ; n > 0; n-- {
		col++
		for {
			if col >= len(b.lines[row]) {
				if row == last {
					return row, max(len(b.lines[row])-1, 0)
				}
				row, col = row+1, 0
				continue
			}
			if b.classAt(row, col) == classSpace {
				col++
				continue
			}
			break
		}
		cl := b.classAt(row, col)
		for col+1 < len(b.lines[row]) && b.classAt(row, col+1) == cl {
			col++
		}
	}
	return row, col
}

// normalCommand runs the commands that are no motion and no operator.
func (b *Buffer) normalCommand(key string, n int) {
	switch key {
	case "i":
		b.save()
		b.mode = Insert
	case "a":
		b.save()
		b.mode = Insert
		if len(b.lines[b.row]) > 0 {
			b.col++
		}
	case "I":
		b.save()
		b.mode = Insert
		b.col = firstNonBlank(b.lines[b.row])
		if len(strings.TrimSpace(string(b.lines[b.row]))) == 0 {
			b.col = len(b.lines[b.row])
		}
	case "A":
		b.save()
		b.mode, b.col = Insert, len(b.lines[b.row])
	case "o":
		b.save()
		b.openLine(b.row + 1)
	case "O":
		b.save()
		b.openLine(b.row)
	case "x":
		b.charwiseOperator('d', b.row, b.col, b.row, min(b.col+n, len(b.lines[b.row])))
	case "X":
		b.charwiseOperator('d', b.row, max(b.col-n, 0), b.row, b.col)
	case "D":
		b.charwiseOperator('d', b.row, b.col, min(b.row+n-1, len(b.lines)-1), len(b.lines[min(b.row+n-1, len(b.lines)-1)]))
	case "C":
		b.charwiseOperator('c', b.row, b.col, b.row, len(b.lines[b.row]))
	case "Y":
		b.linewiseOperator('y', b.row, min(b.row+n-1, len(b.lines)-1))
	case "p":
		b.paste(true, n)
	case "P":
		b.paste(false, n)
	case "J":
		b.joinLines(max(n, 2) - 1)
	case "~":
		b.toggleCase(n)
	case "u":
		b.doUndo()
	case "ctrl+r":
		b.doRedo()
	case "v":
		b.mode, b.anchorRow, b.anchorCol = Visual, b.row, b.col
	case "V":
		b.mode, b.anchorRow, b.anchorCol = VisualLine, b.row, b.col
	}
}

func (b *Buffer) handleVisualKey(key string, n int) Result {
	switch key {
	case "v":
		if b.mode == Visual {
			b.mode = Normal
		} else {
			b.mode = Visual
		}
		b.resetPending()
		return None
	case "V":
		if b.mode == VisualLine {
			b.mode = Normal
		} else {
			b.mode = VisualLine
		}
		b.resetPending()
		return None
	case "o":
		b.row, b.anchorRow = b.anchorRow, b.row
		b.col, b.anchorCol = b.anchorCol, b.col
		b.resetPending()
		return None
	case "d", "x", "y", "c":
		b.visualOperator(key)
		b.resetPending()
		return None
	}
	if m, ok := b.motionFor(key, n); ok {
		b.row, b.col = m.row, m.col
		b.clampCursor()
	}
	b.resetPending()
	return None
}

func (b *Buffer) visualOperator(key string) {
	r1, c1, r2, c2 := b.anchorRow, b.anchorCol, b.row, b.col
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	linewise := b.mode == VisualLine
	b.mode = Normal
	op := rune('d')
	switch key {
	case "y":
		op = 'y'
	case "c":
		op = 'c'
	}
	if linewise {
		b.linewiseOperator(op, r1, r2)
		return
	}
	b.charwiseOperator(op, r1, c1, r2, min(c2+1, len(b.lines[r2])))
}
