package vimbuf

import (
	"strings"
	"sync"
	"time"
)

// Clipboard connects the unnamed register to the system clipboard: yanked and deleted text is copied to it, and a
// paste takes what is on it. Read returns the clipboard text (an error means "nothing usable"), Write replaces it.
type Clipboard struct {
	Read  func() (string, error)
	Write func(text string) error
}

// flushTimeout is how long FlushClipboard waits for a write that is still running.
const flushTimeout = 3 * time.Second

// clipboardSync copies to the clipboard in the background (tools like powershell.exe take a while) and makes a paste wait
// for the copies that are still running, so a copy followed at once by a paste does not read the old clipboard.
type clipboardSync struct {
	clip Clipboard

	mu      sync.Mutex
	pending *string // the newest text that still has to be written
	running bool
	done    chan struct{} // closed when the writer goroutine ends
	last    string        // what this buffer copied last
}

// put copies text to the clipboard; only the newest of several quick copies is written.
func (c *clipboardSync) put(text string) {
	if c.clip.Write == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = text
	c.pending = &text
	if c.running {
		return
	}
	c.running = true
	c.done = make(chan struct{})
	go c.loop(c.done)
}

func (c *clipboardSync) loop(done chan struct{}) {
	defer close(done)
	for {
		c.mu.Lock()
		next := c.pending
		c.pending = nil
		if next == nil {
			c.running = false
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		_ = c.clip.Write(*next) // a clipboard that cannot be written does not stop editing
	}
}

// flush waits until all copies are written.
func (c *clipboardSync) flush() {
	c.mu.Lock()
	done := c.done
	running := c.running
	c.mu.Unlock()
	if !running || done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(flushTimeout):
	}
}

// get returns the clipboard text when it is something other than what this buffer copied itself.
func (c *clipboardSync) get() (string, bool) {
	if c.clip.Read == nil {
		return "", false
	}
	c.flush()
	text, err := c.clip.Read()
	if err != nil || text == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return text, text != c.last
}

// SetClipboard connects the buffer to the system clipboard.
func (b *Buffer) SetClipboard(clip Clipboard) {
	b.clip = &clipboardSync{clip: clip}
}

// FlushClipboard waits until the text copied last is on the clipboard; call it before the program ends.
func (b *Buffer) FlushClipboard() {
	if b.clip != nil {
		b.clip.flush()
	}
}

// registerText is how a register is written to the clipboard: lines joined with line breaks, and a final line break for whole lines,
// which is how a later paste knows that they were whole lines.
func registerText(lines []string, linewise bool) string {
	text := strings.Join(lines, "\n")
	if linewise {
		text += "\n"
	}
	return text
}

// clipboardRegister parses clipboard text into register contents: text that ends with a line break is whole lines.
func clipboardRegister(text string) (lines []string, linewise bool) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if strings.HasSuffix(text, "\n") {
		return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), true
	}
	return strings.Split(text, "\n"), false
}

// PasteText inserts text typed or pasted by the terminal: in insert mode as typed text, otherwise like the p command with
// text that came from outside (whole lines when it ends with a line break).
func (b *Buffer) PasteText(text string) {
	switch b.mode {
	case Insert:
		for _, r := range strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n") {
			key := string(r)
			if r == '\n' {
				key = "enter"
			}
			if r == '\t' {
				key = "tab"
			}
			b.Handle(key)
		}
	case Command:
		b.cmd += strings.Map(func(r rune) rune {
			if r < ' ' {
				return -1
			}
			return r
		}, text)
	default:
		if b.mode == Visual || b.mode == VisualLine {
			b.mode = Normal
			b.clampCursor()
		}
		b.register, b.registerLine = clipboardRegister(text)
		b.pasteRegister(true, 1)
	}
}
