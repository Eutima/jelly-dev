// Package nanokeys is the key handler of the nano-style editor: it turns the keys a user presses into operations on a vimbuf.Buffer that is in
// modeless use (see vimbuf.EnterModeless), and runs the questions nano asks in its prompt line (search, replace, go to line, save before leaving).
// It answers with the same Result values as the vim handling of the buffer, so the editors that use it need no new control flow.
package nanokeys

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eutima/jelly-dev/internal/keymap"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// Lookup returns the nano command of a key ("" when the key has none).
type Lookup func(key string) keymap.Command

// Outcome is what a key did.
type Outcome struct {
	Result  vimbuf.Result // what the caller has to do: write, leave (the same values as the vim handling)
	Handled bool          // the key belonged to the editor (text, a movement, a command or the prompt); false: the caller may use it
}

type promptKind int

const (
	noPrompt promptKind = iota
	promptSearch
	promptReplaceFind
	promptReplaceWith
	promptReplaceAsk
	promptGoto
	promptExit
)

// Session is the state of one editor: the prompt that is open, and the replace that is running.
type Session struct {
	lookup Lookup

	kind   promptKind
	input  []rune
	status string // the message of the last operation, shown until the next key

	replaceFind, replaceWith string
	replaced                 int
	startRow, startCol       int
}

// New returns a session that finds the commands of keys with lookup.
func New(lookup Lookup) *Session { return &Session{lookup: lookup} }

// Prompt returns the question that is open: its text, what has been typed for it, and whether there is one.
func (s *Session) Prompt() (label, input string, open bool) {
	switch s.kind {
	case promptSearch:
		return "Search", string(s.input), true
	case promptReplaceFind:
		return "Search (to replace)", string(s.input), true
	case promptReplaceWith:
		return "Replace with", string(s.input), true
	case promptReplaceAsk:
		return "Replace this instance? (y: yes, n: skip, a: all in the file, esc: stop)", "", true
	case promptGoto:
		return "Go to line[,column]", string(s.input), true
	case promptExit:
		return "Save modified buffer? (y: yes, n: no, esc: cancel)", "", true
	}
	return "", "", false
}

// Status is the message of the last operation ("" when there is none); the next key clears it.
func (s *Session) Status() string { return s.status }

// Cancel closes the prompt and drops the message.
func (s *Session) Cancel() { s.kind, s.input, s.status = noPrompt, nil, "" }

// HintCommands are the commands the footer shows, with their labels.
var HintCommands = []struct {
	Command keymap.Command
	Text    string
}{
	{keymap.NanoExit, "Exit"},
	{keymap.NanoSearch, "Search"},
	{keymap.NanoCut, "Cut"},
	{keymap.NanoUncut, "Paste"},
	{keymap.NanoUndo, "Undo"},
	{keymap.NanoRedo, "Redo"},
	{keymap.NanoReplace, "Replace"},
	{keymap.NanoGoto, "Go to line"},
	{keymap.NanoMark, "Mark"},
}

// Handle does what a key says to the buffer.
func (s *Session) Handle(b *vimbuf.Buffer, key string) Outcome {
	s.status = ""
	if s.kind != noPrompt {
		return s.handlePrompt(b, key)
	}
	if command := s.lookup(key); command != "" {
		return s.run(b, command)
	}
	switch key {
	case "left", "right", "up", "down", "home", "end":
		b.Move(key)
	case "pgup", "pgdown", "ctrl+left", "ctrl+right", "ctrl+home", "ctrl+end":
		b.Move(key) // the keys of nano.pageup and the others when a file took them away: still a movement
	case "enter":
		b.Newline()
	case "backspace", "ctrl+h":
		b.Backspace()
	case "delete":
		b.DeleteForward()
	case "tab":
		b.TypeText("\t")
	case "esc":
		b.ClearMark()
	case " ":
		b.TypeText(" ")
	default:
		if runes := []rune(key); len(runes) == 1 && runes[0] >= ' ' {
			b.TypeText(key)
			return Outcome{Handled: true}
		}
		return Outcome{}
	}
	return Outcome{Handled: true}
}

func (s *Session) run(b *vimbuf.Buffer, command keymap.Command) Outcome {
	switch command {
	case keymap.NanoExit:
		if !b.Modified() {
			return Outcome{Result: vimbuf.Quit, Handled: true}
		}
		s.kind = promptExit
	case keymap.NanoCut:
		b.CutLine()
	case keymap.NanoUncut:
		b.Uncut()
	case keymap.NanoSearch:
		s.open(promptSearch)
	case keymap.NanoSearchNext:
		if !b.FindNext(false) {
			s.status = "No match"
		}
	case keymap.NanoReplace:
		s.open(promptReplaceFind)
	case keymap.NanoGoto:
		s.open(promptGoto)
	case keymap.NanoUndo:
		b.Undo()
	case keymap.NanoRedo:
		b.Redo()
	case keymap.NanoMark:
		b.SetMark()
		if b.HasMark() {
			s.status = "Mark set"
		} else {
			s.status = "Mark unset"
		}
	case keymap.NanoCopy:
		if b.CopyMarked() {
			s.status = "Copied"
		} else {
			s.status = "Nothing marked: set the mark first"
		}
	case keymap.NanoPageUp:
		b.Move("pgup")
	case keymap.NanoPageDown:
		b.Move("pgdown")
	case keymap.NanoWordLeft:
		b.Move("ctrl+left")
	case keymap.NanoWordRight:
		b.Move("ctrl+right")
	case keymap.NanoTop:
		b.Move("ctrl+home")
	case keymap.NanoBottom:
		b.Move("ctrl+end")
	}
	return Outcome{Handled: true}
}

