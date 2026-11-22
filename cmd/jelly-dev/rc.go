package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// loadKeys reads the .jdrc files (the home folder, the jd config folder and the folder jd starts in) and reports the lines it could not
// use. The project folder is the folder or the folder of the file named on the command line, else the working directory.
func loadKeys(args []string, env environment) (keymap.Map, []keymap.Problem) {
	return keymap.Load(env.getenv, env.homeDir, keysProjectDir(args, env.workDir))
}

// isHelp tells whether the command line asks for the usage text (-h or --help anywhere, or help alone), which needs no key mapping.
func isHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return len(args) == 1 && args[0] == "help"
}

// keyProblemsMessage is what jd prints when the validator finds problems in the .jdrc files, before anything runs.
func keyProblemsMessage(problems []keymap.Problem) string {
	var out strings.Builder
	for _, problem := range problems {
		fmt.Fprintf(&out, "jd: %s\n", problem)
	}
	fmt.Fprintf(&out, "jd: the key mapping has %d problem(s): fix the lines above, or move the file away (jd rc shows the mapping and these problems)\n", len(problems))
	return out.String()
}

func keysProjectDir(args []string, workDir string) string {
	if len(args) == 1 && args[0] != "" && args[0][0] != '-' {
		if info, err := os.Stat(args[0]); err == nil {
			if abs, err := filepath.Abs(args[0]); err == nil {
				if info.IsDir() {
					return abs
				}
				return filepath.Dir(abs)
			}
		}
	}
	return workDir
}

// runRC handles `jd rc`: it prints the key mapping in force, in the .jdrc syntax, with the defaults for everything no file changes.
func runRC(args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "jd: rc takes no argument\n%s\n", usage)
		return exitUsage
	}
	mapping, problems := loadKeys(nil, env)
	fmt.Fprint(stdout, mapping.Render())
	if len(problems) > 0 {
		fmt.Fprint(stdout, "\n# Problems (jd does not start until they are fixed; the mapping above is what jd rc could make of the files):\n")
		for _, problem := range problems {
			fmt.Fprintf(stdout, "# %s\n", problem)
		}
	}
	return exitOK
}
