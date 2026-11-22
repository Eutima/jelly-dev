package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/eutima/jelly-dev/internal/keymap"
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
	var clicks []string
	if enabled {
		clicks = commandKeys(key)
	}
	b.hintWith(key, clicks, label, enabled, theme)
}

// binding appends the hint of a command whose shortcut can be changed in .jdrc: it shows the key the command has now, and a click sends
// that key. A command without a key (unbound) has no hint.
func (b *bar) binding(command keymap.Command, label string, enabled bool, theme Theme) {
	key := keys.Key(command)
	if key == "" {
		return
	}
	var clicks []string
	if enabled {
		clicks = []string{key}
	}
	b.hintWith(keymap.Pretty(key), clicks, label, enabled, theme)
}

// stepHint appends the hint for the next and previous change: "↑↓" by default, "ctrl+k/ctrl+j" for longer names. A click goes to the next
// change. Nothing is shown when the next-change command has no key.
func (b *bar) stepHint(label string, theme Theme) {
	next, prev := keys.Key(keymap.DiffNext), keys.Key(keymap.DiffPrev)
	if next == "" {
		return
	}
	shown := keymap.Pretty(next)
	if prev != "" {
		separator := ""
		if len([]rune(keymap.Pretty(prev))) > 1 || len([]rune(keymap.Pretty(next))) > 1 {
			separator = "/"
		}
		shown = keymap.Pretty(prev) + separator + keymap.Pretty(next)
	}
	b.hintWith(shown, []string{next}, label, true, theme)
}

// hintWith is command with the keys a click sends given.
func (b *bar) hintWith(key string, clicks []string, label string, enabled bool, theme Theme) {
	keyStyle, labelStyle := theme.Header, theme.Footer
	if !enabled {
		// a command that cannot be used now is dim on purpose; every usable one keeps full white text
		keyStyle, labelStyle = theme.Footer.Faint(true), theme.Footer.Faint(true)
	}
	full := []barPart{{"[" + key + "]", keyStyle}, {" " + label, labelStyle}}
	tail := []barPart{{"  ", labelStyle}}
	b.ops = append(b.ops, barOp{parts: full, short: full[:1], tail: tail, keys: clicks, collapsible: true})
	b.place(clicks, full...)
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

// wrap lays the bar out on up to maxLines lines of width columns: a command that does not fit on the line goes on the next one, so
// nothing is cut as long as it fits. When even that needs too many lines, the last commands lose their labels and then drop out,
// the last one first. A bar that was not built from commands is a single line.
func (b *bar) wrap(width, maxLines int) []*bar {
	if width <= 0 || len(b.ops) == 0 || maxLines < 1 {
		return []*bar{b}
	}
	state := make([]int, len(b.ops)) // 0 full, 1 short, 2 dropped
	for {
		lines := b.layout(state, width)
		if len(lines) <= maxLines {
			return lines
		}
		if !b.degrade(state) {
			return lines[:maxLines]
		}
	}
}

// degrade takes one step towards fewer lines: the last command that still has its label loses it, when there is none the last
// one that is still shown is dropped. It returns false when nothing is left to give up.
func (b *bar) degrade(state []int) bool {
	for i := len(b.ops) - 1; i >= 0; i-- {
		if b.ops[i].collapsible && state[i] == 0 {
			state[i] = 1
			if b.ops[i].short == nil {
				state[i] = 2
			}
			return true
		}
	}
	for i := len(b.ops) - 1; i >= 0; i-- {
		if b.ops[i].collapsible && state[i] == 1 {
			state[i] = 2
			return true
		}
	}
	return false
}

// layout puts the operations on lines of width columns, a collapsible one on a new line when it does not fit on the current one.
func (b *bar) layout(state []int, width int) []*bar {
	cur := &bar{}
	lines := []*bar{cur}
	for i, op := range b.ops {
		parts := op.parts
		switch state[i] {
		case 2:
			continue
		case 1:
			parts = op.short
		}
		// the spaces after a command are not needed at the end of a line
		if op.collapsible && cur.width > 0 && cur.width+segmentsWidth(parts) > width {
			cur = &bar{}
			lines = append(lines, cur)
			cur.append(barPart{" ", parts[0].style})
		}
		if len(op.keys) > 0 {
			cur.place(op.keys, parts...)
		} else {
			cur.append(parts...)
		}
		cur.append(op.tail...)
	}
	return lines
}

// renderLines draws the wrapped bar in exactly width columns per line.
func (b *bar) renderLines(width, maxLines int, fill lipgloss.Style) []string {
	lines := b.wrap(width, maxLines)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = renderSegments(line.parts, width, fill)
	}
	return out
}

