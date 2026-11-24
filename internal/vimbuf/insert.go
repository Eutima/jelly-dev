package vimbuf

import (
	"strconv"
	"strings"
)

func (b *Buffer) handleInsert(key string) {
	line := b.lines[b.row]
	switch key {
	case "esc", "ctrl+c", "left", "right", "up", "down", "home", "end":
	default:
		b.revision++ // everything else may change the text
	}
	switch key {
	case "esc", "ctrl+c":
		b.finishBlockInsert()
		b.mode = Normal
		b.col = max(b.col-1, 0)
		b.clampCursor()
	case "enter", "\n", "\r":
		head := append([]rune(nil), line[:b.col]...)
		tail := append([]rune(nil), line[b.col:]...)
		indent := []rune(nil)
		if !b.rawNewline {
			indent = leadingWhite(head)
			if len(tail) == 0 && len(indent) == len(head) { // a line of nothing but its indent is emptied, like vim does
				head = nil
			}
		}
		b.lines[b.row] = head
		owner := b.origin[b.row]
		b.lines = append(b.lines[:b.row+1], append([][]rune{append(append([]rune(nil), indent...), tail...)}, b.lines[b.row+1:]...)...)
		b.origin = append(b.origin[:b.row+1], append([]int{owner}, b.origin[b.row+1:]...)...)
		b.row, b.col = b.row+1, len(indent)
		b.noLines = false
	case "backspace", "ctrl+h":
		switch {
		case b.col > 0:
			b.lines[b.row] = append(append([]rune(nil), line[:b.col-1]...), line[b.col:]...)
			b.col--
		case b.row > 0:
			previous := b.lines[b.row-1]
			b.col = len(previous)
			b.lines[b.row-1] = append(append([]rune(nil), previous...), line...)
			b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
			b.origin = append(b.origin[:b.row], b.origin[b.row+1:]...)
			b.row--
		}
	case "delete", "ctrl+d":
		switch {
		case b.col < len(line):
			b.lines[b.row] = append(append([]rune(nil), line[:b.col]...), line[b.col+1:]...)
		case b.row < len(b.lines)-1:
			b.lines[b.row] = append(append([]rune(nil), line...), b.lines[b.row+1]...)
			b.lines = append(b.lines[:b.row+1], b.lines[b.row+2:]...)
			b.origin = append(b.origin[:b.row+1], b.origin[b.row+2:]...)
		}
	case "left":
		b.col = max(b.col-1, 0)
	case "right":
		b.col = min(b.col+1, len(line))
	case "up":
		b.row = max(b.row-1, 0)
		b.col = min(b.col, len(b.lines[b.row]))
	case "down":
		b.row = min(b.row+1, len(b.lines)-1)
		b.col = min(b.col, len(b.lines[b.row]))
	case "home":
		b.col = 0
	case "end":
		b.col = len(line)
	case "tab":
		b.typeRunes(b.tabText())
	case "\t": // a tab that is text (pasted) stays a tab
		b.typeRunes([]rune{'\t'})
	case "ctrl+u": // delete everything before the cursor
		b.lines[b.row] = append([]rune(nil), line[b.col:]...)
		b.col = 0
	case "ctrl+k": // delete everything from the cursor on
		b.lines[b.row] = append([]rune(nil), line[:b.col]...)
	default:
		if r := []rune(key); len(r) == 1 && (r[0] >= ' ') {
			b.typeRunes(r)
		}
	}
}

// SetIndent sets what the tab key types in insert mode: "\t" for a tab, or spaces (the width of one level of indent). A tab pasted as
// text always stays a tab.
func (b *Buffer) SetIndent(unit string) {
	if unit == "" {
		unit = "\t"
	}
	b.indentUnit = []rune(unit)
}

// tabText is what the tab key types at the cursor: a tab, or the spaces up to the next multiple of the width of an indent level.
func (b *Buffer) tabText() []rune {
	if len(b.indentUnit) == 0 || b.indentUnit[0] == '\t' {
		return []rune{'\t'}
	}
	width := len(b.indentUnit)
	return []rune(strings.Repeat(" ", width-b.col%width))
}

// leadingWhite is the spaces and tabs a line starts with.
func leadingWhite(line []rune) []rune {
	end := 0
	for end < len(line) && (line[end] == ' ' || line[end] == '\t') {
		end++
	}
	return append([]rune(nil), line[:end]...)
}

func (b *Buffer) typeRunes(runes []rune) {
	line := b.lines[b.row]
	text := make([]rune, 0, len(line)+len(runes))
	text = append(text, line[:b.col]...)
	text = append(text, runes...)
	text = append(text, line[b.col:]...)
	b.lines[b.row] = text
	b.col += len(runes)
	b.noLines = false
}

func (b *Buffer) handleCommand(key string) Result {
	switch key {
	case "esc", "ctrl+c":
		b.mode, b.cmd = Normal, ""
	case "backspace":
		if b.cmd == "" {
			b.mode = Normal
			break
		}
		r := []rune(b.cmd)
		b.cmd = string(r[:len(r)-1])
	case "enter":
		text, prefix := b.cmd, b.cmdPrefix
		b.mode, b.cmd, b.cmdPrefix = Normal, "", 0
		b.clampCursor()
		if prefix == '/' || prefix == '?' {
			b.finishSearch(prefix, text)
			return None
		}
		return b.execute(strings.TrimSpace(text))
	default:
		if r := []rune(key); len(r) == 1 && r[0] >= ' ' {
			b.cmd += key
		}
	}
	return None
}

// execute runs a command typed after ":".
func (b *Buffer) execute(cmd string) Result {
	switch cmd {
	case "":
		return None
	case "w":
		return Write
	case "wq", "x":
		return WriteQuit
	case "q":
		if b.modified {
			b.message = "E37: No write since last change (add ! to override)"
			return None
		}
		return Quit
	case "q!":
		return ForceQuit
	case "comment":
		b.ToggleComment()
		return None
	case "noh", "nohlsearch":
		b.ClearHighlight()
		return None
	}
	if line, err := strconv.Atoi(cmd); err == nil {
		b.GotoLine(line)
		return None
	}
	if b.passEx {
		b.ex = cmd
		return Ex
	}
	b.message = "E492: Not an editor command: " + cmd
	return None
}
