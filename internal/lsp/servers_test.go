package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerSpecFor_HtmlInAnAngularProjectGoesToTheAngularServer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(root, DefaultServers())
	template := filepath.Join(root, "src", "app", "pricing-table-roles.html")
	if spec, _, _ := m.specFor(template); spec.Name != "html" {
		t.Fatalf("without angular.json it is plain HTML: %s", spec.Name)
	}
	if err := os.WriteFile(filepath.Join(root, "angular.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if spec, id, _ := m.specFor(template); spec.Name != "angular" || id != "html" {
		t.Fatalf("with angular.json any template is for the Angular server: %s %s", spec.Name, id)
	}
	if spec, _, _ := m.specFor(filepath.Join(root, "src", "app", "a.component.html")); spec.Name != "angular" {
		t.Fatalf("a component template still is: %s", spec.Name)
	}
}
