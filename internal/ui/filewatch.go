package ui

import (
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/vimbuf"
)

// watchInterval is how often the open files are looked at for changes made by other programs. Polling is cheap for the few files that are
// open, and it works where file system notifications do not (WSL, network folders).
const watchInterval = time.Second

// watchTickMsg is the timer of the look at the open files.
type watchTickMsg struct{}

func watchTick() tea.Cmd {
	return tea.Tick(watchInterval, func(time.Time) tea.Msg { return watchTickMsg{} })
}

// fileStamp is what tells that a file was written: its size and time of change.
type fileStamp struct {
	size  int64
	mtime time.Time
}

// stampBook remembers the stamp of every open file as it was last seen. The window is copied by value, so the book is shared.
type stampBook map[string]fileStamp

// diskChanged looks at the open files and reports whether one of them was written since the last look. A file seen for the first time only
// gets recorded, and a file that cannot be read is ignored.
func (m ProjectModel) diskChanged() bool {
	if m.stamps == nil {
		return false
	}
	changed := false
	for _, e := range m.openEditors() {
		path := e.Path()
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		now := fileStamp{size: info.Size(), mtime: info.ModTime()}
		if before, seen := (*m.stamps)[path]; seen && before != now {
			changed = true
		}
		(*m.stamps)[path] = now
	}
	return changed
}

// watchedFiles handles the timer: when another program wrote an open file, the tab shows the new text. Unsaved edits of that tab are
// replaced as well; undo brings them back.
func (m ProjectModel) watchedFiles() (ProjectModel, tea.Cmd) {
	if m.editor.nano == nil && m.editor.vim.Mode() != vimbuf.Normal {
		return m, watchTick() // typing or selecting: the text is not replaced under the cursor, the next look in normal mode does it
	}
	if m.diskChanged() {
		if reloaded := m.reloadChanged(true); len(reloaded) > 0 {
			m.say("Reloaded "+strings.Join(reloaded, ", ")+" (changed on disk, unsaved edits are in undo)", false)
		}
	}
	return m, watchTick()
}
