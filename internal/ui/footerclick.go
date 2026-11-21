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
}

// add appends text that is not clickable.
func (b *bar) add(parts ...barPart) {
	for _, part := range parts {
		b.parts = append(b.parts, part)
		b.width += runewidth.StringWidth(part.text)
	}
}

// click appends text that sends keys when clicked; nil keys (a disabled command) make it inert.
func (b *bar) click(keys []string, parts ...barPart) {
	from := b.width
	b.add(parts...)
	if len(keys) > 0 {
		b.regions = append(b.regions, region{from: from, to: b.width, keys: keys})
	}
}

// command appends "[key] label  " in the usual look: the key bracketed and highlighted, the label plain; a
// disabled command is faint and inert. The clickable part ends before the two spaces that separate commands.
func (b *bar) command(key, label string, enabled bool, theme Theme) {
	keyStyle, labelStyle := theme.Header, theme.Footer
	if !enabled {
		keyStyle, labelStyle = theme.Footer.Faint(true), theme.Footer.Faint(true)
	}
	var keys []string
	if enabled {
		keys = commandKeys(key)
	}
	b.click(keys, barPart{"[" + key + "]", keyStyle}, barPart{" " + label, labelStyle})
	b.add(barPart{"  ", labelStyle})
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
	return renderSegments(b.parts, width, fill)
}

// visibleKeysAt is keysAt for a bar that is cut to width columns: columns beyond it are not clickable.
func (b *bar) visibleKeysAt(x, width int) []string {
	if x < 0 || x >= width {
		return nil
	}
	return b.keysAt(x)
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
		"ctrl+r": tea.KeyCtrlR, "ctrl+u": tea.KeyCtrlU, "ctrl+k": tea.KeyCtrlK,
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
