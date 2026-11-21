package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func request() Request {
	return Request{File: "app.go", Conflicts: []Conflict{{ID: 1, Base: []string{"a"}, Ours: []string{"b"}, Theirs: []string{"c"}}, {ID: 2}}}
}

func TestParseResponseReadsJSONInsideCodeFence(t *testing.T) {
	out := "Here you go:\n```json\n{\"resolutions\":[{\"id\":1,\"lines\":[\"b\",\"c\"]},{\"id\":2,\"lines\":[]}]}\n```"
	got, err := ParseResponse(out, request())
	if err != nil || len(got) != 2 || strings.Join(got[1], ",") != "b,c" || len(got[2]) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseResponseFailsWhenAnswerIsInvalid(t *testing.T) {
	for name, out := range map[string]string{
		"no json":        "sorry",
		"bad json":       "{nope}",
		"unknown id":     `{"resolutions":[{"id":9,"lines":[]}]}`,
		"missing lines":  `{"resolutions":[{"id":1}]}`,
		"no resolutions": `{"resolutions":[]}`,
	} {
		if _, err := ParseResponse(out, request()); err == nil {
			t.Errorf("%s: error expected", name)
		}
	}
}

func TestBuildPromptContainsConflictLinesAndFileName(t *testing.T) {
	prompt := BuildPrompt(request())
	for _, want := range []string{`"app.go"`, `"ours": [`, `"id": 2`, `"base": []`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestResolveReturnsLinesWhenAssistantAnswers(t *testing.T) {
	assistant := New("Fake", "x", func(_ context.Context, dir, prompt string) (string, error) {
		if dir == "" || !strings.Contains(prompt, "Conflicts:") {
			t.Errorf("dir %q prompt %q", dir, prompt)
		}
		return `{"resolutions":[{"id":1,"lines":["x"]}]}`, nil
	})
	got, err := assistant.Resolve(context.Background(), request())
	if err != nil || strings.Join(got[1], "") != "x" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestResolveFailsWhenAssistantFails(t *testing.T) {
	assistant := New("Fake", "x", func(context.Context, string, string) (string, error) { return "", errors.New("boom") })
	if _, err := assistant.Resolve(context.Background(), request()); err == nil || !strings.Contains(err.Error(), "Fake: boom") {
		t.Fatalf("got %v", err)
	}
	if _, err := assistant.Resolve(context.Background(), Request{}); err == nil {
		t.Fatal("error expected without conflicts")
	}
}
