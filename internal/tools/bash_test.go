package tools

import (
	"slices"
	"testing"
)

func TestLanguages_BashIsAnOrdinaryLanguageButNotAFirstStartDefault(t *testing.T) {
	if all, _ := ParseLanguages("all"); !slices.Contains(all, "bash") {
		t.Errorf("all must include bash: %v", all)
	}
	if slices.Contains(DefaultLanguages(), "bash") {
		t.Errorf("bash must not be a first-start default: %v", DefaultLanguages())
	}
	if got := ToolsFor([]string{"bash"}); !slices.Equal(got, []string{"bash-language-server"}) {
		t.Errorf("tools of bash: %v", got)
	}
}
