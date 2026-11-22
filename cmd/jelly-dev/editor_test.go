package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/ui"
)

// editorAnswers is a runUI for the editor question: it answers with keys and counts how often it was asked; the language question gets enter.
func editorAnswers(asked *int, keys ...tea.KeyMsg) uiRunner {
	return func(model tea.Model) error {
		model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
		if _, isEditor := model.(ui.EditorChoiceModel); !isEditor {
			model.Update(keyEnter)
			return nil
		}
		*asked++
		for _, key := range keys {
			model, _ = model.Update(key)
		}
		return nil
	}
}

func envWith(fake *installerFake, values map[string]string, interactive bool) environment {
	env := fake.env(interactive, "")
	env.getenv = func(name string) string { return values[name] }
	return env
}

func TestChooseEditor_AsksOnceOnTheFirstStart_AndSavesTheAnswer(t *testing.T) {
	dir, fake, asked := t.TempDir(), &installerFake{}, 0
	env := envWith(fake, nil, true)
	env.runUI = editorAnswers(&asked, tea.KeyMsg{Type: tea.KeyUp}, keyEnter) // nano
	setupOnFirstStart(dir, io.Discard, io.Discard, env)
	if asked != 1 {
		t.Fatalf("the editor question is asked once: %d", asked)
	}
	if style, ok := config.LoadEditorStyle(dir); !ok || style != config.EditorNano {
		t.Fatalf("the answer is saved: %q %v", style, ok)
	}
	// a start without a marker (the first start was declined) must not ask again once it is answered
	asked = 0
	if err := chooseEditor(dir, io.Discard, env, false); err != nil || asked != 0 {
		t.Fatalf("answered: no second question (err %v, asked %d)", err, asked)
	}
}

func TestChooseEditor_VimIsPreselectedInTheQuestion(t *testing.T) {
	dir, asked := t.TempDir(), 0
	env := envWith(&installerFake{}, nil, true)
	env.runUI = editorAnswers(&asked, keyEnter)
	if err := chooseEditor(dir, io.Discard, env, false); err != nil {
		t.Fatal(err)
	}
	if style, _ := config.LoadEditorStyle(dir); style != config.EditorVim {
		t.Fatalf("enter keeps the preselected vim: %q", style)
	}
}

func TestChooseEditor_JdEditorSaysItWithoutAsking_AndAnInvalidValueAsksInstead(t *testing.T) {
	dir, asked := t.TempDir(), 0
	env := envWith(&installerFake{}, map[string]string{config.EnvEditor: "vim"}, true)
	env.runUI = editorAnswers(&asked, keyEnter)
	if err := chooseEditor(dir, io.Discard, env, false); err != nil || asked != 0 {
		t.Fatalf("JD_EDITOR answers: err %v, asked %d", err, asked)
	}
	if _, ok := config.LoadEditorStyle(dir); ok {
		t.Fatal("JD_EDITOR is not remembered")
	}
	dir = t.TempDir()
	var stderr bytes.Buffer
	env = envWith(&installerFake{}, map[string]string{config.EnvEditor: "emacs"}, true)
	env.runUI = editorAnswers(&asked, keyEnter)
	if err := chooseEditor(dir, &stderr, env, false); err != nil || asked != 1 || !strings.Contains(stderr.String(), config.EnvEditor) {
		t.Fatalf("an invalid value says so and asks: err %v, asked %d, %q", err, asked, stderr.String())
	}
}

func TestChooseEditor_WithoutATerminalNothingIsAskedOrSaved(t *testing.T) {
	dir, asked := t.TempDir(), 0
	env := envWith(&installerFake{}, nil, false)
	env.runUI = editorAnswers(&asked, keyEnter)
	if err := chooseEditor(dir, io.Discard, env, false); err != nil || asked != 0 {
		t.Fatalf("err %v asked %d", err, asked)
	}
	if _, ok := config.LoadEditorStyle(dir); ok {
		t.Fatal("nothing is saved: the question is still to come")
	}
}

func TestChooseEditor_JdSetupAsksAgainWithTheStoredStylePreselected(t *testing.T) {
	dir, asked := t.TempDir(), 0
	if err := config.SaveEditorStyle(dir, config.EditorVim); err != nil {
		t.Fatal(err)
	}
	env := envWith(&installerFake{}, nil, true)
	env.runUI = editorAnswers(&asked, keyEnter) // enter keeps what was preselected
	if err := chooseEditor(dir, io.Discard, env, false); err != nil || asked != 0 {
		t.Fatalf("the first start does not ask when a style is stored: %d", asked)
	}
	if err := chooseEditor(dir, io.Discard, env, true); err != nil || asked != 1 {
		t.Fatalf("jd setup asks again: %d", asked)
	}
	if style, _ := config.LoadEditorStyle(dir); style != config.EditorVim {
		t.Fatalf("vim was preselected: %q", style)
	}
}

func TestChooseEditor_ANotNowAnswerSavesNothing_AndDeclinesTheSetup(t *testing.T) {
	dir, asked := t.TempDir(), 0
	env := envWith(&installerFake{}, nil, true)
	env.runUI = editorAnswers(&asked, keyEsc)
	if err := chooseEditor(dir, io.Discard, env, false); err != errSetupDeclined {
		t.Fatalf("err %v", err)
	}
	if _, ok := config.LoadEditorStyle(dir); ok {
		t.Fatal("nothing is saved")
	}
}

func TestEditorStyle_ExistingInstallationsKeepVim_NewOnesGetNano(t *testing.T) {
	dir := t.TempDir()
	env := envWith(&installerFake{}, nil, true)
	if style, err := editorStyle(dir, env); err != nil || style != config.EditorNano {
		t.Fatalf("a new installation: %q %v", style, err)
	}
	if err := markSetupDone(dir); err != nil {
		t.Fatal(err)
	}
	if style, err := editorStyle(dir, env); err != nil || style != config.EditorVim {
		t.Fatalf("setup finished before the choice existed: %q %v", style, err)
	}
	if err := config.SaveEditorStyle(dir, config.EditorNano); err != nil {
		t.Fatal(err)
	}
	if style, _ := editorStyle(dir, env); style != config.EditorNano {
		t.Fatalf("a saved choice wins: %q", style)
	}
	if _, err := os.Stat(filepath.Join(dir, "editor.json")); err != nil {
		t.Fatal(err)
	}
}
