package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/project"
)

const scratchTitle = "Scratch files (type a name to create one, delete removes the chosen one)"

// scratchPicker is the open list of scratch files: the existing ones and, first, the one the typed text would create.
type scratchPicker struct {
	picker  PickerModel
	dir     string
	files   []project.Scratch
	newName string // the file the first entry creates, "" when the typed name is invalid or exists
	first   bool   // the first entry is the "new file" entry

	deleting string // while the question to delete a file is asked: its name

	stem string // while the file type is asked: the name typed so far, without an extension ("" when not asking)
}

// scratchTypes are the file types offered for a new scratch file: the ending and what it is.
var scratchTypes = []struct{ ext, label string }{
	{".py", "Python"}, {".js", "JavaScript"}, {".ts", "TypeScript"}, {".go", "Go"}, {".sh", "Shell"}, {".sql", "SQL"},
	{".json", "JSON"}, {".yaml", "YAML"}, {".html", "HTML"}, {".css", "CSS"}, {".md", "Markdown"}, {".txt", "Plain text"},
}

// askDelete shows the question whether to delete the scratch file name; the first entry keeps it, so enter pressed twice deletes nothing.
func (s *scratchPicker) askDelete(theme Theme, name string) {
	s.deleting = name
	s.picker = newPicker(theme, "Delete the scratch file "+sanitize(name)+"?", []PickItem{
		{Lead: "keep", Label: "No, keep it"},
		{Lead: "delete", Label: "Yes, delete it"},
	})
}

// hasExtension tells whether a name typed by the user says its file type.
func hasExtension(name string) bool {
	name = strings.TrimSpace(name)
	return filepath.Ext(name) != "" && !strings.HasSuffix(name, ".")
}

// askType shows the list of file types for a new scratch file called stem.
func (s *scratchPicker) askType(theme Theme, stem string) {
	s.stem = stem
	items := make([]PickItem, len(scratchTypes))
	for i, t := range scratchTypes {
		items[i] = PickItem{Lead: "type", Label: t.ext, Detail: t.label}
	}
	s.picker = newPicker(theme, "File type of "+sanitize(stem)+" (the ending)", items)
}

// scratchDir is the folder of the scratch files, "" when the window has none.
func (m ProjectModel) scratchDir() string {
	if m.hooks.ScratchDir == nil {
		return ""
	}
	return m.hooks.ScratchDir()
}

// scratchCommand handles `:scratch [name]`: a name opens (or creates) that scratch file, no name shows the list.
func (m ProjectModel) scratchCommand(argument string) (tea.Model, tea.Cmd) {
	dir := m.scratchDir()
	if dir == "" {
		m.say("Scratch files are not available", true)
		return m, nil
	}
	if argument != "" {
		name, err := project.ValidScratchName(argument)
		if err != nil {
			m.say(err.Error(), true)
			return m, nil
		}
		if _, statErr := os.Stat(filepath.Join(dir, name)); !hasExtension(argument) && os.IsNotExist(statErr) {
			picker := &scratchPicker{dir: dir}
			picker.askType(m.theme, strings.TrimSuffix(strings.TrimSpace(argument), "."))
			m.scratch = picker
			return m, nil
		}
		return m.openScratch(dir, name)
	}
	files, err := project.ListScratches(filepath.Dir(dir))
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	picker := &scratchPicker{dir: dir, files: files}
	picker.rebuild(m.theme, nil)
	m.scratch = picker
	return m, nil
}

// rebuild fills the list for the typed text: the entry that creates a file (when the text is a name that does not exist yet, or with no
// text a file named by the time) and then the existing files.
func (s *scratchPicker) rebuild(theme Theme, query []rune) {
	s.newName, s.first = "", false
	var items []PickItem
	typed, err := project.ValidScratchName(string(query))
	switch {
	case len(query) == 0:
		s.newName = project.NewScratchName(time.Now())
		items = append(items, PickItem{Lead: "new", Label: "New scratch file"})
	case err == nil && !s.exists(typed):
		s.newName = typed
		items = append(items, PickItem{Lead: "new", Label: "New scratch file: " + sanitize(typed)})
	}
	s.first = s.newName != ""
	for _, file := range s.files {
		items = append(items, PickItem{Lead: "scratch", Label: sanitize(file.Name), Detail: file.Modified.Format("2006-01-02 15:04")})
	}
	s.picker = newPicker(theme, scratchTitle, items)
	s.picker.query = query
	s.picker = s.picker.refilter()
}