func (s *Session) open(kind promptKind) { s.kind, s.input = kind, nil }

// handlePrompt feeds a key to the open question.
func (s *Session) handlePrompt(b *vimbuf.Buffer, key string) Outcome {
	handled := Outcome{Handled: true}
	switch s.kind {
	case promptExit:
		switch key {
		case "y", "Y":
			s.Cancel()
			return Outcome{Result: vimbuf.WriteQuit, Handled: true}
		case "n", "N":
			s.Cancel()
			return Outcome{Result: vimbuf.ForceQuit, Handled: true}
		case "esc", "ctrl+c":
			s.Cancel()
		}
		return handled
	case promptReplaceAsk:
		return s.answerReplace(b, key)
	}
	switch key {
	case "esc", "ctrl+c":
		s.Cancel()
	case "backspace", "ctrl+h":
		if len(s.input) > 0 {
			s.input = s.input[:len(s.input)-1]
		}
	case "enter":
		s.finishPrompt(b)
	case " ":
		s.input = append(s.input, ' ')
	default:
		if runes := []rune(key); len(runes) == 1 && runes[0] >= ' ' {
			s.input = append(s.input, runes[0])
		}
	}
	return handled
}

// finishPrompt acts on the text typed for the open question.
func (s *Session) finishPrompt(b *vimbuf.Buffer) {
	text := string(s.input)
	kind := s.kind
	s.Cancel()
	switch kind {
	case promptSearch:
		if !b.Find(text, false, false) {
			s.status = fmt.Sprintf("%q not found", firstNonEmpty(text, b.LastSearch()))
		}
	case promptGoto:
		line, column, ok := parsePosition(text)
		if !ok {
			s.status = "Not a line number: " + text
			return
		}
		b.GotoPosition(line, column)
	case promptReplaceFind:
		if text == "" {
			return
		}
		s.replaceFind = text
		s.open(promptReplaceWith)
	case promptReplaceWith:
		s.replaceWith = text
		s.startReplace(b)
	}
}

// startReplace finds the first match and asks about it.
func (s *Session) startReplace(b *vimbuf.Buffer) {
	s.replaced = 0
	if !b.FindHere(s.replaceFind, false) { // a match at the cursor is the first one
		s.status = fmt.Sprintf("%q not found", s.replaceFind)
		return
	}
	s.startRow, s.startCol = b.Cursor()
	s.kind = promptReplaceAsk
}

// answerReplace handles y, n, a and esc while a replace asks about a match.
func (s *Session) answerReplace(b *vimbuf.Buffer, key string) Outcome {
	handled := Outcome{Handled: true}
	switch key {
	case "y", "Y":
		if b.ReplaceHere(s.replaceWith) {
			s.replaced++
		}
		s.next(b)
	case "n", "N":
		s.next(b)
	case "a", "A":
		s.replaced += b.ReplaceAll(s.replaceWith)
		s.finishReplace()
	case "esc", "ctrl+c":
		s.finishReplace()
	}
	return handled
}

// next goes to the next match; when the search comes around to where it started the replace is over.
func (s *Session) next(b *vimbuf.Buffer) {
	row, col := b.Cursor()
	if !b.FindNext(false) {
		s.finishReplace()
		return
	}
	nextRow, nextCol := b.Cursor()
	if nextRow < row || (nextRow == row && nextCol <= col) || (nextRow == s.startRow && nextCol == s.startCol) { // wrapped around
		s.finishReplace()
	}
}

func (s *Session) finishReplace() {
	s.kind = noPrompt
	switch s.replaced {
	case 0:
		s.status = "Nothing replaced"
	case 1:
		s.status = "Replaced 1 occurrence"
	default:
		s.status = fmt.Sprintf("Replaced %d occurrences", s.replaced)
	}
}

// Paste adds pasted text to the open question (line breaks and other control characters are left out). It reports whether a question was open.
func (s *Session) Paste(text string) bool {
	if s.kind == noPrompt || s.kind == promptExit || s.kind == promptReplaceAsk {
		return false
	}
	for _, r := range text {
		if r >= ' ' && r != 0x7f {
			s.input = append(s.input, r)
		}
	}
	return true
}

// parsePosition reads "12" or "12,5" (a column is optional).
func parsePosition(text string) (line, column int, ok bool) {
	lineText, columnText, hasColumn := strings.Cut(strings.TrimSpace(text), ",")
	line, err := strconv.Atoi(strings.TrimSpace(lineText))
	if err != nil || line < 1 {
		return 0, 0, false
	}
	column = 1
	if hasColumn {
		if column, err = strconv.Atoi(strings.TrimSpace(columnText)); err != nil || column < 1 {
			return 0, 0, false
		}
	}
	return line, column, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
