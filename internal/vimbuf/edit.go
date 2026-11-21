package vimbuf

import (
	"strings"
	"unicode"
)

// setRegister stores yanked or deleted text.
func (b *Buffer) setRegister(text []string, linewise bool) {
	b.register, b.registerLine = text, linewise
	if b.clip != nil {
		b.clip.put(registerText(text, linewise))
	}
}

// linewiseOperator applies d, c or y to the lines r1..r2.
func (b *Buffer) linewiseOperator(op rune, r1, r2 int) {
	text := make([]string, 0, r2-r1+1)
	for i := r1; i <= r2; i++ {
		text = append(text, string(b.lines[i]))
	}
	b.setRegister(text, true)
	if op == 'y' {
		b.row, b.col = r1, 0
		if b.mode != Normal {
			b.mode = Normal
		}
		b.clampCursor()
		return
	}
	b.save()
	if op == 'c' {
		first := b.origin[r1]
		b.lines = append(b.lines[:r1], append([][]rune{{}}, b.lines[r2+1:]...)...)
		b.origin = append(b.origin[:r1], append([]int{first}, b.origin[r2+1:]...)...)
		b.row, b.col, b.mode = r1, 0, Insert
		b.noLines = false
		return
	}
	first := b.origin[r1]
	b.lines = append(b.lines[:r1], b.lines[r2+1:]...)
	b.origin = append(b.origin[:r1], b.origin[r2+1:]...)
	if len(b.lines) == 0 {
		b.lines, b.origin, b.noLines = [][]rune{{}}, []int{first}, true
	}
	b.row = min(r1, len(b.lines)-1)
	b.col = firstNonBlank(b.lines[b.row])
}

// charwiseOperator applies d, c or y to the text from (r1,c1) up to, not including, (r2,c2).
func (b *Buffer) charwiseOperator(op rune, r1, c1, r2, c2 int) {
	c1 = min(c1, len(b.lines[r1]))
	c2 = min(c2, len(b.lines[r2]))
	var text []string
	if r1 == r2 {
		text = []string{string(b.lines[r1][c1:c2])}
	} else {
		text = append(text, string(b.lines[r1][c1:]))
		for i := r1 + 1; i < r2; i++ {
			text = append(text, string(b.lines[i]))
		}
		text = append(text, string(b.lines[r2][:c2]))
	}
	if r1 == r2 && c1 == c2 && op != 'c' {
		b.setRegister(text, false)
		return
	}
	b.setRegister(text, false)
	if op == 'y' {
		b.row, b.col = r1, c1
		b.clampCursor()
		return
	}
	b.save()
	joined := append(append([]rune(nil), b.lines[r1][:c1]...), b.lines[r2][c2:]...)
	b.lines = append(b.lines[:r1], append([][]rune{joined}, b.lines[r2+1:]...)...)
	b.origin = append(b.origin[:r1+1], b.origin[r2+1:]...)
	b.row, b.col = r1, c1
	if op == 'c' {
		b.mode = Insert
		b.noLines = false
	} else {
		b.clampCursor()
	}
}

// openLine inserts an empty line at index and starts typing in it.
func (b *Buffer) openLine(index int) {
	owner := b.origin[min(b.row, len(b.origin)-1)]
	b.lines = append(b.lines[:index], append([][]rune{{}}, b.lines[index:]...)...)
	b.origin = append(b.origin[:index], append([]int{owner}, b.origin[index:]...)...)
	b.row, b.col, b.mode = index, 0, Insert
	b.noLines = false
}

// OpenLineBelow adds an empty line below the cursor line and starts typing in it (a caller shortcut for "o").
func (b *Buffer) OpenLineBelow() {
	b.save()
	b.openLine(b.row + 1)
}

// GotoLine moves the cursor to the one-based line number.
func (b *Buffer) GotoLine(line int) {
	b.row = min(max(line-1, 0), len(b.lines)-1)
	b.col = firstNonBlank(b.lines[b.row])
}

func (b *Buffer) paste(after bool, n int) {
	if b.clip != nil { // text that was copied somewhere else wins over the register
		if text, outside := b.clip.get(); outside {
			b.register, b.registerLine = clipboardRegister(text)
			b.clip.mu.Lock()
			b.clip.last = text
			b.clip.mu.Unlock()
		}
	}
	b.pasteRegister(after, n)
}

// pasteRegister puts the register contents into the text.
func (b *Buffer) pasteRegister(after bool, n int) {
	if len(b.register) == 0 {
		return
	}
	b.save()
	b.noLines = false
	owner := b.origin[b.row]
	if b.registerLine {
		index := b.row
		if after {
			index++
		}
		var add [][]rune
		var owners []int
		for i := 0; i < n; i++ {
			for _, line := range b.register {
				add = append(add, []rune(line))
				owners = append(owners, owner)
			}
		}
		b.lines = append(b.lines[:index], append(add, b.lines[index:]...)...)
		b.origin = append(b.origin[:index], append(owners, b.origin[index:]...)...)
		b.row = index
		b.col = firstNonBlank(b.lines[b.row])
		return
	}
	col := b.col
	if after && len(b.lines[b.row]) > 0 {
		col++
	}
	text := strings.Repeat(strings.Join(b.register, "\n"), n)
	b.insertText(col, text)
}