func (s *scratchPicker) exists(name string) bool {
	for _, file := range s.files {
		if file.Name == name {
			return true
		}
	}
	return false
}

// nameOf returns the file name of an entry of the list.
func (s *scratchPicker) nameOf(index int) string {
	if s.first {
		if index == 0 {
			return s.newName
		}
		index--
	}
	if index < 0 || index >= len(s.files) {
		return ""
	}
	return s.files[index].Name
}

// updateScratchPicker passes a key or click to the open list; the chosen entry is opened.
func (m ProjectModel) updateScratchPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var outcome pickOutcome
	scratch := *m.scratch
	picker := scratch.picker
	switch msg := msg.(type) {
	case tea.KeyMsg:
		picker, outcome = picker.handleKey(msg, m.height)
	case tea.MouseMsg:
		picker, outcome = picker.handleMouse(msg, m.width, m.height)
	}
	if outcome == pickNone && scratch.deleting == "" && scratch.stem == "" && msgKey(msg) == "delete" {
		if name := scratch.nameOf(scratch.picker.Chosen()); name != "" && !(scratch.first && scratch.picker.Chosen() == 0) {
			scratch.askDelete(m.theme, name)
			m.scratch = &scratch
			return m, nil
		}
	}
	switch outcome {
	case pickNone:
		if scratch.stem == "" && string(picker.query) != string(scratch.picker.query) { // not while the file type is asked: the typed text filters the types
			scratch.rebuild(m.theme, picker.query)
		} else {
			scratch.picker = picker
		}
		m.scratch = &scratch
		return m, nil
	case pickCancelled:
		m.scratch = nil
		return m, nil
	}
	if scratch.deleting != "" { // the answer to the question
		if picker.Chosen() == 1 {
			path := filepath.Join(scratch.dir, scratch.deleting)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				m.say(err.Error(), true)
			} else {
				if i := m.tabIndex(path); i >= 0 {
					m.closeTab(i)
				}
				m.say("Deleted the scratch file "+scratch.deleting, false)
			}
		}
		return m.scratchCommand("") // the list again, without the file
	}
	if scratch.stem != "" { // the file type was chosen
		m.scratch = nil
		if i := picker.Chosen(); i >= 0 && i < len(scratchTypes) {
			return m.openScratch(scratch.dir, scratch.stem+scratchTypes[i].ext)
		}
		return m, nil
	}
	index := picker.Chosen()
	name := scratch.nameOf(index)
	if name == "" {
		m.scratch = nil
		return m, nil
	}
	if scratch.first && index == 0 && !hasExtension(string(scratch.picker.query)) { // a new file without an ending: ask for it
		stem := strings.TrimSuffix(strings.TrimSpace(string(scratch.picker.query)), ".")
		if stem == "" {
			stem = strings.TrimSuffix(name, project.DefaultScratchExt)
		}
		scratch.askType(m.theme, stem)
		m.scratch = &scratch
		return m, nil
	}
	m.scratch = nil
	return m.openScratch(scratch.dir, name)
}

// openScratch opens a scratch file in a tab; a new one is created empty, so it is in the list the next time. The file belongs to no
// project: the language server and the debugger use the environment of the open project.
func (m ProjectModel) openScratch(dir, name string) (tea.Model, tea.Cmd) {
	if err := os.MkdirAll(dir, noteDirMode); err != nil {
		m.say(fmt.Sprintf("cannot create the scratch folder: %v", err), true)
		return m, nil
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	_ = file.Close()
	if err := m.openFile(path, false); err != nil {
		m.say(err.Error(), true)
		return m, nil
	}
	m.focus = focusProjectEdit
	m.editor.notice("Scratch file "+path, false)
	return m.resizeEditor(), nil
}

// msgKey is the name of the key a message carries, "" for other messages.
func msgKey(msg tea.Msg) string {
	if key, ok := msg.(tea.KeyMsg); ok {
		return key.String()
	}
	return ""
}
