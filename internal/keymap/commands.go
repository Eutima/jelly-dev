// Package keymap holds the commands whose shortcuts can be changed in a .jdrc file, their default keys, the reader of that file and
// the lookup the user interface uses to find the command of a key and the key of a command.
package keymap

// Scope is a group of commands that are active at the same time: a key may be bound to one command per scope.
type Scope string

// The scopes.
const (
	ScopeIDE   Scope = "ide"   // the project window
	ScopeMerge Scope = "merge" // the three-way merge view
	ScopeDiff  Scope = "diff"  // the diff, git and directory views
)

// Scopes lists the scopes in the order `jd rc` prints them.
func Scopes() []Scope { return []Scope{ScopeIDE, ScopeMerge, ScopeDiff} }

// Title describes a scope.
func (s Scope) Title() string {
	switch s {
	case ScopeIDE:
		return "the project window (keys need ctrl, alt, shift or a function key: bare letters belong to the editor and the shell)"
	case ScopeMerge:
		return "the three-way merge view"
	case ScopeDiff:
		return "the diff, git and directory views"
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
	DiffNext   Command = "diff.next"
	DiffPrev   Command = "diff.prev"
	DiffSplit  Command = "diff.split"
	DiffRevert Command = "diff.revert"
	DiffEdit   Command = "diff.edit"
	DiffUndo   Command = "diff.undo"
	DiffQuit   Command = "diff.quit"
)

// definition is one command: where it is active, what it does and the keys it has when no file says otherwise.
type definition struct {
	command Command
	scope   Scope
	doc     string
	keys    []string
}

// definitions lists every command in the order `jd rc` prints them. The default keys are the ones jd always had.
var definitions = []definition{
	{Palette, ScopeIDE, "show the command palette", []string{"alt+p", "f1"}},
	{Tabs, ScopeIDE, "switch tab (files and terminals); shift+tab only in the tree and the editor's normal mode, alt+t everywhere", []string{"shift+tab", "alt+t"}},
	{Terminal, ScopeIDE, "show or hide the terminal panel", []string{"ctrl+t"}},
	{Projects, ScopeIDE, "switch project", []string{"ctrl+p"}},
	{Find, ScopeIDE, "find a file by name", []string{"ctrl+f"}},
	{Grep, ScopeIDE, "search text in the project", []string{"ctrl+g"}},
	{Diff, ScopeIDE, "show the git diff", []string{"alt+d"}},
	{Focus, ScopeIDE, "switch focus between the tree and the editor", []string{"ctrl+e"}},
	{LeaveTerminal, ScopeIDE, "give the focus back from the terminal panel to the editor", []string{"ctrl+]"}},
	{CloseTerminal, ScopeIDE, "end the shown terminal tab", []string{"alt+w"}},
	{Back, ScopeIDE, "go back to where the last jump started", []string{"ctrl+o"}},
	{Save, ScopeIDE, "save the file (in every editor mode)", []string{"ctrl+s"}},

	{MergePalette, ScopeMerge, "show the command palette", []string{"alt+p", "f1"}},
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
