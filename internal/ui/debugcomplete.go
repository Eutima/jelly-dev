package ui

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/dap"
)

// historyCap is how many evaluated expressions the debug console remembers.
const historyCap = 200

// maxCandidates is how many suggestions are listed in the console when a completion has several.
const maxCandidates = 60

// pushHistory adds an expression to the history, unless it is the last one.
func pushHistory(history []string, expression string) []string {
	if len(history) > 0 && history[len(history)-1] == expression {
		return history
	}
	history = append(history, expression)
	if extra := len(history) - historyCap; extra > 0 {
		history = history[extra:]
	}
	return history
}

// recall shows an older (step -1) or newer (step 1) line of the history in the input; going past the newest one brings back the line that
// was being typed.
func (m *ProjectModel) recall(step int) {
	run := m.dbg
	if run.histPos > len(m.evalHistory) { // the history is shorter than when this run began
		run.histPos = len(m.evalHistory)
	}
	next := run.histPos + step
	if next < 0 || next > len(m.evalHistory) {
		return
	}
	if run.histPos == len(m.evalHistory) { // leaving the line that is being typed
		run.draft = slices.Clone(run.input)
	}
	run.histPos = next
	if next == len(m.evalHistory) {
		run.input = slices.Clone(run.draft)
	} else {
		run.input = []rune(m.evalHistory[next])
	}
	run.cursor = len(run.input)
}

// Messages of the completion.
type debugCompleteMsg struct {
	run    *debugRun
	gen    int
	text   string // the line the suggestions were asked for
	cursor int
	items  []dap.Completion
	err    error
}

// complete asks the adapter for suggestions for the line at the cursor.
func (m ProjectModel) complete() (tea.Model, tea.Cmd) {
	run := m.dbg
	if run.state != debugStopped || run.frame >= len(run.frames) {
		m.say("completion needs a program that is stopped at a breakpoint", true)
		return m, nil
	}
	text, cursor, gen, frame, session := string(run.input), run.cursor, run.gen, run.frames[run.frame].ID, run.session
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		items, err := session.Completions(ctx, frame, text, cursor)
		return debugCompleteMsg{run: run, gen: gen, text: text, cursor: cursor, items: items, err: err}
	}
}

// isNameRune says what a name in an expression is made of.
func isNameRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// nameStart is where the name before the cursor begins (dots and brackets too when withDots: "user.Na", "arr[0].x").
func nameStart(input []rune, cursor int, withDots bool) int {
	start := cursor
	for start > 0 && (isNameRune(input[start-1]) || (withDots && strings.ContainsRune(".[]", input[start-1]))) {
		start--
	}
	return start
}

