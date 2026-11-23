package dap

import (
	"os"
	"path/filepath"
	"strings"
)

// nodeLaunch builds the launch request for js-debug. A JavaScript file runs with Node.js. A TypeScript file needs something that runs
// TypeScript: tsx or ts-node of the project when it has them, else Node.js itself, which strips the types (Node.js 22.6 and newer).
func nodeLaunch(file, root string) map[string]any {
	config := map[string]any{
		"type": "pwa-node", "request": "launch", "name": "jd", "program": file, "cwd": root, "console": "internalConsole",
		"sourceMaps": true, "skipFiles": []string{"<node_internals>/**"},
	}
	switch strings.ToLower(filepath.Ext(file)) {
	case ".ts", ".mts", ".cts":
		bin := filepath.Join(root, "node_modules", ".bin")
		switch {
		case fileExists(filepath.Join(bin, "tsx")):
			config["runtimeExecutable"] = filepath.Join(bin, "tsx")
		case fileExists(filepath.Join(bin, "tsx.cmd")):
			config["runtimeExecutable"] = filepath.Join(bin, "tsx.cmd")
		case fileExists(filepath.Join(bin, "ts-node")) || fileExists(filepath.Join(bin, "ts-node.cmd")):
			config["runtimeArgs"] = []string{"-r", "ts-node/register"}
		default:
			config["runtimeArgs"] = []string{"--experimental-strip-types"}
		}
	}
	return config
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
