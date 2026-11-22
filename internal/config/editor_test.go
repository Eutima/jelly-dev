package config

import (
	"os"
	"path/filepath"
	"testing"
)

func editorEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestParseEditorStyle(t *testing.T) {
	for name, want := range map[string]EditorStyle{"nano": EditorNano, " NANO ": EditorNano, "vim": EditorVim, "vi": EditorVim, "Vim": EditorVim} {
		if got, err := ParseEditorStyle(name); err != nil || got != want {
			t.Errorf("%q = %q, %v", name, got, err)
		}
	}
	if _, err := ParseEditorStyle("emacs"); err == nil {
		t.Error("emacs is not an editor style")
	}
}

func TestSaveAndLoadEditorStyle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jd")
	if _, ok := LoadEditorStyle(dir); ok {
		t.Fatal("nothing is chosen in an empty folder")
	}
	if err := SaveEditorStyle(dir, EditorVim); err != nil {
		t.Fatal(err)
	}
	if style, ok := LoadEditorStyle(dir); !ok || style != EditorVim {
		t.Fatalf("%q %v", style, ok)
	}
	if err := SaveEditorStyle(dir, "emacs"); err == nil {
		t.Fatal("an unknown style is not saved")
	}
	if err := os.WriteFile(filepath.Join(dir, editorFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadEditorStyle(dir); ok {
		t.Fatal("an unreadable file means: nothing chosen")
	}
}

func TestResolveEditorStyle_EnvThenFileThenDefault(t *testing.T) {
	dir := t.TempDir()
	if style, err := ResolveEditorStyle(dir, editorEnv(nil), false); err != nil || style != EditorNano {
		t.Fatalf("a new install gets nano: %q %v", style, err)
	}
	if style, err := ResolveEditorStyle(dir, editorEnv(nil), true); err != nil || style != EditorVim {
		t.Fatalf("someone who finished the setup earlier keeps vim: %q %v", style, err)
	}
	if err := SaveEditorStyle(dir, EditorNano); err != nil {
		t.Fatal(err)
	}
	if style, _ := ResolveEditorStyle(dir, editorEnv(nil), true); style != EditorNano {
		t.Fatalf("a saved choice wins over the default: %q", style)
	}
	if style, err := ResolveEditorStyle(dir, editorEnv(map[string]string{EnvEditor: "vim"}), false); err != nil || style != EditorVim {
		t.Fatalf("JD_EDITOR wins over the file: %q %v", style, err)
	}
	if _, err := ResolveEditorStyle(dir, editorEnv(map[string]string{EnvEditor: "pico"}), false); err == nil {
		t.Fatal("an invalid JD_EDITOR is an error")
	}
}