// localCompletions are the names of the variables that the panel knows (the opened ones, with their fields as "user.Name") that start with
// the word before the cursor. They stand in for an adapter that cannot complete (delve).
func (r *debugRun) localCompletions(input []rune, cursor int) []dap.Completion {
	start := nameStart(input, cursor, true)
	word := string(input[start:cursor])
	if word == "" {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	var walk func(nodes []*varNode, prefix string)
	walk = func(nodes []*varNode, prefix string) {
		for _, node := range nodes {
			name := prefix + node.name
			if strings.HasPrefix(node.name, "[") { // an element of an array is arr[0], not arr.[0]
				name = strings.TrimSuffix(prefix, ".") + node.name
			}
			if prefix != "" || !isScopeNode(r, node) { // a scope itself is no name; its variables are
				if strings.HasPrefix(name, word) && name != word && !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
			if node.open {
				if prefix == "" && node.ref > 0 && isScopeNode(r, node) {
					walk(node.children, "")
				} else {
					walk(node.children, name+".")
				}
			}
		}
	}
	walk(r.vars, "")
	sort.Strings(names)
	items := make([]dap.Completion, 0, len(names))
	for _, name := range names {
		items = append(items, dap.Completion{Label: name, Start: start, Length: cursor - start})
	}
	return items
}

// isScopeNode tells whether a node is one of the scopes (the roots of the tree).
func isScopeNode(r *debugRun, node *varNode) bool { return slices.Contains(r.vars, node) }

// debugCompleted puts what the suggestions have in common into the line, or lists them when that adds nothing. (DAP counts the columns of
// the line in UTF-16 code units, this counts runes: they differ only for characters outside the basic plane, which are rare in an expression.)
func (m ProjectModel) debugCompleted(msg debugCompleteMsg) (tea.Model, tea.Cmd) {
	run := msg.run
	if run != m.dbg || msg.gen != run.gen || string(run.input) != msg.text || run.cursor != msg.cursor {
		return m, nil // the line, the stop or the frame changed since: the suggestions are for something else
	}
	items := msg.items
	if msg.err != nil || len(items) == 0 {
		if local := run.localCompletions(run.input, run.cursor); len(local) > 0 {
			items = local // the adapter cannot complete, or has nothing: the names that are on screen
		} else if msg.err != nil {
			m.say("this debugger cannot complete here: "+msg.err.Error(), true)
			return m, nil
		} else {
			m.say("no completions", false)
			return m, nil
		}
	}
	start, end := replacedRange(run.input, run.cursor, items[0])
	sameRange := true
	for _, item := range items[1:] {
		if s, e := replacedRange(run.input, run.cursor, item); s != start || e != end {
			sameRange = false // the suggestions replace different parts of the line: they cannot be merged
		}
	}
	inserts := make([]string, 0, len(items))
	labels := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		if key := item.Label + "\x00" + strconv.Itoa(item.Start) + "\x00" + strconv.Itoa(item.Length); seen[key] {
			continue
		} else {
			seen[key] = true
		}
		inserts = append(inserts, item.Insert())
		labels = append(labels, item.Label)
	}
	common := runeCommonPrefix(inserts)
	typed := string(run.input[start:end])
	if len(inserts) == 1 || (sameRange && len([]rune(common)) > len([]rune(typed))) {
		insert := []rune(common)
		rest := slices.Clone(run.input[end:])
		run.input = append(append(slices.Clone(run.input[:start]), insert...), rest...)
		run.cursor = start + len(insert)
		if len(inserts) == 1 {
			return m, nil
		}
	}
	if sameRange && len(inserts) > 1 {
		popup := &consolePopup{start: start}
		for _, item := range items {
			if !popup.has(item.Label) {
				popup.all = append(popup.all, item)
			}
		}
		if popup.refilter(run.input, run.cursor) {
			run.popup = popup
			return m, nil
		}
	}
	shown := labels
	more := 0
	if len(shown) > maxCandidates {
		shown, more = shown[:maxCandidates], len(shown)-maxCandidates
	}
	line := "  " + strings.Join(shown, "  ")
	if more > 0 {
		line += "  (" + strconv.Itoa(more) + " more)"
	}
	run.say(line)
	return m, nil
}

// replacedRange is the part of the line that a suggestion replaces: what the adapter says, else the name before the cursor.
func replacedRange(input []rune, cursor int, item dap.Completion) (start, end int) {
	if item.Start >= 0 && item.Start <= len(input) {
		return item.Start, min(item.Start+max(item.Length, 0), len(input))
	}
	return nameStart(input, cursor, false), cursor
}

// runeCommonPrefix is the longest start that all strings share.
func runeCommonPrefix(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	prefix := []rune(texts[0])
	for _, text := range texts[1:] {
		other := []rune(text)
		n := 0
		for n < len(prefix) && n < len(other) && prefix[n] == other[n] {
			n++
		}
		prefix = prefix[:n]
	}
	return string(prefix)
}

// consolePopup is the open list of suggestions above the line for expressions. Typing narrows it to the suggestions that start with what was
// typed since it opened; the keys are the ones of the completion popup of the editor.
type consolePopup struct {
	all   []dap.Completion
	items []dap.Completion // the suggestions that match the word, in the order the debugger gave them
	start int              // where the word being completed starts in the line
	sel   int
	top   int
}

// has tells whether a suggestion with this label is in the list already.
func (p *consolePopup) has(label string) bool {
	return slices.ContainsFunc(p.all, func(item dap.Completion) bool { return item.Label == label })
}

// refilter keeps the suggestions that start with the word between start and the cursor; false: none is left (or the cursor left the word).
// A debugger that matches loosely (not by the start) or sends a text that is only the end of the name gets no popup: its answer is listed in the
// console line instead. Accepting replaces the word between start and the cursor, so text behind the cursor stays.
func (p *consolePopup) refilter(input []rune, cursor int) bool {
	if cursor < p.start || p.start > len(input) {
		return false
	}
	word := strings.ToLower(string(input[p.start:cursor]))
	chosen := ""
	if p.sel < len(p.items) {
		chosen = p.items[p.sel].Label
	}
	p.items = p.items[:0:0]
	for _, item := range p.all {
		if strings.HasPrefix(strings.ToLower(item.Insert()), word) {
			p.items = append(p.items, item)
		}
	}
	p.sel, p.top = 0, 0
	for i, item := range p.items { // the chosen suggestion stays chosen while the list narrows
		if item.Label == chosen {
			p.sel = i
		}
	}
	return len(p.items) > 0
}

// popupKey handles a key while the popup is open; used reports whether the key belonged to it. Keys that edit the line go on to the line and
// narrow the list afterwards (see narrowPopup).
func (m ProjectModel) popupKey(key string) (used bool) {
	run := m.dbg
	switch key {
	case "ctrl+n", "down", "ctrl+j":
		run.popup.sel = min(run.popup.sel+1, len(run.popup.items)-1)
	case "ctrl+p", "up", "ctrl+k":
		run.popup.sel = max(run.popup.sel-1, 0)
	case "tab", "enter":
		item := run.popup.items[run.popup.sel]
		insert := []rune(item.Insert())
		rest := slices.Clone(run.input[run.cursor:])
		run.input = append(append(slices.Clone(run.input[:run.popup.start]), insert...), rest...)
		run.cursor = run.popup.start + len(insert)
		run.popup = nil
	case "esc", "ctrl+c", "ctrl+d":
		run.popup = nil
	case "left", "right", "home", "end", "alt+up", "alt+down", "ctrl+u":
		run.popup = nil // the cursor leaves the word: the list is closed and the key does its work
		return false
	default:
		return false
	}
	return true
}

// narrowPopup keeps the popup in step with the line after a key edited it.
func (m ProjectModel) narrowPopup() {
	if run := m.dbg; run.popup != nil && !run.popup.refilter(run.input, run.cursor) {
		run.popup = nil
	}
}

// popupRows are the rows of the popup (at most maxRows lines) for a console column width columns wide, and which of them belong to the chosen
// suggestion. A long suggestion continues on further lines.
func (p *consolePopup) popupRows(width, maxRows int) (lines []string, chosen []bool) {
	rowLines := func(i int) []string { return splitRowLines("  "+sanitize(p.items[i].Label), max(width, 1)) }
	p.top = scrollToShow(p.sel, p.top, maxRows, len(p.items), func(i int) int { return len(rowLines(i)) })
	lines, rowOf := drawRows(p.top, len(p.items), maxRows, rowLines)
	chosen = make([]bool, len(lines))
	for i, row := range rowOf {
		chosen[i] = row == p.sel
	}
	return lines, chosen
}
