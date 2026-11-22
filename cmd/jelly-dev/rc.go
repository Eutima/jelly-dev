package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eutima/jelly-dev/internal/keymap"
)

// loadKeys reads the .jdrc files (the home folder, the jd config folder and the folder jd starts in) and reports the lines it could not
// use. The project folder is the folder or the folder of the file named on the command line, else the working directory.
func loadKeys(args []string, env environment) (keymap.Map, []keymap.Problem) {
	return keymap.Load(env.getenv, env.homeDir, keysProjectDir(args, env.workDir))
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
		fmt.Fprint(stdout, "\n# Lines that were not used:\n")
		for _, problem := range problems {
			fmt.Fprintf(stdout, "# %s\n", problem)
		}
	}
	return exitOK
}
