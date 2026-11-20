// Package ui contains the Bubble Tea models and rendering for jelly-diff.
package ui

import "github.com/eutima/jelly-diff/internal/diff"

// CellKind describes how a cell is drawn.
type CellKind int

const (
	// CellEqual is an unchanged line.
	CellEqual CellKind = iota
	// CellAdded is a line that exists only in the new text.
	CellAdded
	// CellDeleted is a line that exists only in the old text.
	CellDeleted
	// CellModified is a line that differs from its counterpart.
	CellModified
	// CellFiller is an empty placeholder that keeps both panes aligned.
	CellFiller
)

// Cell is one side of a Row.
type Cell struct {
	Kind CellKind
	No   int // one-based line number, 0 for fillers
	Text string
}

// Row is one visual line of the side-by-side view.
type Row struct {
	Left  Cell
	Right Cell
	Hunk  int // index of the change this row belongs to, -1 for unchanged rows
}

// BuildRows turns diff blocks into aligned rows and returns the index of the first row of each change.
func BuildRows(blocks []diff.Block) (rows []Row, hunkStarts []int) {
	for _, block := range blocks {
		if block.Kind == diff.Equal {
			for i, text := range block.Old {
				rows = append(rows, Row{
					Left:  Cell{Kind: CellEqual, No: block.OldStart + i + 1, Text: text},
					Right: Cell{Kind: CellEqual, No: block.NewStart + i + 1, Text: text},
					Hunk:  -1,
				})
			}
			continue
		}
		hunk := len(hunkStarts)
		hunkStarts = append(hunkStarts, len(rows))
		height := max(len(block.Old), len(block.New))
		for i := 0; i < height; i++ {
			row := Row{Left: Cell{Kind: CellFiller}, Right: Cell{Kind: CellFiller}, Hunk: hunk}
			hasOld, hasNew := i < len(block.Old), i < len(block.New)
			switch {
			case hasOld && hasNew:
				row.Left = Cell{Kind: CellModified, No: block.OldStart + i + 1, Text: block.Old[i]}
				row.Right = Cell{Kind: CellModified, No: block.NewStart + i + 1, Text: block.New[i]}
			case hasOld:
				row.Left = Cell{Kind: CellDeleted, No: block.OldStart + i + 1, Text: block.Old[i]}
			default:
				row.Right = Cell{Kind: CellAdded, No: block.NewStart + i + 1, Text: block.New[i]}
			}
			rows = append(rows, row)
		}
	}
	return rows, hunkStarts
}
