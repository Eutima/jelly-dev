// Package merge implements a line based three-way merge (diff3).
package merge

import (
	"sort"

	"github.com/eutima/jelly-diff/internal/diff"
)

// Kind tells what happened to a chunk between base and the two sides.
type Kind int

const (
	// Unchanged: neither side changed the lines.
	Unchanged Kind = iota
	// OursOnly: only "ours" changed the lines; the merge takes ours.
	OursOnly
	// TheirsOnly: only "theirs" changed the lines; the merge takes theirs.
	TheirsOnly
	// Both: both sides made the identical change; the merge takes it.
	Both
	// Conflict: both sides changed the same lines differently; a person has to decide.
	Conflict
)

// Chunk is a run of lines with the three versions of it. For Unchanged chunks all three are equal.
// For Both chunks created by trimming a conflict, Base is empty (the lines were added by both sides).
// A Conflict chunk that had shared edge lines trimmed keeps the complete Base region of the conflict, so
// Base does not necessarily line up with the trimmed Ours and Theirs.
//
// The line slices alias the input slices given to Merge: callers must copy before modifying them.
// Lines carry no end-of-line information; a missing newline at the end of a file is not preserved.
type Chunk struct {
	Kind   Kind
	Base   []string
	Ours   []string
	Theirs []string
}

// Resolved returns the merged lines of a chunk that needs no decision; ok is false for conflicts.
func (c Chunk) Resolved() (lines []string, ok bool) {
	switch c.Kind {
	case Unchanged:
		return c.Base, true
	case OursOnly, Both:
		return c.Ours, true
	case TheirsOnly:
		return c.Theirs, true
	default:
		return nil, false
	}
}

// HasConflicts reports whether any chunk is a conflict.
func HasConflicts(chunks []Chunk) bool { return ConflictCount(chunks) > 0 }

// ConflictCount returns the number of conflict chunks.
func ConflictCount(chunks []Chunk) int {
	count := 0
	for _, chunk := range chunks {
		if chunk.Kind == Conflict {
			count++
		}
	}
	return count
}

// Assemble joins all chunks into one text; resolve decides the lines of conflict chunks.
// A nil resolve takes the Ours lines of every conflict.
func Assemble(chunks []Chunk, resolve func(Chunk) []string) []string {
	if resolve == nil {
		resolve = func(c Chunk) []string { return c.Ours }
	}
	var lines []string
	for _, chunk := range chunks {
		if resolved, ok := chunk.Resolved(); ok {
			lines = append(lines, resolved...)
			continue
		}
		lines = append(lines, resolve(chunk)...)
	}
	return lines
}

// Conflict marker lines used by WithMarkers (same shape as `git merge-file`).
const (
	markerOurs   = "<<<<<<< "
	markerSplit  = "======="
	markerTheirs = ">>>>>>> "
)

// WithMarkers assembles the text and writes unresolved conflicts with conflict markers.
func WithMarkers(chunks []Chunk, oursLabel, theirsLabel string) []string {
	var lines []string
	for _, chunk := range chunks {
		if resolved, ok := chunk.Resolved(); ok {
			lines = append(lines, resolved...)
			continue
		}
		lines = append(lines, markerOurs+oursLabel)
		lines = append(lines, chunk.Ours...)
		lines = append(lines, markerSplit)
		lines = append(lines, chunk.Theirs...)
		lines = append(lines, markerTheirs+theirsLabel)
	}
	return lines
}

// hunk is a change of one side: base[start:end] is replaced by lines.
type hunk struct {
	start, end int
	lines      []string
	fromOurs   bool
}

// Merge merges the two sides against their common base and returns chunks covering the whole text.
func Merge(base, ours, theirs []string) []Chunk {
	hunks := append(hunksOf(base, ours, true), hunksOf(base, theirs, false)...)
	sort.SliceStable(hunks, func(i, j int) bool { return hunks[i].start < hunks[j].start })

	var (
		chunks []Chunk
		pos    int // next unprocessed base line
	)
	for i := 0; i < len(hunks); {
		// Hunks that overlap or touch form one cluster (same rule as git's xdiff).
		start, end := hunks[i].start, hunks[i].end
		j := i
		for j < len(hunks) && hunks[j].start <= end {
			end = max(end, hunks[j].end)
			j++
		}
		cluster := hunks[i:j]
		if start > pos {
			chunks = append(chunks, stable(base[pos:start]))
		}
		chunks = append(chunks, mergeCluster(base, start, end, cluster)...)
		pos = end
		i = j
	}
	if pos < len(base) {
		chunks = append(chunks, stable(base[pos:]))
	}
	return chunks
}

func stable(lines []string) Chunk {
	return Chunk{Kind: Unchanged, Base: lines, Ours: lines, Theirs: lines}
}

// hunksOf returns the changes that turn base into side.
func hunksOf(base, side []string, fromOurs bool) []hunk {
	var hunks []hunk
	for _, block := range diff.Lines(base, side) {
		if block.Kind != diff.Change {
			continue
		}
		hunks = append(hunks, hunk{
			start: block.OldStart, end: block.OldStart + len(block.Old), lines: block.New, fromOurs: fromOurs,
		})
	}
	return hunks
}

// mergeCluster merges all hunks that touch base[start:end].
func mergeCluster(base []string, start, end int, cluster []hunk) []Chunk {
	var ours, theirs []hunk
	for _, h := range cluster {
		if h.fromOurs {
			ours = append(ours, h)
		} else {
			theirs = append(theirs, h)
		}
	}
	baseLines := base[start:end]
	oursLines := apply(base, start, end, ours)
	theirsLines := apply(base, start, end, theirs)
	switch {
	case len(theirs) == 0:
		return []Chunk{{Kind: OursOnly, Base: baseLines, Ours: oursLines, Theirs: baseLines}}
	case len(ours) == 0:
		return []Chunk{{Kind: TheirsOnly, Base: baseLines, Ours: baseLines, Theirs: theirsLines}}
	case equal(oursLines, theirsLines):
		return []Chunk{{Kind: Both, Base: baseLines, Ours: oursLines, Theirs: theirsLines}}
	}
	return trimConflict(baseLines, oursLines, theirsLines)
}

// apply returns base[start:end] with the given hunks of one side applied.
func apply(base []string, start, end int, hunks []hunk) []string {
	var lines []string
	pos := start
	for _, h := range hunks {
		lines = append(lines, base[pos:h.start]...)
		lines = append(lines, h.lines...)
		pos = h.end
	}
	return append(lines, base[pos:end]...)
}

// trimConflict moves leading and trailing lines that both sides share out of the conflict.
func trimConflict(base, ours, theirs []string) []Chunk {
	prefix := 0
	for prefix < len(ours) && prefix < len(theirs) && ours[prefix] == theirs[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(ours)-prefix && suffix < len(theirs)-prefix &&
		ours[len(ours)-1-suffix] == theirs[len(theirs)-1-suffix] {
		suffix++
	}
	var chunks []Chunk
	if prefix > 0 {
		chunks = append(chunks, Chunk{Kind: Both, Ours: ours[:prefix], Theirs: theirs[:prefix]})
	}
	chunks = append(chunks, Chunk{
		Kind: Conflict, Base: base,
		Ours: ours[prefix : len(ours)-suffix], Theirs: theirs[prefix : len(theirs)-suffix],
	})
	if suffix > 0 {
		chunks = append(chunks, Chunk{
			Kind: Both, Ours: ours[len(ours)-suffix:], Theirs: theirs[len(theirs)-suffix:],
		})
	}
	return chunks
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
