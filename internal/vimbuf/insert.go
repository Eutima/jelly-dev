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
		b.mode = Normal
		b.col = max(b.col-1, 0)
		b.clampCursor()
	case "enter", "\n", "\r":
		tail := append([]rune(nil), line[b.col:]...)
		b.lines[b.row] = append([]rune(nil), line[:b.col]...)
		owner := b.origin[b.row]
		b.lines = append(b.lines[:b.row+1], append([][]rune{tail}, b.lines[b.row+1:]...)...)
		b.origin = append(b.origin[:b.row+1], append([]int{owner}, b.origin[b.row+1:]...)...)
		b.row, b.col = b.row+1, 0
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