// insertText inserts text (which may hold line breaks) at col of the cursor line; the cursor ends on its last character,
// or at its start when the text has several lines.
func (b *Buffer) insertText(col int, text string) {
	parts := strings.Split(text, "\n")
	line := b.lines[b.row]
	head, tail := append([]rune(nil), line[:col]...), append([]rune(nil), line[col:]...)
	owner := b.origin[b.row]
	if len(parts) == 1 {
		b.lines[b.row] = append(append(head, []rune(parts[0])...), tail...)
		b.col = col + len([]rune(parts[0])) - 1
		b.clampCursor()
		return
	}
	newLines := [][]rune{append(head, []rune(parts[0])...)}
	owners := []int{owner}
	for _, part := range parts[1 : len(parts)-1] {
		newLines = append(newLines, []rune(part))
		owners = append(owners, owner)
	}
	newLines = append(newLines, append([]rune(parts[len(parts)-1]), tail...))
	owners = append(owners, owner)
	b.lines = append(b.lines[:b.row], append(newLines, b.lines[b.row+1:]...)...)
	b.origin = append(b.origin[:b.row], append(owners, b.origin[b.row+1:]...)...)
	b.col = col
	b.clampCursor()
}

func (b *Buffer) joinLines(count int) {
	if b.row >= len(b.lines)-1 {
		return
	}
	b.save()
	for i := 0; i < count && b.row < len(b.lines)-1; i++ {
		current, next := b.lines[b.row], []rune(strings.TrimLeftFunc(string(b.lines[b.row+1]), unicode.IsSpace))
		joinAt := len(current)
		joined := append([]rune(nil), current...)
		if len(next) > 0 && len(current) > 0 && current[len(current)-1] != ' ' {
			joined = append(joined, ' ')
		}
		joined = append(joined, next...)
		b.lines[b.row] = joined
		b.lines = append(b.lines[:b.row+1], b.lines[b.row+2:]...)
		b.origin = append(b.origin[:b.row+1], b.origin[b.row+2:]...)
		b.col = joinAt
	}
	b.clampCursor()
}

func (b *Buffer) replaceChars(r rune, n int) {
	line := b.lines[b.row]
	if b.col+n > len(line) {
		return
	}
	b.save()
	b.lines[b.row] = append([]rune(nil), line...)
	for i := 0; i < n; i++ {
		b.lines[b.row][b.col+i] = r
	}
	b.col += n - 1
}

func (b *Buffer) toggleCase(n int) {
	line := b.lines[b.row]
	if b.col >= len(line) {
		return
	}
	b.save()
	b.lines[b.row] = append([]rune(nil), line...)
	for i := 0; i < n && b.col < len(line); i++ {
		r := line[b.col]
		switch {
		case unicode.IsUpper(r):
			r = unicode.ToLower(r)
		case unicode.IsLower(r):
			r = unicode.ToUpper(r)
		}
		b.lines[b.row][b.col] = r
		b.col++
	}
	b.clampCursor()
}

// ReplaceLines replaces the whole text as one change (one undo step), keeping the cursor on the same line and column
// as far as the new text allows.
func (b *Buffer) ReplaceLines(lines []string) {
	b.save()
	owner := 0
	if len(b.origin) > 0 {
		owner = b.origin[0]
	}
	b.lines = make([][]rune, len(lines))
	b.origin = make([]int, len(lines))
	for i, line := range lines {
		b.lines[i] = []rune(line)
		b.origin[i] = owner
	}
	b.noLines = false
	if len(b.lines) == 0 {
		b.lines, b.origin, b.noLines = [][]rune{{}}, []int{owner}, true
	}
	b.mode = Normal
	b.resetPending()
	b.clampCursor()
}

// ReplaceInLine replaces the runes [from, to) of a line with text, which may contain line breaks, and puts the cursor
// behind the new text. It is meant for insert mode (completion): it does not start a new undo step, so it is undone
// together with the rest of the insert.
func (b *Buffer) ReplaceInLine(row, from, to int, text string) {
	if row < 0 || row >= len(b.lines) {
		return
	}
	line := b.lines[row]
	from = min(max(from, 0), len(line))
	to = min(max(to, from), len(line))
	head := append([]rune(nil), line[:from]...)
	tail := append([]rune(nil), line[to:]...)
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	owner := b.origin[row]
	newLines := make([][]rune, 0, len(parts))
	owners := make([]int, 0, len(parts))
	for i, part := range parts {
		runes := []rune(part)
		if i == 0 {
			runes = append(head, runes...)
		}
		if i == len(parts)-1 {
			b.row, b.col = row+i, len(runes)
			runes = append(runes, tail...)
		}
		newLines = append(newLines, runes)
		owners = append(owners, owner)
	}
	b.lines = append(b.lines[:row], append(newLines, b.lines[row+1:]...)...)
	b.origin = append(b.origin[:row], append(owners, b.origin[row+1:]...)...)
	b.noLines = false
	b.modified = true
	b.revision++
}
