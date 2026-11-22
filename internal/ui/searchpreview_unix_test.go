//go:build !windows

package ui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSearch_PreviewNeverOpensAPipeThatReplacedTheFile(t *testing.T) {
	model, fx := newProjectFixture(t, 100, map[string]string{"a.txt": "one\ntwo needle\nthree\n"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(ProjectModel)
	model = projectSend(model, tea.KeyMsg{Type: tea.KeyCtrlG})
	model = projectSend(model, runeKeys("needle")...)
	model = projectSend(model, searchTickMsg{seq: model.search.seq})
	path := filepath.Join(fx.root, "a.txt")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skip("no named pipes here")
	}
	done := make(chan string, 1)
	go func() { done <- stripANSI(model.View()) }()
	select {
	case view := <-done:
		if !strings.Contains(view, "cannot read the file") {
			t.Errorf("a pipe must not be read:\n%s", view)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("drawing the preview blocks on a named pipe")
	}
}
