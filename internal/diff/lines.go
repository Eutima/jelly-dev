// Package diff computes line-level and word-level differences between two texts.
package diff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Kind tells whether a Block is unchanged or differs between old and new.
type Kind int

const (
	// Equal marks a block that is identical on both sides.
	Equal Kind = iota
	// Change marks a block where Old and New differ (either may be empty).
	Change
)

// Block is a run of lines. For Equal blocks Old and New hold the same lines.
// For Change blocks, an empty Old is a pure insertion and an empty New a pure deletion.
type Block struct {
	Kind     Kind
	Old      []string
	New      []string
	OldStart int // zero-based index of the first line in the old text
	NewStart int // zero-based index of the first line in the new text
}

// SplitLines splits text into lines without line terminators.
// A trailing newline does not produce an extra empty line; "\r\n" is treated as "\n".
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// Lines returns the blocks that turn oldLines into newLines, in order, covering both inputs completely.
func Lines(oldLines, newLines []string) []Block {
	dmp := diffmatchpatch.New()
	oldChars, newChars, table := dmp.DiffLinesToChars(joinLines(oldLines), joinLines(newLines))
	diffs := dmp.DiffMain(oldChars, newChars, false)
	diffs = dmp.DiffCharsToLines(diffs, table)

	var (
		blocks          []Block
		oldPos, newPos  int
		pendingOld      []string
		pendingNew      []string
		pendingOldStart int
		pendingNewStart int
	)
	flush := func() {
		if len(pendingOld) == 0 && len(pendingNew) == 0 {
			return
		}
		blocks = append(blocks, Block{
			Kind: Change, Old: pendingOld, New: pendingNew,
			OldStart: pendingOldStart, NewStart: pendingNewStart,
		})
		pendingOld, pendingNew = nil, nil
	}
	for _, d := range diffs {
		lines := SplitLines(d.Text)
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			blocks = append(blocks, Block{Kind: Equal, Old: lines, New: lines, OldStart: oldPos, NewStart: newPos})
			oldPos += len(lines)
			newPos += len(lines)
		case diffmatchpatch.DiffDelete:
			if len(pendingOld) == 0 && len(pendingNew) == 0 {
				pendingOldStart, pendingNewStart = oldPos, newPos
			}
			pendingOld = append(pendingOld, lines...)
			oldPos += len(lines)
		case diffmatchpatch.DiffInsert:
			if len(pendingOld) == 0 && len(pendingNew) == 0 {
				pendingOldStart, pendingNewStart = oldPos, newPos
			}
			pendingNew = append(pendingNew, lines...)
			newPos += len(lines)
		}
	}
	flush()
	return blocks
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
