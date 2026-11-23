package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/dap"
	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// debugView is what the list on the left of the debug panel shows.
type debugView int

const (
	viewVariables debugView = iota
	viewStack
)

// varNode is a scope or a variable of the tree in the panel. The children of a node are fetched when it is opened for the first time.
type varNode struct {
	name, value, typ string
	ref              int // above zero: the node has children that the adapter can list
	open             bool
	loaded           bool
	loading          bool
	children         []*varNode
}

// varRow is a visible row of the tree.
type varRow struct {
	node  *varNode
	depth int
}

// text is the row as the panel shows it: indent, open or closed marker, name, value and type.
func (r varRow) text() string {
	marker := "  "
	if r.node.ref > 0 {
		marker = "▸ "
		if r.node.open {
			marker = "▾ "
		}
	}
	text := strings.Repeat("  ", r.depth) + marker + r.node.name
	if r.node.value != "" {
		text += " = " + r.node.value
	}
	if r.node.typ != "" {
		text += "  (" + r.node.typ + ")"
	}
	return sanitize(text)
}

// flatVars returns the visible rows of the tree, depth first.
func (r *debugRun) flatVars() []varRow {
	var rows []varRow
	var walk func(nodes []*varNode, depth int)
	walk = func(nodes []*varNode, depth int) {
		for _, node := range nodes {
			rows = append(rows, varRow{node: node, depth: depth})
			if node.open {
				walk(node.children, depth+1)
			}
		}
	}
	walk(r.vars, 0)
	return rows
}

func nodesOf(vars []dap.Variable) []*varNode {
	nodes := make([]*varNode, 0, len(vars))
	for _, v := range vars {
		nodes = append(nodes, &varNode{name: v.Name, value: v.Value, typ: v.Type, ref: v.Reference})
	}
	return nodes
}

// Messages of the variables and of expressions. gen says which stop or frame they were asked for: the answer of an older one is dropped.
type (
	debugScopesMsg struct {
		run    *debugRun
		gen    int
		scopes []dap.Scope
		first  []dap.Variable // the variables of the first scope, which is shown open
		err    error
	}
	debugChildrenMsg struct {
		run  *debugRun
		gen  int
		node *varNode
		vars []dap.Variable
		err  error
	}
	debugEvalMsg struct {
		run        *debugRun
		gen        int
		expression string
		value      dap.Variable
		err        error
	}
)

// fetchVariables asks for the scopes of the selected frame and the variables of the first one.
func (m ProjectModel) fetchVariables() tea.Cmd {
	run := m.dbg
	if run == nil || run.session == nil || run.frame >= len(run.frames) {
		return nil
	}
	run.gen++
	run.vars, run.varSel, run.varTop, run.varsLoading = nil, 0, 0, true
	gen, frame, session := run.gen, run.frames[run.frame].ID, run.session
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		msg := debugScopesMsg{run: run, gen: gen}
		msg.scopes, msg.err = session.Scopes(ctx, frame)
		if msg.err == nil && len(msg.scopes) > 0 && msg.scopes[0].Reference > 0 {
			msg.first, msg.err = session.Variables(ctx, msg.scopes[0].Reference)
		}
		return msg
	}
}

func (m ProjectModel) debugScopesReady(msg debugScopesMsg) (tea.Model, tea.Cmd) {
	run := msg.run
	if run != m.dbg || msg.gen != run.gen {
		return m, nil
	}
	run.varsLoading = false
	if msg.err != nil {
		run.say("cannot read the variables: " + msg.err.Error())
		return m, nil
	}
	for i, scope := range msg.scopes {
		node := &varNode{name: scope.Name, ref: scope.Reference}
		if i == 0 {
			node.open, node.loaded, node.children = true, true, nodesOf(msg.first)
		}
		run.vars = append(run.vars, node)
	}
	return m, nil
}

func (m ProjectModel) debugChildrenReady(msg debugChildrenMsg) (tea.Model, tea.Cmd) {
	if msg.run != m.dbg || msg.gen != msg.run.gen {
		return m, nil
	}
	msg.node.loading = false
	if msg.err != nil {
		msg.node.open = false
		msg.run.say("cannot read " + msg.node.name + ": " + msg.err.Error())
		return m, nil
	}
	msg.node.children, msg.node.loaded = nodesOf(msg.vars), true
	return m, nil
}