// keysAtLine returns the keys of the clickable region under column x of line y of the wrapped bar, nil when there is none.
func (b *bar) keysAtLine(x, y, width, maxLines int) []string {
	lines := b.wrap(width, maxLines)
	if y < 0 || y >= len(lines) || x < 0 || x >= width {
		return nil
	}
	return lines[y].keysAt(x)
}

// rowKeysAt returns the keys of the clickable region at (x, y) of wrapped footer rows that fill the last lines of a view of
// the given size, nil when the position is outside them or on no command.
func rowKeysAt(rows []*bar, x, y, width, height int) []string {
	first := height - len(rows)
	if y < first || y >= height || x < 0 || x >= width {
		return nil
	}
	return rows[y-first].keysAt(x)
}

// padRows adds empty rows until there are at least n.
func padRows(rows []*bar, n int) []*bar {
	for len(rows) < n {
		rows = append(rows, &bar{})
	}
	return rows
}

// renderRowLines draws wrapped footer rows in exactly width columns per row.
func renderRowLines(rows []*bar, width int, fill lipgloss.Style) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = renderSegments(row.parts, width, fill)
	}
	return out
}

// renderRows is renderRowLines joined by newlines.
func renderRows(rows []*bar, width int, fill lipgloss.Style) string {
	return strings.Join(renderRowLines(rows, width, fill), "\n")
}

// textRows wraps a message at the spaces into at most maxLines bars (each starts with a space); the bars have no clickable parts.
func textRows(text string, width, maxLines int, style lipgloss.Style) []*bar {
	lines := wrapWords(text, max(width, 1)-1, maxLines) // one column for the space that starts a line
	rows := make([]*bar, len(lines))
	for i, line := range lines {
		rows[i] = &bar{}
		rows[i].add(barPart{" " + line, style})
	}
	return rows
}

// messageLines is textRows drawn in exactly width columns per line.
func messageLines(text string, width, maxLines int, style lipgloss.Style) []string {
	return renderRowLines(textRows(text, width, maxLines, style), max(width, 1), style)
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

// keyTypes are the keys with a name of their own, as tea.KeyMsg.String reports them.
var keyTypes = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete, "insert": tea.KeyInsert,
	"tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab, "left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"ctrl+left": tea.KeyCtrlLeft, "ctrl+right": tea.KeyCtrlRight, "ctrl+up": tea.KeyCtrlUp, "ctrl+down": tea.KeyCtrlDown,
	"shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight, "shift+up": tea.KeyShiftUp, "shift+down": tea.KeyShiftDown,
	"ctrl+home": tea.KeyCtrlHome, "ctrl+end": tea.KeyCtrlEnd, "ctrl+pgup": tea.KeyCtrlPgUp, "ctrl+pgdown": tea.KeyCtrlPgDown,
	"ctrl+@": tea.KeyCtrlAt, "ctrl+]": tea.KeyCtrlCloseBracket, "ctrl+\\": tea.KeyCtrlBackslash, "ctrl+^": tea.KeyCtrlCaret, "ctrl+_": tea.KeyCtrlUnderscore,
	"f1": tea.KeyF1, "f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4, "f5": tea.KeyF5, "f6": tea.KeyF6, "f7": tea.KeyF7, "f8": tea.KeyF8,
	"f9": tea.KeyF9, "f10": tea.KeyF10, "f11": tea.KeyF11, "f12": tea.KeyF12, "f13": tea.KeyF13, "f14": tea.KeyF14, "f15": tea.KeyF15,
	"f16": tea.KeyF16, "f17": tea.KeyF17, "f18": tea.KeyF18, "f19": tea.KeyF19, "f20": tea.KeyF20,
}

// keyMsg turns a key name (as tea.KeyMsg.String reports it) back into a message.
func keyMsg(name string) tea.KeyMsg {
	alt := false
	if rest, ok := strings.CutPrefix(name, "alt+"); ok && rest != "" {
		name, alt = rest, true
	}
	keyType, named := keyTypes[name]
	switch {
	case named:
		return tea.KeyMsg{Type: keyType, Alt: alt}
	case len(name) == len("ctrl+")+1 && strings.HasPrefix(name, "ctrl+") && name[len("ctrl+")] >= 'a' && name[len("ctrl+")] <= 'z':
		return tea.KeyMsg{Type: tea.KeyType(1 + name[len("ctrl+")] - 'a'), Alt: alt} // ctrl+a is 1 ... ctrl+z is 26
	case name == " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Alt: alt}
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
