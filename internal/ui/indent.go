package ui

import (
	"path/filepath"
	"strings"
)

// indentByExtension is the width of one level of indent of the file types that are indented with 2 spaces (the rest use 4), and "\t" for
// the ones that use tabs.
var indentByExtension = map[string]string{}

func init() {
	for unit, extensions := range map[string]string{
		"\t": ".go .mk",
		"  ": ".ts .tsx .js .jsx .mjs .cjs .mts .cts .html .htm .json .jsonc .yaml .yml .css .scss .less .vue .svelte .rb .tf .tfvars .nix .lua .sh .bash",
	} {
		for _, extension := range strings.Fields(extensions) {
			indentByExtension[extension] = unit
		}
	}
}

// indentFor is what the tab key types in a file: the way the file is indented already, else the habit of its type. A file with indented
// lines that start with a tab keeps tabs; otherwise the smallest indent of the lines that start with spaces is one level.
func indentFor(path string, lines []string) string {
	if name := strings.ToLower(filepath.Base(path)); name == "makefile" || name == "gnumakefile" {
		return "\t"
	}
	tabs, smallest := 0, 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "\t"):
			tabs++
		case strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "":
			spaces := len(line) - len(strings.TrimLeft(line, " "))
			if smallest == 0 || spaces < smallest {
				smallest = spaces
			}
		}
	}
	switch {
	case tabs > 0 && smallest == 0:
		return "\t"
	case smallest >= 2 && smallest <= 8:
		return strings.Repeat(" ", smallest)
	}
	if unit, ok := indentByExtension[strings.ToLower(filepath.Ext(path))]; ok {
		return unit
	}
	return "    "
}
