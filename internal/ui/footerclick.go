package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// region is a clickable stretch of a bar: the columns [from, to) and the keys a click sends.
type region struct {
	from, to int
	keys     []string
}

// bar is a status line under construction: the styled text plus the regions that react to a click.
type bar struct {
	parts   []barPart
	regions []region
	width   int
	ops     []barOp // what was added, in order: lets the bar shrink to a narrower terminal (see fitted)
}

// barOp is one step of building a bar. A collapsible one has a short form (nil: none) that replaces it when the bar does not
// fit, and can be dropped altogether after that; tail is text that follows it and goes with it.
type barOp struct {
	parts, short, tail []barPart
	keys               []string
	collapsible        bool
}

// add appends text that is not clickable.
func (b *bar) add(parts ...barPart) {
	b.ops = append(b.ops, barOp{parts: parts})
	b.append(parts...)
}

func (b *bar) append(parts ...barPart) {
	for _, part := range parts {
		b.parts = append(b.parts, part)
		b.width += runewidth.StringWidth(part.text)
	}
}

// click appends text that sends keys when clicked; nil keys (a disabled command) make it inert.
func (b *bar) click(keys []string, parts ...barPart) {
	b.ops = append(b.ops, barOp{parts: parts, keys: keys})
	b.place(keys, parts...)
}

// place appends parts and makes them clickable (when there are keys).
func (b *bar) place(keys []string, parts ...barPart) {
	from := b.width
	b.append(parts...)
	if len(keys) > 0 {
		b.regions = append(b.regions, region{from: from, to: b.width, keys: keys})
	}
}

// command appends "[key] label  " in the usual look: the key bracketed and highlighted, the label plain; a
// disabled command is faint and inert. The clickable part ends before the two spaces that separate commands.
func (b *bar) command(key, label string, enabled bool, theme Theme) {
	keyStyle, labelStyle := theme.Header, theme.Footer
	if !enabled {
		// a command that cannot be used now is dim on purpose; every usable one keeps full white text
		keyStyle, labelStyle = theme.Footer.Faint(true), theme.Footer.Faint(true)
	}
	var keys []string
	if enabled {
		keys = commandKeys(key)
	}
	full := []barPart{{"[" + key + "]", keyStyle}, {" " + label, labelStyle}}
	tail := []barPart{{"  ", labelStyle}}
	b.ops = append(b.ops, barOp{parts: full, short: full[:1], tail: tail, keys: keys, collapsible: true})
	b.place(keys, full...)
	b.append(tail...)
}

// button appends a clickable part that gives up its hint when the bar is too narrow (short), and the whole button after that.
func (b *bar) button(keys []string, full, short []barPart) {
	b.ops = append(b.ops, barOp{parts: full, short: short, keys: keys, collapsible: true})
	b.place(keys, full...)
}

// hint appends text that is the first to go when the bar is too narrow.
func (b *bar) hint(parts ...barPart) {
	b.ops = append(b.ops, barOp{parts: parts, collapsible: true})
	b.append(parts...)
}

// fitted returns the bar for a terminal width columns wide: when it is too wide the commands, from the last to the first,
// lose their labels (only the key stays), and when that is not enough they drop out, again the last one first.
func (b *bar) fitted(width int) *bar {
	if width <= 0 || b.width <= width || len(b.ops) == 0 {
		return b
	}
	state := make([]int, len(b.ops)) // 0 full, 1 short, 2 dropped
	size := func() int {
		total := 0
		for i, op := range b.ops {
			switch state[i] {
			case 0:
				total += segmentsWidth(op.parts) + segmentsWidth(op.tail)
			case 1:
				total += segmentsWidth(op.short) + segmentsWidth(op.tail)
			}
		}
		return total
	}
	for target := 1; target <= 2 && size() > width; target++ {
		for i := len(b.ops) - 1; i >= 0 && size() > width; i-- {
			if !b.ops[i].collapsible || state[i] != target-1 {
				continue
			}
			state[i] = target
			if target == 1 && b.ops[i].short == nil {
				state[i] = 2
			}
		}
	}
	out := &bar{}
	for i, op := range b.ops {
		parts := op.parts
		switch state[i] {
		case 2:
			continue
		case 1:
			parts = op.short
		}
		if len(op.keys) > 0 {
			out.place(op.keys, parts...)
		} else {
			out.append(parts...)
		}
		out.append(op.tail...)
	}
	return out
}

// keysAt returns the keys of the clickable region under column x, nil when there is none.
func (b *bar) keysAt(x int) []string {
	for _, r := range b.regions {
		if x >= r.from && x < r.to {
			return r.keys
		}
	}
	return nil
}

// render draws the bar in exactly width columns.
func (b *bar) render(width int, fill lipgloss.Style) string {
	return renderSegments(b.fitted(width).parts, width, fill)
}

// visibleKeysAt is keysAt for a bar that is cut to width columns: columns beyond it are not clickable.
func (b *bar) visibleKeysAt(x, width int) []string {
	if x < 0 || x >= width {
		return nil
	}
	return b.fitted(width).keysAt(x)
}

// commandKeys maps the text shown in brackets to the keys a click on it sends.
func commandKeys(shown string) []string {
	switch shown {
	case "→", "→/o":
		return []string{"right"}
	case "←", "←/t":
		return []string{"left"}
	case "⌫":
		return []string{"backspace"}
	case "↑↓":
		return []string{"down"}
	case "n/p":
		return []string{"n"}
	case "j/k":
		return []string{"j"}
	case "v/V":
		return []string{"v"}
	case "o/t/r":
		return []string{"o"}
	case "space":
		return []string{" "}
	case "gd":
		return []string{"g", "d"}
	case "gr":
		return []string{"g", "r"}
	}
	if strings.HasPrefix(shown, ":") { // an Ex command: type it and press enter
		keys := make([]string, 0, len(shown)+1)
		for _, r := range shown {
			keys = append(keys, string(r))
		}
		return append(keys, "enter")
	}
	return []string{shown}
}

// keyMsg turns a key name (as tea.KeyMsg.String reports it) back into a message.
func keyMsg(name string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"tab": tea.KeyTab, "left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"ctrl+c": tea.KeyCtrlC, "ctrl+d": tea.KeyCtrlD, "ctrl+o": tea.KeyCtrlO, "ctrl+t": tea.KeyCtrlT,
		"ctrl+r": tea.KeyCtrlR, "ctrl+u": tea.KeyCtrlU, "ctrl+k": tea.KeyCtrlK, "ctrl+p": tea.KeyCtrlP, "ctrl+g": tea.KeyCtrlG,
		"ctrl+n": tea.KeyCtrlN, "ctrl+f": tea.KeyCtrlF, "ctrl+@": tea.KeyCtrlAt,
	}
	if keyType, ok := named[name]; ok {
		return tea.KeyMsg{Type: keyType}
	}
	if name == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// leftPress reports whether msg is the press of the left mouse button.
func leftPress(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
}

// replay sends keys to a model one after the other and collects the commands they return.
func replay(model tea.Model, keys []string) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, key := range keys {
		var cmd tea.Cmd
		model, cmd = model.Update(keyMsg(key))
		cmds = append(cmds, cmd)
	}
	return model, tea.Batch(cmds...)
}
