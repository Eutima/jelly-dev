package lsp

import (
	"testing"

	"github.com/eutima/jelly-dev/internal/tools"
)

func TestSpecFor_DockerfilesComposeFilesAndYamlGoToTheirOwnServers(t *testing.T) {
	servers := DefaultServers()
	tests := map[string][2]string{
		"/p/Dockerfile":                  {"dockerfile", "dockerfile"},
		"/p/api.dockerfile":              {"dockerfile", "dockerfile"},
		"/p/Dockerfile.dev":              {"dockerfile", "dockerfile"},
		"/p/docker-compose.yml":          {"compose", "dockercompose"},
		"/p/docker-compose.yaml":         {"compose", "dockercompose"},
		"/p/docker-compose.override.yml": {"compose", "dockercompose"},
		"/p/compose.yaml":                {"compose", "dockercompose"},
		"/p/compose.prod.yml":            {"compose", "dockercompose"},
		"/p/prod.docker-compose.yml":     {"compose", "dockercompose"},
		"/p/config/values.yaml":          {"yaml", "yaml"},
		"/p/.github/workflows/ci.yml":    {"yaml", "yaml"},
		"/p/mydockerfile.yml":            {"yaml", "yaml"},
		"/p/notcompose.yaml":             {"yaml", "yaml"},
	}
	for path, want := range tests {
		spec, languageID, ok := specFor(servers, path)
		if !ok || spec.Name != want[0] || languageID != want[1] {
			t.Errorf("%s: server %q language %q (found %v), want %v", path, spec.Name, languageID, ok, want)
		}
	}
	for _, path := range []string{"/p/notes.txt", "/p/mydockerfile", "/p/composeyml", "/p/Makefile"} {
		if spec, _, ok := specFor(servers, path); ok {
			t.Errorf("%s has no server, found %s", path, spec.Name)
		}
	}
}

// every language server of the defaults can be installed: its command is the binary of a tool of the installer, or one more
// binary of the package of such a tool (vscode-langservers-extracted installs the HTML, CSS and JSON servers together)
func TestDefaultServers_EveryServerHasAToolThatInstallsIt(t *testing.T) {
	binaries := map[string]bool{}
	for _, tool := range tools.Defaults() {
		binaries[tool.Binary] = true
	}
	samePackage := map[string]string{
		"vscode-css-language-server":  "vscode-html-language-server",
		"vscode-json-language-server": "vscode-html-language-server",
	}
	for _, server := range DefaultServers() {
		command := server.Command[0]
		if other, ok := samePackage[command]; ok {
			command = other
		}
		if !binaries[command] {
			t.Errorf("server %s (%s) has no tool in tools.Defaults()", server.Name, server.Command[0])
		}
	}
}

func TestSpecFor_CCppAndRustFiles(t *testing.T) {
	servers := DefaultServers()
	tests := map[string][2]string{
		"/p/main.c": {"clangd", "c"}, "/p/util.h": {"clangd", "c"}, "/p/app.cpp": {"clangd", "cpp"}, "/p/app.cc": {"clangd", "cpp"},
		"/p/app.hpp": {"clangd", "cpp"}, "/p/lib.rs": {"rust-analyzer", "rust"}, "/p/MAIN.CPP": {"clangd", "cpp"},
	}
	for path, want := range tests {
		spec, languageID, ok := specFor(servers, path)
		if !ok || spec.Name != want[0] || languageID != want[1] {
			t.Errorf("%s: server %q language %q (found %v), want %v", path, spec.Name, languageID, ok, want)
		}
		if len(spec.Formatter) == 0 {
			t.Errorf("%s: the %s server has a formatter", path, spec.Name)
		}
	}
}
