package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

const (
	// JellyInterval is the time between two animation frames.
	JellyInterval = 110 * time.Millisecond
	// jellyHeight is the number of rows of every jelly frame, so the layout never jumps.
	jellyHeight = 4
	// minJellyBody is the smallest pane body (rows) in which the jelly is drawn besides the text.
	minJellyBody = 8
)

// jellyTickMsg advances the wobble animation.
type jellyTickMsg struct{}

// jellyTick schedules the next animation frame.
func jellyTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return jellyTickMsg{} })
}

// jellyFrames is the wobble: the jelly leans, stretches and squashes on its plate.
func jellyFrames() [][jellyHeight]string {
	return [][jellyHeight]string{
		{"   ▄▆▆▆▆▄   ", "  ▐██████▌  ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // rest
		{"  ▄▆▆▆▆▄    ", " ▐██████▌   ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // lean left
		{"   ▄▆▆▆▆▄   ", "  ▐██████▌  ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // rest
		{"    ▄▆▆▆▆▄  ", "   ▐██████▌ ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // lean right
		{"    ▄▄▄▄    ", "   ▐████▌   ", "  ▟██████▙  ", " ▀▀▀▀▀▀▀▀▀▀ "}, // stretched
		{"   ▄▆▆▆▆▄   ", "  ▐██████▌  ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // rest
		{"            ", " ▄▆██████▆▄ ", "▟██████████▙", "▀▀▀▀▀▀▀▀▀▀▀▀"}, // squashed
		{"   ▄▆▆▆▆▄   ", "  ▐██████▌  ", " ▟████████▙ ", " ▀▀▀▀▀▀▀▀▀▀ "}, // rest
	}
}

// JellyFrame returns the rows of animation frame i (the animation repeats).
func JellyFrame(i int) []string {
	frames := jellyFrames()
	frame := frames[((i%len(frames))+len(frames))%len(frames)]
	return frame[:]
}

// JellyLine returns a one-line jelly wave for places without room for the full animation.
func JellyLine(i int) string {
	const wave = "▁▃▅▇▅▃▁"
	runes := []rune(wave)
	var line strings.Builder
	for column := 0; column < len(runes); column++ {
		line.WriteRune(runes[((column+i)%len(runes)+len(runes))%len(runes)])
	}
	return line.String()
}

// centered pads text with spaces so it is centered in width columns.
func centered(text string, width int) string {
	gap := width - runewidth.StringWidth(text)
	if gap <= 0 {
		return text
	}
	return strings.Repeat(" ", gap/2) + text
}
