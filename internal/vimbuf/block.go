package vimbuf

// blockInsert is the text typed into a visual block: what is typed in the first row is repeated in the other rows when insert mode ends.
type blockInsert struct {
	first, last int  // the rows of the block
	col         int  // the column the text goes to
	length      int  // length of the first row when typing began
	pad         bool // short rows are padded with spaces to reach col (A); else they are left out
}

// blockOperator applies d, c or y to the columns from..to (not including to) of the rows r1..r2.
func (b *Buffer) blockOperator(op rune, r1, r2, from, to int) {
	text := make([]string, 0, r2-r1+1)
	for row := r1; row <= r2; row++ {
		line := b.lines[row]
		text = append(text, string(line[min(from, len(line)):min(to, len(line))]))
	}
	b.setRegister(text, false)
	if op == 'y' {
		b.row, b.col = r1, from
		b.clampCursor()
		return
	}
	b.save()
	for row := r1; row <= r2; row++ {
		line := b.lines[row]
		lo, hi := min(from, len(line)), min(to, len(line))
		b.lines[row] = append(append([]rune(nil), line[:lo]...), line[hi:]...)
	}
	b.row, b.col = r1, from
	if op == 'c' {
		b.block = &blockInsert{first: r1, last: r2, col: from, length: len(b.lines[r1])}
		b.mode = Insert
		b.noLines = false
		return
	}
	b.clampCursor()
}

// startBlockInsert begins typing at the left (I) or right (A) edge of the block.
func (b *Buffer) startBlockInsert(atEnd bool) {
	r1, r2 := min(b.anchorRow, b.row), max(b.anchorRow, b.row)
	col := min(b.anchorCol, b.col)
	if atEnd {
		col = max(b.anchorCol, b.col) + 1
	}
	col = min(col, len(b.lines[r1]))
	b.save()
	b.block = &blockInsert{first: r1, last: r2, col: col, length: len(b.lines[r1]), pad: atEnd}
	b.mode, b.row, b.col = Insert, r1, col
}

// finishBlockInsert repeats what was typed in the first row of a block in the other rows; it runs when insert mode ends.
func (b *Buffer) finishBlockInsert() {
	block := b.block
	b.block = nil
	if block == nil || b.row != block.first || len(b.lines[block.first]) <= block.length {
		return // nothing typed, or the cursor left the row (enter, up, down): no repeat
	}
	line := b.lines[block.first]
	typed := line[block.col : block.col+len(line)-block.length]
	for row := block.first + 1; row <= block.last; row++ {
		text := b.lines[row]
		if len(text) < block.col {
			if !block.pad {
				continue
			}
			text = append(append([]rune(nil), text...), spaces(block.col-len(text))...)
		}
		b.lines[row] = append(append(append([]rune(nil), text[:block.col]...), typed...), text[block.col:]...)
	}
	b.revision++
	b.col = block.col + len(typed)
}

func spaces(n int) []rune {
	out := make([]rune, n)
	for i := range out {
		out[i] = ' '
	}
	return out
}
