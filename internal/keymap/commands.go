// Package keymap holds the commands whose shortcuts can be changed in a .jdrc file, their default keys, the reader of that file and
// the lookup the user interface uses to find the command of a key and the key of a command.
package keymap

// Scope is a group of commands that are active at the same time: a key may be bound to one command per scope.
type Scope string

// The scopes.
const (
	ScopeIDE    Scope = "ide"    // the project window
	ScopeMerge  Scope = "merge"  // the three-way merge view
	ScopeDiff   Scope = "diff"   // the diff, git and directory views
	ScopeVim    Scope = "vim"    // the editor's normal and visual mode
	ScopeInsert Scope = "insert" // the editor's insert mode
	ScopeNano   Scope = "nano"   // the nano-style editor
)

// Scopes lists the scopes in the order `jd rc` prints them.
func Scopes() []Scope {
	return []Scope{ScopeIDE, ScopeMerge, ScopeDiff, ScopeVim, ScopeInsert, ScopeNano}
}

// Title describes a scope.
func (s Scope) Title() string {
	switch s {
	case ScopeIDE:
		return "the project window (keys need ctrl, alt, shift or a function key: bare letters belong to the editor and the shell)"
	case ScopeMerge:
		return "the three-way merge view"
	case ScopeDiff:
		return "the diff, git and directory views"
	case ScopeVim:
		return "the editor's normal and visual mode (a key sequence is keys separated by spaces: g g)"
	case ScopeInsert:
		return "the editor's insert mode (keys that are not typed text: ctrl, alt, named and function keys)"
	case ScopeNano:
		return "the nano-style editor (keys that are not typed text; the arrows, enter, backspace, delete and tab are fixed)"
	}
	return string(s)
}

// Command is the name of something a shortcut does, as written in a .jdrc file.
type Command string

// The project window.
const (
	Palette       Command = "palette"
	Tabs          Command = "tabs"
	Terminal      Command = "terminal"
	Projects      Command = "projects"
	Find          Command = "find"
	Grep          Command = "grep"
	Diff          Command = "diff"
	Focus         Command = "focus"
	LeaveTerminal Command = "leaveterminal"
	CloseTerminal Command = "closeterminal"
	Back          Command = "back"
	Hover         Command = "hover"
	Save          Command = "save"
)

// The merge view.
const (
	MergePalette    Command = "merge.palette"
	MergeNext       Command = "merge.next"
	MergePrev       Command = "merge.prev"
	MergeOurs       Command = "merge.ours"
	MergeTheirs     Command = "merge.theirs"
	MergeBoth       Command = "merge.both"
	MergeBase       Command = "merge.base"
	MergePickOurs   Command = "merge.pickours"
	MergePickTheirs Command = "merge.picktheirs"
	MergeIgnore     Command = "merge.ignore"
	MergeCancel     Command = "merge.cancel"
	MergeAuto       Command = "merge.auto"
	MergeAllOurs    Command = "merge.allours"
	MergeAllTheirs  Command = "merge.alltheirs"
	MergeEdit       Command = "merge.edit"
	MergeInsert     Command = "merge.insert"
	MergeDelete     Command = "merge.delete"
	MergeUndo       Command = "merge.undo"
	MergeWrap       Command = "merge.wrap"
	MergePane       Command = "merge.pane"
	MergeSave       Command = "merge.save"
	MergeQuit       Command = "merge.quit"
	MergeClaude     Command = "merge.claude"
	MergeCodex      Command = "merge.codex"
)

// The diff, git and directory views.
const (
	DiffNext    Command = "diff.next"
	DiffPrev    Command = "diff.prev"
	DiffSplit   Command = "diff.split"
	DiffRevert  Command = "diff.revert"
	DiffEdit    Command = "diff.edit"
	DiffUndo    Command = "diff.undo"
	DiffQuit    Command = "diff.quit"
	DiffPalette Command = "diff.palette"
)

