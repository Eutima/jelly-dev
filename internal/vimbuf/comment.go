package vimbuf

import "unicode"

// commentOp is the pending operator of gc, in the place of d, c or y.
const commentOp = 'Ç'

// SetCommentPrefix sets the line comment marker gc uses ("//", "#", ...). Without one gc only reports that the file type has none.
func (b *Buffer) SetCommentPrefix(prefix string) { b.commentPrefix = prefix }

// ToggleComment comments the cursor line, or the selected lines in visual mode, out or in (the command :comment).
func (b *Buffer) ToggleComment() {
	b.message = ""
	r1, r2 := b.row, b.row
	if b.inVisual() {
		r1, r2 = min(b.anchorRow, b.row), max(b.anchorRow, b.row)
		b.mode = Normal
	}
	b.toggleComment(r1, r2)
}

// toggleComment comments the lines r1..r2 out, or in again when every line that has text is commented already. The marker is
// written at the smallest indent of the lines, so the comment lines up; blank lines are left alone. It is one undo step.
func (b *Buffer) toggleComment(r1, r2 int) {
	if b.commentPrefix == "" {
		b.message = "E: no line comment for this file type"
		return
	}
	marker := []rune(b.commentPrefix)
	all, any, indent := true, false, -1
	for row := r1; row <= r2; row++ {
		line := b.lines[row]
		at := indentOf(line)
		if at == len(line) {
			continue
		}
		any = true
		if indent < 0 || at < indent {
			indent = at
		}
		if !hasMarkerAt(line, at, marker) {
			all = false
		}
	}
	if !any {
		return
	}
	b.save()
	for row := r1; row <= r2; row++ {
		line := b.lines[row]
		at := indentOf(line)
		if at == len(line) {
			continue
		}
		if all {
			end := at + len(marker)
			if end < len(line) && line[end] == ' ' {
				end++
			}
			b.lines[row] = append(append([]rune(nil), line[:at]...), line[end:]...)
			continue
		}
		text := append(append(append([]rune(nil), line[:indent]...), marker...), ' ')
		b.lines[row] = append(text, line[indent:]...)
	}
	b.row = r1
	b.col = firstNonBlank(b.lines[r1])
	b.clampCursor()
}

// indentOf is the number of white space characters a line starts with.
func indentOf(line []rune) int {
	for i, r := range line {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return len(line)
}

func hasMarkerAt(line []rune, at int, marker []rune) bool {
	if at+len(marker) > len(line) {
		return false
	}
	for i, r := range marker {
		if line[at+i] != r {
			return false
		}
	}
	return true
}
