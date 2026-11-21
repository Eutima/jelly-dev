package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eutima/jelly-dev/internal/tools"
)

func languagesModel(t *testing.T) (LanguagesModel, *LanguagesOutcome) {
	t.Helper()
	m, outcome := NewLanguagesModel(tools.Languages(), tools.DefaultLanguages())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return updated.(LanguagesModel), outcome
}

func languageKeys(m LanguagesModel, keys ...string) LanguagesModel {
	for _, key := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		updated, _ := m.Update(msg)
		m = updated.(LanguagesModel)
	}
	return m
}

func TestLanguages_StartWithTheDefaultLanguagesChecked(t *testing.T) {
	m, outcome := languagesModel(t)
	view := stripANSI(m.View())
	for _, want := range []string{"which languages do you work with", "[x] Python", "[x] HTML", "[x] CSS", "[x] JSON", "[x] YAML", "[x] Dockerfile", "[x] Docker Compose", "[ ] Go", "[ ] Java", "pyright, black", "[enter] install"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the question lacks %q:\n%s", want, view)
		}
	}
	m = languageKeys(m, "enter")
	if !reflect.DeepEqual(outcome.Chosen, []string{"python", "html", "css", "json", "yaml", "docker", "compose"}) || outcome.Skipped {
		t.Fatalf("enter takes the defaults: %+v", outcome)
	}
}

func TestLanguages_ToggleAllNoneAndSkip(t *testing.T) {
	m, outcome := languagesModel(t)
	m = languageKeys(m, " ")                                    // the first entry, Python, unchecked
	m = languageKeys(m, append(rowsDown("javascript"), " ")...) // down to JavaScript
	if got := m.checkedIDs(); !reflect.DeepEqual(got, []string{"html", "css", "json", "yaml", "docker", "compose", "javascript"}) {
		t.Fatalf("space toggles the entry under the cursor: %v", got)
	}
	m = languageKeys(m, "a")
	if len(m.checkedIDs()) != len(tools.Languages()) {
		t.Fatalf("a checks everything: %v", m.checkedIDs())
	}
	m = languageKeys(m, "n", "enter")
	if outcome.Skipped || len(outcome.Chosen) != 0 {
		t.Fatalf("n then enter chooses no language: %+v", outcome)
	}

	skipped, skippedOutcome := languagesModel(t)
	languageKeys(skipped, "esc")
	if !skippedOutcome.Skipped || !skippedOutcome.Answered || skippedOutcome.Aborted || len(skippedOutcome.Chosen) != 0 {
		t.Fatalf("esc chooses no language: %+v", skippedOutcome)
	}
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD} {
		aborted, abortedOutcome := languagesModel(t)
		updated, _ := aborted.Update(tea.KeyMsg{Type: key})
		_ = updated
		if !abortedOutcome.Aborted || abortedOutcome.Answered || len(abortedOutcome.Chosen) != 0 {
			t.Fatalf("key %v means not now: %+v", key, abortedOutcome)
		}
	}
}

func TestLanguages_AClickTogglesAndTheHintsAreClickable(t *testing.T) {
	m, outcome := languagesModel(t)
	updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: languagesTop + 1}) // HTML
	m = updated.(LanguagesModel)
	if got := m.checkedIDs(); !reflect.DeepEqual(got, []string{"python", "css", "json", "yaml", "docker", "compose"}) {
		t.Fatalf("a click toggles the row: %v", got)
	}
	var x int
	for _, r := range m.hintBar().regions {
		if r.keys[0] == "a" {
			x = r.from + 1
		}
	}
	updated, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 19})
	m = updated.(LanguagesModel)
	if len(m.checkedIDs()) != len(tools.Languages()) {
		t.Fatalf("a click on [a] all checks everything: %v", m.checkedIDs())
	}
	_ = outcome
	for _, r := range m.hintBar().regions { // [space] must send the key that toggles
		if r.keys[0] == " " {
			return
		}
	}
	t.Fatal("the [space] hint must send the space key")
}

// rowsDown returns the "down" keys that lead from the first row of the list to the language.
func rowsDown(id string) []string {
	for i, l := range tools.Languages() {
		if l.ID == id {
			keys := make([]string, i)
			for j := range keys {
				keys[j] = "down"
			}
			return keys
		}
	}
	panic("unknown language " + id)
}
