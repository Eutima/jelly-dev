package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ServerSpec describes a language server: how to start it and which files it serves.
type ServerSpec struct {
	Name        string            `json:"name"`
	Command     []string          `json:"command"`
	Extensions  []string          `json:"extensions"`            // with the dot, e.g. ".go"
	LanguageID  string            `json:"languageId,omitempty"`  // for all extensions of this server
	LanguageIDs map[string]string `json:"languageIds,omitempty"` // per extension, wins over LanguageID
	RootMarkers []string          `json:"rootMarkers,omitempty"` // files that mark the root of a project, e.g. "go.mod"
	// Formatter is a command that reads a file's text on stdin and writes the formatted text to stdout; "{file}" stands
	// for the file name. It is used for :format when it is installed, else the language server formats.
	Formatter []string `json:"formatter,omitempty"`
}

// DefaultServers are the servers the installer sets up. Where a formatter command is listed it formats with priority when
// it is installed; Java has none because jdtls formats Java itself, and so do the servers of the other languages when
// their formatter is missing.
func DefaultServers() []ServerSpec {
	prettier := []string{"prettier", "--stdin-filepath", "{file}"}
	return []ServerSpec{
		{Name: "gopls", Command: []string{"gopls"}, Extensions: []string{".go"}, LanguageID: "go", RootMarkers: []string{"go.work", "go.mod"}, Formatter: []string{"gofmt"}},
		{
			Name: "typescript-language-server", Command: []string{"typescript-language-server", "--stdio"},
			Extensions:  []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"},
			LanguageIDs: map[string]string{".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact", ".js": "javascript", ".jsx": "javascriptreact", ".mjs": "javascript", ".cjs": "javascript"},
			RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json"},
			Formatter:   prettier,
		},
		{Name: "pyright", Command: []string{"pyright-langserver", "--stdio"}, Extensions: []string{".py", ".pyi"}, LanguageID: "python", RootMarkers: []string{"pyproject.toml", "setup.py", "requirements.txt"}, Formatter: []string{"black", "-q", "-"}},
		{
			Name:       "angular",
			Command:    []string{"ngserver", "--stdio", "--tsProbeLocations", "{root}/node_modules,{home}/.local/lib/node_modules", "--ngProbeLocations", "{root}/node_modules,{home}/.local/lib/node_modules"},
			Extensions: []string{".component.html"}, LanguageID: "html", RootMarkers: []string{"angular.json", "package.json"}, Formatter: prettier,
		},
		{Name: "html", Command: []string{"vscode-html-language-server", "--stdio"}, Extensions: []string{".html", ".htm"}, LanguageID: "html", Formatter: prettier},
		{Name: "css", Command: []string{"vscode-css-language-server", "--stdio"}, Extensions: []string{".css", ".scss", ".less"}, LanguageIDs: map[string]string{".css": "css", ".scss": "scss", ".less": "less"}, Formatter: prettier},
		{Name: "json", Command: []string{"vscode-json-language-server", "--stdio"}, Extensions: []string{".json", ".jsonc"}, LanguageID: "json", Formatter: prettier},
		{Name: "jdtls", Command: []string{"jdtls"}, Extensions: []string{".java"}, LanguageID: "java", RootMarkers: []string{"pom.xml", "build.gradle", "build.gradle.kts"}},
		{Name: "groovy", Command: []string{"groovy-language-server"}, Extensions: []string{".groovy", ".gradle"}, LanguageID: "groovy", RootMarkers: []string{"build.gradle", "settings.gradle"}},
	}
}

// LoadServers returns the default servers with the entries of a config file applied: {"servers": [ {name, command,
// extensions, languageId} ]}. An entry with the name of a default replaces it, other entries are added. A missing file
// is not an error.
func LoadServers(configPath string) ([]ServerSpec, error) {
	servers := DefaultServers()
	data, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return servers, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	var config struct {
		Servers []ServerSpec `json:"servers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}
	for _, custom := range config.Servers {
		if custom.Name == "" || len(custom.Command) == 0 || len(custom.Extensions) == 0 {
			return nil, fmt.Errorf("%s: every server needs a name, a command and extensions", configPath)
		}
		replaced := false
		for i := range servers {
			if servers[i].Name == custom.Name {
				servers[i], replaced = custom, true
			}
		}
		if !replaced {
			servers = append(servers, custom)
		}
	}
	return servers, nil
}

// specFor returns the server for a file and the language id to send for it. An extension may have several dots
// (".component.html"); the longest match wins, so Angular templates go to the Angular server and other HTML to the HTML server.
func specFor(servers []ServerSpec, path string) (ServerSpec, string, bool) {
	lower := strings.ToLower(filepath.Base(path))
	best, bestLen, bestExt := -1, 0, ""
	for i, spec := range servers {
		for _, candidate := range spec.Extensions {
			ext := strings.ToLower(candidate)
			if strings.HasSuffix(lower, ext) && len(lower) > len(ext)-1 && len(ext) > bestLen {
				best, bestLen, bestExt = i, len(ext), ext
			}
		}
	}
	if best < 0 {
		return ServerSpec{}, "", false
	}
	spec := servers[best]
	if id := spec.LanguageIDs[bestExt]; id != "" {
		return spec, id, true
	}
	if spec.LanguageID != "" {
		return spec, spec.LanguageID, true
	}
	return spec, strings.TrimPrefix(filepath.Ext(bestExt), "."), true
}

// rootFor returns the folder a server is started in: the nearest folder above the file (up to the project root) that
// holds one of the markers, else the project root.
func rootFor(spec ServerSpec, projectRoot, path string) string {
	for dir := filepath.Dir(path); strings.HasPrefix(dir, projectRoot); dir = filepath.Dir(dir) {
		for _, marker := range spec.RootMarkers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		if dir == projectRoot || filepath.Dir(dir) == dir {
			break
		}
	}
	return projectRoot
}
