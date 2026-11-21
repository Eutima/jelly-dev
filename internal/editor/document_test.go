package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_ReadsLinesAndEndings(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name, text string
		lines      []string
		ending     string
		final      bool
	}{
		{"lf with newline", "a\nb\n", []string{"a", "b"}, "\n", true},
		{"lf without newline", "a\nb", []string{"a", "b"}, "\n", false},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}, "\r\n", true},
		{"empty", "", nil, "\n", true},
	}
	for _, tt := range tests {
		doc, err := Load(write(tt.name, tt.text))
		if err != nil || !reflect.DeepEqual(doc.Lines, tt.lines) || doc.LineEnding != tt.ending || doc.FinalNewline != tt.final || doc.New {
			t.Errorf("%s: %+v, %v", tt.name, doc, err)
		}
	}
}

func TestLoad_ReturnsNewDocument_WhenFileMissing(t *testing.T) {
	doc, err := Load(filepath.Join(t.TempDir(), "missing.txt"))
	if err != nil || !doc.New || len(doc.Lines) != 0 || !doc.FinalNewline {
		t.Fatalf("%+v, %v", doc, err)
	}
}

func TestLoad_FailsForBinaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("a\x00b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("binary files must be refused")
	}
}

func TestSave_KeepsLineEndingAndFinalNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("a\r\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Save(path, []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "a\r\nb\r\nc" {
		t.Fatalf("saved %q", got)
	}
}

func TestSave_CreatesNewFileWithFinalNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	doc, _ := Load(path)
	saved, err := doc.Save(path, []string{"x"})
	if err != nil || saved.New {
		t.Fatalf("%+v, %v", saved, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x\n" {
		t.Fatalf("saved %q", got)
	}
}