// selectedVariable is the node of the selected row, nil when there is none.
func (r *debugRun) selectedVariable() *varNode {
	rows := r.flatVars()
	if r.varSel < 0 || r.varSel >= len(rows) {
		return nil
	}
	return rows[r.varSel].node
}

// toggleVariable opens the selected variable (fetching its children the first time) or closes it.
func (m ProjectModel) toggleVariable() (tea.Model, tea.Cmd) {
	node := m.dbg.selectedVariable()
	if node == nil || node.ref == 0 {
		return m, nil
	}
	return m.openVariable(!node.open)
}

// openVariable opens or closes the selected variable.
func (m ProjectModel) openVariable(open bool) (tea.Model, tea.Cmd) {
	run := m.dbg
	node := run.selectedVariable()
	if node == nil || node.ref == 0 || node.open == open {
		return m, nil
	}
	node.open = open
	if !open || node.loaded || node.loading {
		return m, nil
	}
	node.loading = true
	gen, session, reference := run.gen, run.session, node.ref
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		vars, err := session.Variables(ctx, reference)
		return debugChildrenMsg{run: run, gen: gen, node: node, vars: vars, err: err}
	}
}

// evaluate runs an expression in the selected frame of the stopped program; the answer goes to the console.
func (m ProjectModel) evaluate(expression string) (tea.Model, tea.Cmd) {
	run := m.dbg
	switch {
	case run == nil || run.session == nil:
		m.say("no program is being debugged", true)
		return m, nil
	case run.state != debugStopped || run.frame >= len(run.frames):
		m.say("the program is not stopped: evaluate when it is waiting at a breakpoint", true)
		return m, nil
	}
	m.evalHistory = pushHistory(m.evalHistory, expression)
	run.histPos = len(m.evalHistory)
	frame, session, gen := run.frames[run.frame].ID, run.session, run.gen
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), debugRequestTimeout)
		defer cancel()
		value, err := session.Evaluate(ctx, frame, expression)
		return debugEvalMsg{run: run, gen: gen, expression: expression, value: value, err: err}
	}
}

func (m ProjectModel) debugEvaluated(msg debugEvalMsg) (tea.Model, tea.Cmd) {
	run := msg.run
	if run != m.dbg || msg.gen != run.gen { // the program went on or another frame was chosen: the value is not of the place on screen
		return m, nil
	}
	run.say("> " + msg.expression)
	if msg.err != nil {
		run.say("  " + msg.err.Error())
		m.say(msg.expression+": "+msg.err.Error(), true)
		return m, nil
	}
	text := msg.value.Value
	if msg.value.Type != "" {
		text += "  (" + msg.value.Type + ")"
	}
	run.say("  " + text)
	m.say(fmt.Sprintf("%s = %s", msg.expression, msg.value.Value), false)
	return m, nil
}

// evalCommand is :eval: the expression typed after it, else the selection or the name under the cursor.
func (m ProjectModel) evalCommand(argument string) (tea.Model, tea.Cmd) {
	if m.dbg == nil {
		m.say("no program is being debugged: start it with the debug key", true)
		return m, nil
	}
	if argument == "" {
		argument = m.editor.ExpressionAtCursor()
	}
	if argument == "" {
		m.say("nothing to evaluate: type :eval <expression>, select text or put the cursor on a name", true)
		return m, nil
	}
	return m.evaluate(argument)
}

// ExpressionAtCursor is the text selected in visual mode, else the name under the cursor (letters, digits, underscores and dots, so
// "user.name" is one expression); empty when there is none.
func (m EditorModel) ExpressionAtCursor() string {
	row, col := m.vim.Cursor()
	runes := []rune(m.vim.Line(row))
	if mode := m.vim.Mode(); mode == vimbuf.Visual || mode == vimbuf.VisualLine {
		if from, to, ok := m.vim.Selected(row); ok && from < to && to <= len(runes) {
			return strings.TrimSpace(string(runes[from:to]))
		}
		return ""
	}
	part := func(r rune) bool { return r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	if col >= len(runes) || !part(runes[col]) {
		return ""
	}
	start, end := col, col
	for start > 0 && part(runes[start-1]) {
		start--
	}
	for end < len(runes) && part(runes[end]) {
		end++
	}
	return strings.Trim(string(runes[start:end]), ".")
}