// The editor's normal and visual mode.
const (
	VimLeft         Command = "vim.left"
	VimRight        Command = "vim.right"
	VimDown         Command = "vim.down"
	VimUp           Command = "vim.up"
	VimLineStart    Command = "vim.linestart"
	VimFirstChar    Command = "vim.firstchar"
	VimLineEnd      Command = "vim.lineend"
	VimWordForward  Command = "vim.wordforward"
	VimWordBack     Command = "vim.wordback"
	VimWordEnd      Command = "vim.wordend"
	VimTop          Command = "vim.top"
	VimBottom       Command = "vim.bottom"
	VimHalfUp       Command = "vim.halfup"
	VimPageDown     Command = "vim.pagedown"
	VimPageUp       Command = "vim.pageup"
	VimDelete       Command = "vim.delete"
	VimChange       Command = "vim.change"
	VimYank         Command = "vim.yank"
	VimInsert       Command = "vim.insert"
	VimAppend       Command = "vim.append"
	VimInsertStart  Command = "vim.insertstart"
	VimAppendEnd    Command = "vim.appendend"
	VimOpenBelow    Command = "vim.openbelow"
	VimOpenAbove    Command = "vim.openabove"
	VimDeleteChar   Command = "vim.deletechar"
	VimDeleteBefore Command = "vim.deletebefore"
	VimDeleteEnd    Command = "vim.deleteend"
	VimChangeEnd    Command = "vim.changeend"
	VimYankLine     Command = "vim.yankline"
	VimPaste        Command = "vim.paste"
	VimPasteAbove   Command = "vim.pasteabove"
	VimJoin         Command = "vim.join"
	VimTilde        Command = "vim.tilde"
	VimUndo         Command = "vim.undo"
	VimRedo         Command = "vim.redo"
	VimVisual       Command = "vim.visual"
	VimVisualLine   Command = "vim.visualline"
	VimReplace      Command = "vim.replace"
	VimSearch       Command = "vim.search"
	VimSearchBack   Command = "vim.searchback"
	VimSearchNext   Command = "vim.searchnext"
	VimSearchPrev   Command = "vim.searchprev"
	VimCommandLine  Command = "vim.commandline"
	VimDefinition   Command = "vim.definition"
	VimReferences   Command = "vim.references"
	VimNextTab      Command = "vim.nexttab"
	VimPrevTab      Command = "vim.prevtab"
)

// The editor's insert mode.
const (
	InsertNewline       Command = "insert.newline"
	InsertBackspace     Command = "insert.backspace"
	InsertDelete        Command = "insert.delete"
	InsertLeft          Command = "insert.left"
	InsertRight         Command = "insert.right"
	InsertUp            Command = "insert.up"
	InsertDown          Command = "insert.down"
	InsertHome          Command = "insert.home"
	InsertEnd           Command = "insert.end"
	InsertTab           Command = "insert.tab"
	InsertDeleteToStart Command = "insert.deletetostart"
	InsertDeleteToEnd   Command = "insert.deletetoend"
	InsertComplete      Command = "insert.complete"
)

// The nano-style editor.
const (
	NanoExit       Command = "nano.exit"
	NanoCut        Command = "nano.cut"
	NanoUncut      Command = "nano.uncut"
	NanoSearch     Command = "nano.search"
	NanoSearchNext Command = "nano.searchnext"
	NanoReplace    Command = "nano.replace"
	NanoGoto       Command = "nano.goto"
	NanoUndo       Command = "nano.undo"
	NanoRedo       Command = "nano.redo"
	NanoMark       Command = "nano.mark"
	NanoCopy       Command = "nano.copy"
	NanoPageUp     Command = "nano.pageup"
	NanoPageDown   Command = "nano.pagedown"
	NanoWordLeft   Command = "nano.wordleft"
	NanoWordRight  Command = "nano.wordright"
	NanoTop        Command = "nano.top"
	NanoBottom     Command = "nano.bottom"
)

// definition is one command: where it is active, what it does and the keys it has when no file says otherwise. A default is a key or
// several keys separated by spaces (a sequence); the first one is the command's own name for itself in the editor (see Canonical).
type definition struct {
	command Command
	scope   Scope
	doc     string
	keys    []string
}

// sequences are the default keys of the definition.
func (d definition) sequences() []Sequence {
	out := make([]Sequence, len(d.keys))
	for i, text := range d.keys {
		seq, err := ParseSequence(text)
		if err != nil {
			panic("keymap: the default " + text + " of " + string(d.command) + ": " + err.Error())
		}
		out[i] = seq
	}
	return out
}

