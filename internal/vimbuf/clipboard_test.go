package vimbuf

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeClipboard is a clipboard in memory; writes can be slowed down to test ordering.
type fakeClipboard struct {
	mu    sync.Mutex
	text  string
	err   error
	delay time.Duration
	reads int
}

func (f *fakeClipboard) read() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func (f *fakeClipboard) write(text string) error {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.text = text
	return nil
}

func (f *fakeClipboard) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text
}

func withClipboard(lines ...string) (*Buffer, *fakeClipboard) {
	b := newBuffer(lines...)
	clip := &fakeClipboard{}
	b.SetClipboard(Clipboard{Read: clip.read, Write: clip.write})
	return b, clip
}

func TestClipboard_YankAndDeleteCopyToClipboard(t *testing.T) {
	b, clip := withClipboard("one two", "three")
	typeKeys(b, "yw")
	b.FlushClipboard()
	if got := clip.get(); got != "one " {
		t.Fatalf("yw copied %q", got)
	}
	typeKeys(b, "jyy")
	b.FlushClipboard()
	if got := clip.get(); got != "three\n" {
		t.Fatalf("yy copied %q (whole lines end with a line break)", got)
	}
	typeKeys(b, "kdd")
	b.FlushClipboard()
	if got := clip.get(); got != "one two\n" {
		t.Fatalf("dd copied %q", got)
	}
	typeKeys(b, "Vjd")
	b.FlushClipboard()
	if got := clip.get(); got != "three\n" {
		t.Fatalf("visual delete copied %q", got)
	}
}

func TestClipboard_PasteTakesTextCopiedElsewhere(t *testing.T) {
	b, clip := withClipboard("abc", "def")
	typeKeys(b, "yl") // the register holds "a"
	b.FlushClipboard()
	clip.text = "from outside" // copied in another program
	typeKeys(b, "$p")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"abcfrom outside", "def"}) {
		t.Fatalf("charwise paste: %q", got)
	}
	clip.text = "line one\nline two\n"
	typeKeys(b, "jp")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"abcfrom outside", "def", "line one", "line two"}) {
		t.Fatalf("a clipboard that ends with a line break pastes whole lines: %q", got)
	}
}

func TestClipboard_PasteUsesRegisterWhenClipboardUnchanged(t *testing.T) {
	b, _ := withClipboard("a", "b")
	typeKeys(b, "yyjp")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"a", "b", "a"}) {
		t.Fatalf("got %q", got)
	}
}

func TestClipboard_PasteWaitsForSlowCopy(t *testing.T) {
	b, clip := withClipboard("x", "y")
	clip.delay = 50 * time.Millisecond
	typeKeys(b, "yyjp") // the copy is still running when the paste starts
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"x", "y", "x"}) {
		t.Fatalf("got %q", got)
	}
	if clip.get() != "x\n" {
		t.Fatalf("clipboard %q", clip.get())
	}
}

func TestClipboard_NewestOfQuickCopiesWins(t *testing.T) {
	b, clip := withClipboard("one", "two", "three")
	clip.delay = 30 * time.Millisecond
	typeKeys(b, "yyjyyjyy")
	b.FlushClipboard()
	if got := clip.get(); got != "three\n" {
		t.Fatalf("clipboard %q", got)
	}
}

func TestClipboard_UnreadableClipboardFallsBackToRegister(t *testing.T) {
	b, clip := withClipboard("a", "b")
	clip.err = errors.New("clipboard is empty")
	typeKeys(b, "yyjp")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"a", "b", "a"}) {
		t.Fatalf("got %q", got)
	}
}

func TestBuffer_WorksWithoutClipboard(t *testing.T) {
	b := newBuffer("a", "b")
	typeKeys(b, "yyjp")
	b.FlushClipboard()
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"a", "b", "a"}) {
		t.Fatalf("got %q", got)
	}
}

func TestPasteText_ByMode(t *testing.T) {
	b := newBuffer("abc")
	b.PasteText("XY")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"aXYbc"}) {
		t.Fatalf("normal mode pastes after the cursor: %q", got)
	}
	b = newBuffer("abc")
	b.PasteText("one\r\ntwo\n")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"abc", "one", "two"}) {
		t.Fatalf("text with a final line break is whole lines: %q", got)
	}
	b = newBuffer("abc")
	typeKeys(b, "A")
	b.PasteText("1\n\t2")
	if got := b.Lines(); !reflect.DeepEqual(got, []string{"abc1", "\t2"}) {
		t.Fatalf("insert mode types the text, line breaks and tabs included: %q", got)
	}
	b = newBuffer("abc")
	typeKeys(b, ":")
	b.PasteText("w\nq")
	if b.CommandLine() != "wq" {
		t.Fatalf("command line %q", b.CommandLine())
	}
	b = newBuffer("abc")
	typeKeys(b, "v")
	b.PasteText("Z")
	if b.Mode() != Normal {
		t.Fatalf("pasting leaves visual mode: %v", b.Mode())
	}
}
