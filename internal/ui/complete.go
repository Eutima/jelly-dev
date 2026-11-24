package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

const (
	maxCompletionRows  = 8
	maxCompletionItems = 200
	maxCompletionWidth = 56
)

// completionResultMsg is the answer of a completion request.
type completionResultMsg struct {
	row, col int // where the request was made
	items    []lsp.CompletionItem
	err      error
}

// completionState is the open completion popup.
type completionState struct {
	all    []lsp.CompletionItem
	items  []lsp.CompletionItem // the matches of what was typed since the popup opened, best first
	row    int
	start  int // rune column where the word being completed starts
	sel    int
	top    int
	source string // "language server", "words in the file"
	// labelWidth and naturalWidth are measured over all the matches once, so the popup does not change shape while the selection scrolls
	labelWidth, naturalWidth int
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordStart is the rune column where the identifier before col starts.
func wordStart(line []rune, col int) int {
	col = min(col, len(line))
	start := col
	for start > 0 && isWordRune(line[start-1]) {
		start--
	}
	return start
}

// isCompletionKey reports whether a key asks for completion: ctrl+space (terminals send it as ctrl+@) or ctrl+n.
func isCompletionKey(key string) bool {
	return key == "ctrl+@" || key == "ctrl+space" || key == "ctrl+n"
}

// startCompletion asks for suggestions at the cursor: the language server when there is one, always together with the
// words of the file.
func (m EditorModel) startCompletion() (EditorModel, tea.Cmd) {
	row, col := m.vim.Cursor()
	text := strings.Join(m.vim.Lines(), "\n")
	path, nav := m.doc.Path, m.nav
	m.notice("", false)
	return m, func() tea.Msg {
		var items []lsp.CompletionItem
		var err error
		if nav != nil && path != "" {
			ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
			defer cancel()
			items, err = nav.Complete(ctx, path, text, row, col)
		}
		return completionResultMsg{row: row, col: col, items: items, err: err}
	}
}

// handleCompletionResult opens the popup when the answer still fits the cursor.
func (m EditorModel) handleCompletionResult(msg completionResultMsg) EditorModel {
	row, col := m.vim.Cursor()
	if m.vim.Mode() != vimbuf.Insert || row != msg.row || col < msg.col {
		return m // the user moved on
	}
	line := []rune(m.vim.Line(row))
	state := &completionState{row: row, start: wordStart(line, msg.col), source: "language server"}
	state.all = msg.items
	if len(state.all) == 0 {
		state.all = bufferWords(m.vim.Lines(), string(line[state.start:min(col, len(line))]))
		state.source = "words in the file"
	}
	if msg.err != nil {
		var missing *lsp.NotInstalledError
		if !errors.As(msg.err, &missing) && !errors.Is(msg.err, lsp.ErrNoServer) {
			m.notice("Completion: "+msg.err.Error(), true)
		}
	}
	m.completion = state
	m.refilterCompletion()
	if m.completion == nil {
		m.notice("No completions", true)
	}
	return m
}

// refilterCompletion keeps the items that match what was typed since the word started; the popup closes when none do.
func (m *EditorModel) refilterCompletion() {
	state := m.completion
	row, col := m.vim.Cursor()
	line := []rune(m.vim.Line(row))
	if row != state.row || col < state.start || col > len(line) {
		m.completion = nil
		return
	}
	typed := string(line[state.start:col])
	for _, r := range typed {
		if !isWordRune(r) {
			m.completion = nil
			return
		}
	}
	state.items = rankCompletions(state.all, typed)
	state.sel, state.top = 0, 0
	state.labelWidth, state.naturalWidth = 0, 0
	for _, item := range state.items {
		extra := item.Detail
		if extra == "" {
			extra = lsp.KindName(item.Kind)
		}
		label := runewidth.StringWidth(sanitize(item.Label))
		state.labelWidth = max(state.labelWidth, label)
		state.naturalWidth = max(state.naturalWidth, 1+label+2+runewidth.StringWidth(sanitize(extra))+1)
	}
	if len(state.items) == 0 {
		m.completion = nil
	}
}

// rankCompletions keeps the items that match the typed text and orders them: prefix matches, then other matches, each in
// the order the server gave (sortText).
func rankCompletions(all []lsp.CompletionItem, typed string) []lsp.CompletionItem {
	type scored struct {
		item  lsp.CompletionItem
		score int
		order int
	}
	lower := strings.ToLower(typed)
	var kept []scored
	for i, item := range all {
		filter := item.FilterText
		if filter == "" {
			filter = item.Label
		}
		filter = strings.ToLower(filter)
		score := 0
		switch {
		case lower == "":
		case strings.HasPrefix(filter, lower):
			score = 3
		case strings.Contains(filter, lower):
			score = 2
		case isSubsequence(lower, filter):
			score = 1
		default:
			continue
		}
		if strings.EqualFold(item.Label, typed) && len(all) > 1 && lower != "" {
			score-- // the word as it already is: show the longer suggestions first
		}
		kept = append(kept, scored{item, score, i})
	}
	sort.SliceStable(kept, func(a, b int) bool {
		if kept[a].score != kept[b].score {
			return kept[a].score > kept[b].score
		}
		sortA, sortB := kept[a].item.SortText, kept[b].item.SortText
		if sortA != "" && sortB != "" && sortA != sortB {
			return sortA < sortB
		}
		return kept[a].order < kept[b].order
	})
	if len(kept) > maxCompletionItems {
		kept = kept[:maxCompletionItems]
	}
	out := make([]lsp.CompletionItem, len(kept))
	for i, k := range kept {
		out[i] = k.item
	}
	return out
}

func isSubsequence(needle, haystack string) bool {
	h := []rune(haystack)
	pos := 0
	for _, r := range needle {
		found := false
		for pos < len(h) {
			pos++
			if h[pos-1] == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// bufferWords returns the words of the text, except the one being typed, as completion items: the fallback without a
// language server.
func bufferWords(lines []string, typed string) []lsp.CompletionItem {
	seen := map[string]bool{typed: true}
	var words []string
	for _, line := range lines {
		start := -1
		runes := []rune(line + " ")
		for i, r := range runes {
			if isWordRune(r) {
				if start < 0 {
					start = i
				}
				continue
			}
			if start >= 0 {
				if word := string(runes[start:i]); len([]rune(word)) >= 2 && !seen[word] {
					seen[word] = true
					words = append(words, word)
				}
				start = -1
			}
		}
	}
	sort.Strings(words)
	items := make([]lsp.CompletionItem, len(words))
	for i, word := range words {
		items[i] = lsp.CompletionItem{Label: word}
	}
	return items
}

// moveCompletion selects another suggestion.
func (m *EditorModel) moveCompletion(step int) {
	state := m.completion
	state.sel = (state.sel + step + len(state.items)) % len(state.items)
	width := m.completionWidth(m.textWidth())
	state.top = scrollToShow(state.sel, state.top, maxCompletionRows, len(state.items), func(row int) int { return len(m.completionItemLines(row, width)) })
}

// acceptCompletion puts the selected suggestion into the text.
func (m *EditorModel) acceptCompletion() {
	state := m.completion
	m.completion = nil
	if state == nil || state.sel >= len(state.items) {
		return
	}
	item := state.items[state.sel]
	row, col := m.vim.Cursor()
	line := []rune(m.vim.Line(row))
	from, to, text := state.start, col, item.InsertText
	if text == "" {
		text = item.Label
	}
	if edit := item.Edit; edit != nil && edit.Range.Start.Line == row && edit.Range.End.Line == row {
		text = edit.NewText
		from = lsp.RuneColumn(string(line), edit.Range.Start.Character)
		to = max(lsp.RuneColumn(string(line), edit.Range.End.Character), col)
	}
	m.vim.ReplaceInLine(row, from, to, text)
	m.applyAdditionalEdits(item)
}

// completionResolver is implemented by a navigator that can fetch the rest of a completion item (the import line of an auto import).
type completionResolver interface {
	ResolveCompletion(ctx context.Context, path string, item lsp.CompletionItem) (lsp.CompletionItem, error)
}

// applyAdditionalEdits adds the edits that come with a chosen item, such as the import of an auto import, and keeps the cursor on the
// text that was just inserted. Only edits inside one line are applied (an import line is an insertion at the start of a line).
func (m *EditorModel) applyAdditionalEdits(item lsp.CompletionItem) {
	if resolver, ok := m.nav.(completionResolver); ok && len(item.Additional) == 0 && m.doc.Path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), navTimeout)
		defer cancel()
		if resolved, err := resolver.ResolveCompletion(ctx, m.doc.Path, item); err == nil {
			item = resolved
		}
	}
	edits := append([]lsp.TextEdit(nil), item.Additional...)
	// from the end of the file to the start, so an edit does not move the ones before it
	sort.SliceStable(edits, func(a, b int) bool {
		if edits[a].Range.Start.Line != edits[b].Range.Start.Line {
			return edits[a].Range.Start.Line > edits[b].Range.Start.Line
		}
		return edits[a].Range.Start.Character > edits[b].Range.Start.Character
	})
	cursorRow, cursorCol := m.vim.Cursor()
	for _, edit := range edits {
		row := edit.Range.Start.Line
		if row != edit.Range.End.Line || row < 0 || row >= len(m.vim.Lines()) {
			continue
		}
		line := m.vim.Line(row)
		from := lsp.RuneColumn(line, edit.Range.Start.Character)
		to := lsp.RuneColumn(line, edit.Range.End.Character)
		m.vim.ReplaceInLine(row, from, to, edit.NewText)
		if row <= cursorRow {
			cursorRow += strings.Count(strings.ReplaceAll(edit.NewText, "\r\n", "\n"), "\n")
		}
	}
	if len(edits) > 0 {
		m.vim.SetCursor(cursorRow, cursorCol)
	}
}

// completionKey handles the keys while the popup is open; it reports whether the key was used.
func (m EditorModel) completionKey(key string) (EditorModel, bool) {
	switch key {
	case "ctrl+n", "down", "ctrl+j":
		m.moveCompletion(1)
	case "ctrl+p", "up", "ctrl+k":
		m.moveCompletion(-1)
	case "tab", "enter":
		m.acceptCompletion()
	case "esc", "ctrl+c", "ctrl+d":
		m.completion = nil
	default:
		return m, false
	}
	return m, true
}

// completionLines draws the popup: one string of exactly width columns per row. A suggestion that is longer than the popup continues on
// further rows.
func (m EditorModel) completionLines(width int) []string {
	state := m.completion
	lines, _ := drawRows(state.top, len(state.items), maxCompletionRows, func(row int) []string { return m.completionItemLines(row, width) })
	return lines
}

// completionItemLines draws one suggestion: the label, then what the server says about it (or the kind), wrapped at the width of the popup.
func (m EditorModel) completionItemLines(index, width int) []string {
	state := m.completion
	item := state.items[index]
	labelWidth := state.labelWidth
	label := sanitize(item.Label)
	extra := sanitize(item.Detail)
	if extra == "" {
		extra = lsp.KindName(item.Kind)
	}
	text := " " + label + strings.Repeat(" ", max(labelWidth-runewidth.StringWidth(label), 0)) + "  " + extra
	style := m.completionRowStyle(index == state.sel)
	parts := splitRowLines(text, width)
	lines := make([]string, len(parts))
	for i, part := range parts {
		lines[i] = style.Render(padRight(part, width))
	}
	return lines
}

// completionRowStyle is the style of a popup row. Where the selection is reverse video the rows are too (header style), so
// the chosen one is the plain bold line instead.
func (m EditorModel) completionRowStyle(chosen bool) lipgloss.Style {
	switch {
	case !chosen:
		return m.theme.Header
	case m.theme.Selected.GetReverse():
		return m.theme.Equal.Bold(true)
	}
	return m.theme.Selected
}

// completionWidth is the width of the popup.
func (m EditorModel) completionWidth(room int) int {
	return min(max(m.completion.naturalWidth, 12), maxCompletionWidth, room)
}

// completionHint is the message line text while the popup is open.
func (m EditorModel) completionHint() string {
	state := m.completion
	return fmt.Sprintf(" %d suggestions (%s) · [tab] accept  [ctrl+n/p] choose  [esc] close", len(state.items), state.source)
}
