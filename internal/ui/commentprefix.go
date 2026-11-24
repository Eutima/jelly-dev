package ui

import (
	"path/filepath"
	"strings"
)

// commentByExtension is the line comment marker of the file types gc knows, by extension (lower case, with the dot).
var commentByExtension = map[string]string{}

// commentByName is the same for the files that are known by their whole name.
var commentByName = map[string]string{
	"dockerfile": "#", "makefile": "#", "gnumakefile": "#", "cmakelists.txt": "#", ".gitignore": "#", ".dockerignore": "#",
	".env": "#", ".jdrc": "#", "jdrc": "#", "go.mod": "//", "go.work": "//",
}

func init() {
	for marker, extensions := range map[string]string{
		"//": ".go .js .jsx .mjs .cjs .ts .tsx .c .h .cc .cpp .cxx .hpp .cs .java .kt .kts .scala .swift .rs .php .dart .zig .proto .groovy .gradle .jsonc .scss .less .v .sol",
		"#":  ".py .pyi .sh .bash .zsh .fish .rb .pl .pm .r .yaml .yml .toml .conf .cfg .mk .tf .tfvars .ps1 .psm1 .nim .cr .ex .exs .jl .gitattributes .properties .dockerfile .nix",
		"--": ".sql .lua .hs .elm .ada .vhdl .vhd",
		";":  ".ini .lisp .el .clj .cljs .scm .asm .au3",
		"\"": ".vim",
		"%":  ".tex .sty .erl .m .mat",
		"'":  ".vb .bas .vbs",
		"::": ".bat .cmd",
	} {
		for _, extension := range strings.Fields(extensions) {
			commentByExtension[extension] = marker
		}
	}
}

// commentPrefixFor is the line comment marker of a file ("" when the file type has none, such as JSON or HTML).
func commentPrefixFor(path string) string {
	name := strings.ToLower(filepath.Base(path))
	if marker, ok := commentByName[name]; ok {
		return marker
	}
	return commentByExtension[filepath.Ext(name)]
}
