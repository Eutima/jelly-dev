package ui

import "slices"

// debugMarks are the lines the debugger marks in the editors of a project: the breakpoints (lines are 1-based, as shown). The project window owns
// them and every editor holds the same pointer, because an editor is copied on every key press.
type debugMarks struct {
	breakpoints map[string][]int // sorted lines by absolute file path
}

func newDebugMarks() *debugMarks { return &debugMarks{breakpoints: map[string][]int{}} }

// has tells whether a line of a file has a breakpoint.
func (d *debugMarks) has(path string, line int) bool {
	if d == nil {
		return false
	}
	_, found := slices.BinarySearch(d.breakpoints[path], line)
	return found
}

// toggle sets the breakpoint of a line or removes it, and tells whether it is set now.
func (d *debugMarks) toggle(path string, line int) bool {
	lines := d.breakpoints[path]
	index, found := slices.BinarySearch(lines, line)
	if found {
		lines = slices.Delete(lines, index, index+1)
	} else {
		lines = slices.Insert(lines, index, line)
	}
	if len(lines) == 0 {
		delete(d.breakpoints, path)
	} else {
		d.breakpoints[path] = lines
	}
	return !found
}

// all returns a copy of the breakpoints by file path.
func (d *debugMarks) all() map[string][]int {
	out := make(map[string][]int, len(d.breakpoints))
	for path, lines := range d.breakpoints {
		out[path] = slices.Clone(lines)
	}
	return out
}

// set replaces the breakpoints of a file (no lines: none).
func (d *debugMarks) set(path string, lines []int) {
	lines = slices.Clone(lines)
	slices.Sort(lines)
	lines = slices.Compact(lines)
	if len(lines) == 0 {
		delete(d.breakpoints, path)
		return
	}
	d.breakpoints[path] = lines
}

// breakpointColumn is the width of the column in front of the line numbers that shows the breakpoints.
const breakpointColumn = 2

// breakpointMark is what the marker column shows for a line with a breakpoint.
const breakpointMark = "●"

// ToggleBreakpointMsg asks the project window to set or remove the breakpoint of a line (a click in the marker column).
type ToggleBreakpointMsg struct {
	Path string
	Line int // 1-based
}
