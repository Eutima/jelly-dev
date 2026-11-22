package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/ui"
)

// chooseEditor asks which editor style to use in the setup, once: JD_EDITOR (valid) says it without asking; a style chosen before is kept unless
// again is set (jd setup asks again, the stored style preselected); without a terminal nothing is asked and nothing is saved, the default
// (nano) counts. The answer is saved. errSetupDeclined means "not now": nothing is chosen and the question comes again.
func chooseEditor(configDir string, stderr io.Writer, env environment, again bool) error {
	stored, hasStored := config.LoadEditorStyle(configDir)
	if said := env.getenv(config.EnvEditor); said != "" {
		style, err := config.ParseEditorStyle(said)
		if err == nil {
			_ = style // JD_EDITOR answers for this run; it is not remembered (a script must not pin the style of the user)
			return nil
		}
		fmt.Fprintf(stderr, "jd: %s: %v; asking instead\n", config.EnvEditor, err)
	}
	if hasStored && !again {
		return nil
	}
	if env.interactive == nil || !env.interactive() || env.runUI == nil {
		return nil // nobody to ask (a script, CI): nano, and the question is still to come
	}
	current := config.EditorVim // preselected: enter keeps it (an install without a terminal gets nano, see config.ResolveEditorStyle)
	if hasStored {
		current = stored
	}
	model, outcome := ui.NewEditorChoiceModel(current)
	if err := env.runUI(model); err != nil {
		fmt.Fprintf(stderr, "jd: could not ask which editor: %v (using %s)\n", err, current)
		return nil
	}
	if outcome.Aborted {
		return errSetupDeclined
	}
	if outcome.Answered {
		saveEditor(configDir, outcome.Style, stderr)
	}
	return nil
}

func saveEditor(configDir string, style config.EditorStyle, stderr io.Writer) {
	if err := config.SaveEditorStyle(configDir, style); err != nil {
		fmt.Fprintf(stderr, "jd: cannot remember the editor: %v\n", err)
	}
}

// editorStyle is the style the editors use now: see config.ResolveEditorStyle. Someone who finished the first-start setup before the choice
// existed (the marker is there, no choice is saved) keeps vim.
func editorStyle(configDir string, env environment) (config.EditorStyle, error) {
	_, err := os.Stat(filepath.Join(configDir, setupMarker))
	return config.ResolveEditorStyle(configDir, env.getenv, err == nil)
}

// useEditorStyle resolves the editor style and installs it for the editors that are made from now on. An invalid JD_EDITOR is an error (the
// caller exits with usage).
func useEditorStyle(stderr io.Writer, env environment) bool {
	dir, err := project.ConfigDir(env.getenv)
	if err != nil {
		return true // no config folder: the default style stays
	}
	style, err := editorStyle(dir, env)
	if err != nil {
		printError(stderr, err)
		return false
	}
	ui.UseEditorStyle(style)
	return true
}
