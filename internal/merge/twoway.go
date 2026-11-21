package merge

import "github.com/eutima/jelly-diff/internal/diff"

// TwoWay compares two texts that have no common ancestor. Equal runs are unchanged chunks; every run in which
// the texts differ is a Conflict (Ours from a, Theirs from b, empty Base), so a person decides each of them and
// nothing is taken over automatically. Assembling the chunks with the Ours (Theirs) lines of every conflict
// gives a (b) again.
func TwoWay(a, b []string) []Chunk {
	var chunks []Chunk
	for _, block := range diff.Lines(a, b) {
		if block.Kind == diff.Equal {
			chunks = append(chunks, stable(block.Old))
			continue
		}
		chunks = append(chunks, Chunk{Kind: Conflict, Ours: block.Old, Theirs: block.New})
	}
	return chunks
}