// definitions lists every command in the order `jd rc` prints them. The default keys are the ones jd always had.
var definitions = []definition{
	{Palette, ScopeIDE, "show the command palette", []string{"ctrl+a", "f1"}},
	{Tabs, ScopeIDE, "switch tab (files and terminals); shift+tab only in the tree and the editor's normal mode, alt+t everywhere", []string{"shift+tab", "alt+t"}},
	{Terminal, ScopeIDE, "show or hide the terminal panel", []string{"ctrl+t"}},
	{Projects, ScopeIDE, "switch project", []string{"ctrl+o"}},
	{Find, ScopeIDE, "find a file by name", []string{"ctrl+f"}},
	{Grep, ScopeIDE, "search text in the project", []string{"ctrl+g"}},
	{Diff, ScopeIDE, "show the git diff", []string{"alt+d"}},
	{Focus, ScopeIDE, "switch focus between the tree and the editor", []string{"ctrl+e"}},
	{LeaveTerminal, ScopeIDE, "give the focus back from the terminal panel to the editor", []string{"ctrl+]"}},
	{CloseTerminal, ScopeIDE, "end the shown terminal tab", []string{"ctrl+d"}},
	{Back, ScopeIDE, "go back to where the last jump started", []string{"ctrl+b"}},
	{Hover, ScopeIDE, "show the documentation of the symbol under the cursor (language server)", []string{"ctrl+alt+space"}},
	{Save, ScopeIDE, "save the file (in every editor mode)", []string{"ctrl+s"}},

	{MergePalette, ScopeMerge, "show the command palette", []string{"ctrl+a", "f1"}},
	{MergeNext, ScopeMerge, "next change", []string{"n", "tab"}},
	{MergePrev, ScopeMerge, "previous change", []string{"p", "shift+tab"}},
	{MergeOurs, ScopeMerge, "add ours to the change (again: remove it)", []string{"right"}},
	{MergeTheirs, ScopeMerge, "add theirs to the change (again: remove it)", []string{"left"}},
	{MergeBoth, ScopeMerge, "take both sides and jump to the next conflict", []string{"b"}},
	{MergeBase, ScopeMerge, "take the base and jump to the next conflict", []string{"r"}},
	{MergePickOurs, ScopeMerge, "take ours and jump to the next conflict", []string{"o"}},
	{MergePickTheirs, ScopeMerge, "take theirs and jump to the next conflict", []string{"t"}},
	{MergeIgnore, ScopeMerge, "ignore both sides of an open conflict", []string{"x"}},
	{MergeCancel, ScopeMerge, "cancel the decision for the change", []string{"backspace", "delete"}},
	{MergeAuto, ScopeMerge, "resolve the conflicts that are none in fact", []string{"m"}},
	{MergeAllOurs, ScopeMerge, "take ours for every open conflict", []string{"ctrl+o", "ctrl+right"}},
	{MergeAllTheirs, ScopeMerge, "take theirs for every open conflict", []string{"ctrl+t", "ctrl+left"}},
	{MergeEdit, ScopeMerge, "edit the change under the cursor", []string{"e", "enter"}},
	{MergeInsert, ScopeMerge, "insert a line below the cursor", []string{"i"}},
	{MergeDelete, ScopeMerge, "delete the line under the cursor", []string{"d"}},
	{MergeUndo, ScopeMerge, "undo", []string{"u"}},
	{MergeWrap, ScopeMerge, "wrap or cut long lines", []string{"w"}},
	{MergePane, ScopeMerge, "switch the shown pane on a narrow terminal", []string{"v"}},
	{MergeSave, ScopeMerge, "save the result", []string{"ctrl+s", "s"}},
	{MergeQuit, ScopeMerge, "quit", []string{"q", "esc", "ctrl+c", "ctrl+d"}},
	{MergeClaude, ScopeMerge, "let Claude resolve the open conflicts", []string{"a"}},
	{MergeCodex, ScopeMerge, "let Codex resolve the open conflicts", []string{"c"}},

	{DiffNext, ScopeDiff, "next change (or file)", []string{"down", "tab"}},
	{DiffPrev, ScopeDiff, "previous change (or file)", []string{"up", "shift+tab"}},
	{DiffSplit, ScopeDiff, "switch between the split and the unified view", []string{"t"}},
	{DiffRevert, ScopeDiff, "revert the selected change", []string{"r", "backspace", "delete"}},
	{DiffEdit, ScopeDiff, "edit the file", []string{"e", "enter"}},
	{DiffUndo, ScopeDiff, "undo the last revert or edit", []string{"u"}},
	{DiffQuit, ScopeDiff, "quit", []string{"q", "esc", "ctrl+c", "ctrl+d"}},
	{DiffPalette, ScopeDiff, "show the command palette", []string{"ctrl+a", "f1"}},

	{VimLeft, ScopeVim, "move left", []string{"h", "left", "backspace"}},
	{VimRight, ScopeVim, "move right", []string{"l", "right", "space"}},
	{VimDown, ScopeVim, "move down", []string{"j", "down"}},
	{VimUp, ScopeVim, "move up", []string{"k", "up"}},
	{VimLineStart, ScopeVim, "to the start of the line (0 always works too)", []string{"home"}},
	{VimFirstChar, ScopeVim, "to the first character of the line", []string{"^"}},
	{VimLineEnd, ScopeVim, "to the end of the line", []string{"$", "end"}},
	{VimWordForward, ScopeVim, "to the next word", []string{"w"}},
	{VimWordBack, ScopeVim, "to the previous word", []string{"b"}},
	{VimWordEnd, ScopeVim, "to the end of the word", []string{"e"}},
	{VimTop, ScopeVim, "to the first line (with a count: to that line)", []string{"g g"}},
	{VimBottom, ScopeVim, "to the last line (with a count: to that line)", []string{"G"}},
	{VimHalfUp, ScopeVim, "scroll half a page up", []string{"ctrl+u"}},
	{VimPageDown, ScopeVim, "scroll a page down", []string{"pgdown"}},
	{VimPageUp, ScopeVim, "scroll a page up", []string{"pgup"}},
	{VimDelete, ScopeVim, "delete operator (twice: the line)", []string{"d"}},
	{VimChange, ScopeVim, "change operator (twice: the line)", []string{"c"}},
	{VimYank, ScopeVim, "yank operator (twice: the line)", []string{"y"}},
	{VimInsert, ScopeVim, "insert before the cursor", []string{"i"}},
	{VimAppend, ScopeVim, "insert after the cursor", []string{"a"}},
	{VimInsertStart, ScopeVim, "insert at the start of the line", []string{"I"}},
	{VimAppendEnd, ScopeVim, "insert at the end of the line", []string{"A"}},
	{VimOpenBelow, ScopeVim, "open a line below (visual: swap the ends of the selection)", []string{"o"}},
	{VimOpenAbove, ScopeVim, "open a line above", []string{"O"}},
	{VimDeleteChar, ScopeVim, "delete the character under the cursor", []string{"x"}},
	{VimDeleteBefore, ScopeVim, "delete the character before the cursor", []string{"X"}},
	{VimDeleteEnd, ScopeVim, "delete to the end of the line", []string{"D"}},
	{VimChangeEnd, ScopeVim, "change to the end of the line", []string{"C"}},
	{VimYankLine, ScopeVim, "yank the line", []string{"Y"}},
	{VimPaste, ScopeVim, "paste after the cursor", []string{"p"}},
	{VimPasteAbove, ScopeVim, "paste before the cursor", []string{"P"}},
	{VimJoin, ScopeVim, "join the next line to this one", []string{"J"}},
	{VimTilde, ScopeVim, "switch the case of the character", []string{"~"}},
	{VimUndo, ScopeVim, "undo", []string{"u"}},
	{VimRedo, ScopeVim, "redo", []string{"ctrl+r"}},
	{VimVisual, ScopeVim, "visual mode", []string{"v"}},
	{VimVisualLine, ScopeVim, "visual line mode", []string{"V"}},
	{VimReplace, ScopeVim, "replace the character (the next key is the new one)", []string{"r"}},
	{VimSearch, ScopeVim, "search forwards", []string{"/"}},
	{VimSearchBack, ScopeVim, "search backwards", []string{"?"}},
	{VimSearchNext, ScopeVim, "next match", []string{"n"}},
	{VimSearchPrev, ScopeVim, "previous match", []string{"N"}},
	{VimCommandLine, ScopeVim, "start a : command", []string{":"}},
	{VimDefinition, ScopeVim, "go to the definition of the symbol", []string{"g d"}},
	{VimReferences, ScopeVim, "list the references of the symbol", []string{"g r"}},
	{VimNextTab, ScopeVim, "next file tab (project window)", []string{"g t"}},
	{VimPrevTab, ScopeVim, "previous file tab (project window)", []string{"g T"}},

	{InsertNewline, ScopeInsert, "break the line", []string{"enter"}},
	{InsertBackspace, ScopeInsert, "delete the character before the cursor", []string{"backspace"}},
	{InsertDelete, ScopeInsert, "delete the character under the cursor", []string{"delete"}},
	{InsertLeft, ScopeInsert, "move left", []string{"left"}},
	{InsertRight, ScopeInsert, "move right", []string{"right"}},
	{InsertUp, ScopeInsert, "move up", []string{"up"}},
	{InsertDown, ScopeInsert, "move down", []string{"down"}},
	{InsertHome, ScopeInsert, "to the start of the line", []string{"home"}},
	{InsertEnd, ScopeInsert, "to the end of the line", []string{"end"}},
	{InsertTab, ScopeInsert, "insert a tab", []string{"tab"}},
	{InsertDeleteToStart, ScopeInsert, "delete everything before the cursor", []string{"ctrl+u"}},
	{InsertDeleteToEnd, ScopeInsert, "delete everything after the cursor", []string{"ctrl+k"}},
	{InsertComplete, ScopeInsert, "open the completion popup", []string{"ctrl+space", "ctrl+n"}},

	{NanoExit, ScopeNano, "leave the file (asks to save when there are changes)", []string{"ctrl+x"}},
	{NanoCut, ScopeNano, "cut the line, or the marked text", []string{"ctrl+k"}},
	{NanoUncut, ScopeNano, "paste what was cut or copied", []string{"ctrl+u"}},
	{NanoSearch, ScopeNano, "search for text", []string{"ctrl+w"}},
	{NanoSearchNext, ScopeNano, "go to the next match of the last search", []string{"alt+w"}},
	{NanoReplace, ScopeNano, "search and replace (ctrl+\\ is not sent by every terminal: alt+r always works)", []string{"ctrl+\\", "alt+r"}},
	{NanoGoto, ScopeNano, "go to a line (ctrl+_ is not sent by every terminal: alt+g always works)", []string{"ctrl+_", "alt+g"}},
	{NanoUndo, ScopeNano, "undo", []string{"alt+u"}},
	{NanoRedo, ScopeNano, "redo", []string{"alt+e"}},
	{NanoMark, ScopeNano, "set or remove the mark: the text up to the cursor is selected", []string{"alt+a", "ctrl+^"}},
	{NanoCopy, ScopeNano, "copy the marked text", []string{"alt+6"}},
	{NanoPageUp, ScopeNano, "one page up", []string{"pgup", "ctrl+y"}},
	{NanoPageDown, ScopeNano, "one page down", []string{"pgdown", "ctrl+v"}},
	{NanoWordLeft, ScopeNano, "one word to the left", []string{"ctrl+left"}},
	{NanoWordRight, ScopeNano, "one word to the right", []string{"ctrl+right"}},
	{NanoTop, ScopeNano, "to the start of the file", []string{"ctrl+home"}},
	{NanoBottom, ScopeNano, "to the end of the file", []string{"ctrl+end"}},
}

func find(command Command) (definition, bool) {
	for _, d := range definitions {
		if d.command == command {
			return d, true
		}
	}
	return definition{}, false
}

// Commands lists every command name, in the order `jd rc` prints them.
func Commands() []Command {
	out := make([]Command, len(definitions))
	for i, d := range definitions {
		out[i] = d.command
	}
	return out
}

// ScopeOf returns the scope of a command ("" for an unknown one).
func ScopeOf(command Command) Scope {
	d, _ := find(command)
	return d.scope
}

// Doc returns what a command does.
func Doc(command Command) string {
	d, _ := find(command)
	return d.doc
}

// Canonical is the command's name for itself in the editor: its first default key or sequence. The editor's engine is written for these keys
// (vim.left is h, vim.top is g g), so the resolver turns whatever the user bound into them.
func Canonical(command Command) Sequence {
	d, ok := find(command)
	if !ok {
		return nil
	}
	return d.sequences()[0]
}
