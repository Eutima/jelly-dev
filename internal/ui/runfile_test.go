package ui

import (
	"strings"
	"testing"

	"github.com/eutima/jelly-dev/internal/dap"
)

type unknownCSISequenceMsg []byte

func TestAsCtrlF5_RecognisesTheEscapeSequenceBubbleTeaDoesNotKnow(t *testing.T) {
	if _, ok := asCtrlF5(unknownCSISequenceMsg("\x1b[15;5~")); !ok {
		t.Fatal("the sequence of ctrl+f5 is ctrl+f5")
	}
	if _, ok := asCtrlF5(unknownCSISequenceMsg("\x1b[15;6~")); ok {
		t.Fatal("another sequence is not")
	}
	if _, ok := asCtrlF5("\x1b[15;5~"); ok {
		t.Fatal("only the message of Bubble Tea counts")
	}
}

func TestShellLine_QuotesEveryPart(t *testing.T) {
	run := dap.Command{Argv: []string{"/venv/bin/python", "/p/it's.py"}, Dir: "/my proj", Env: map[string]string{"B": "2", "A": "1"}}
	want := `cd '/my proj' && env 'A=1' 'B=2' '/venv/bin/python' '/p/it'\''s.py'`
	if got := shellLine("linux", run); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := shellLine("windows", run); !strings.HasPrefix(got, "Set-Location '/my proj'; $env:A='1'; $env:B='2'; & '/venv/bin/python' '/p/it''s.py'") {
		t.Fatalf("powershell: %s", got)
	}
}

func TestRun_StartsAShellTabAndNotTheDebugger_WhenF5(t *testing.T) {
	fx := debugProject(t)
	fx.model.hooks.Terminal = nil
	t.Setenv("SHELL", "/bin/sh")
	updated, _ := fx.model.runFile()
	model := updated.(ProjectModel)
	defer model.closeTerminal()
	if len(model.terms) != 1 || model.runTerm != model.terms[0] || !strings.HasPrefix(model.runTerm.Name(), "run main.go") {
		t.Fatalf("terms %d, run tab %v", len(model.terms), model.runTerm)
	}
	if model.dbg != nil || len(fx.started) != 0 {
		t.Fatal("F5 does not start the debugger")
	}
	again, _ := model.runFile()
	if got := again.(ProjectModel); len(got.terms) != 1 || got.runTerm == model.runTerm {
		t.Fatalf("a second run replaces the first tab: %d tabs", len(got.terms))
	} else {
		defer got.closeTerminal()
	}
}
