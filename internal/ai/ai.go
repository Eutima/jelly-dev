// Package ai asks an installed coding assistant CLI (Claude Code or Codex) to resolve merge conflicts.
//
// The conflicts are sent as text on stdin and the answer is read from stdout. The assistant runs in an empty
// temporary directory without tools (Claude) or in a read-only sandbox (Codex), so it only sees what is sent.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Timeout is the longest an assistant may take for one request.
const Timeout = 5 * time.Minute

// Conflict is one conflict to resolve; ID identifies it in the answer.
type Conflict struct {
	ID                 int
	Base, Ours, Theirs []string
}

// Request is everything the assistant gets to see.
type Request struct {
	File      string // name of the merged file, a hint for the language
	Conflicts []Conflict
}

// Assistant is an installed assistant CLI.
type Assistant struct {
	Name string // shown to the user, e.g. "Claude"
	Key  string // key that starts a request in the merge viewer
	// run executes the CLI with the prompt on stdin in dir and returns what it printed.
	run func(ctx context.Context, dir, prompt string) (string, error)
}

// Detect returns the assistants whose CLI is found on PATH, Claude first.
func Detect() []Assistant {
	var found []Assistant
	if path, err := exec.LookPath("claude"); err == nil {
		found = append(found, Assistant{Name: "Claude", Key: "a", run: cliRunner(path, "-p", "--tools", "")})
	}
	if path, err := exec.LookPath("codex"); err == nil {
		found = append(found, Assistant{Name: "Codex", Key: "c",
			run: cliRunner(path, "exec", "--sandbox", "read-only", "--skip-git-repo-check", "-")})
	}
	return found
}

// New builds an assistant with a custom runner (used by tests).
func New(name, key string, run func(ctx context.Context, dir, prompt string) (string, error)) Assistant {
	return Assistant{Name: name, Key: key, run: run}
}

func cliRunner(path string, args ...string) func(context.Context, string, string) (string, error) {
	return func(ctx context.Context, dir, prompt string) (string, error) {
		cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- path comes from exec.LookPath, args are fixed
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(prompt)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("%w: %s", err, lastLine(stderr.String()))
		}
		return stdout.String(), nil
	}
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Resolve asks the assistant and returns the resolved lines per conflict ID. Conflicts the answer does not
// mention are missing from the result.
func (a Assistant) Resolve(ctx context.Context, request Request) (map[int][]string, error) {
	if a.run == nil {
		return nil, errors.New("assistant is not available")
	}
	if len(request.Conflicts) == 0 {
		return nil, errors.New("no conflicts to resolve")
	}
	dir, err := os.MkdirTemp("", "jd-ai-")
	if err != nil {
		return nil, fmt.Errorf("create work directory: %w", err)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	output, err := a.run(ctx, dir, BuildPrompt(request))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.Name, err)
	}
	return ParseResponse(output, request)
}

type wireConflict struct {
	ID     int      `json:"id"`
	Base   []string `json:"base"`
	Ours   []string `json:"ours"`
	Theirs []string `json:"theirs"`
}

// BuildPrompt describes the conflicts and the expected answer format.
func BuildPrompt(request Request) string {
	conflicts := make([]wireConflict, len(request.Conflicts))
	for i, c := range request.Conflicts {
		conflicts[i] = wireConflict{ID: c.ID, Base: nonNil(c.Base), Ours: nonNil(c.Ours), Theirs: nonNil(c.Theirs)}
	}
	data, _ := json.MarshalIndent(conflicts, "", "  ") // plain strings and ints cannot fail to marshal
	var b strings.Builder
	b.WriteString("You resolve git merge conflicts. Do not use any tools and do not read or write files.\n")
	fmt.Fprintf(&b, "The file is named %q. Each conflict below has the lines of the common ancestor (base), of our side\n", request.File)
	b.WriteString("(ours) and of their side (theirs). Produce the lines that replace the conflict and keep the intent of\n")
	b.WriteString("both sides. Keep the indentation and style of the surrounding code. Lines carry no line ending.\n\n")
	b.WriteString("Answer with JSON only, no explanation and no code fence, in exactly this shape:\n")
	b.WriteString(`{"resolutions":[{"id":1,"lines":["resolved line","another line"]}]}` + "\n")
	b.WriteString("Use the id of each conflict. An empty lines array means the conflict resolves to nothing.\n\n")
	b.WriteString("Conflicts:\n")
	b.Write(data)
	b.WriteString("\n")
	return b.String()
}

func nonNil(lines []string) []string {
	if lines == nil {
		return []string{}
	}
	return lines
}

// ParseResponse extracts the resolutions from the assistant output. Output around the JSON object (a code
// fence, a sentence) is ignored; ids that were not asked for are rejected.
func ParseResponse(output string, request Request) (map[int][]string, error) {
	start, end := strings.Index(output, "{"), strings.LastIndex(output, "}")
	if start < 0 || end < start {
		return nil, errors.New("answer contains no JSON")
	}
	var answer struct {
		Resolutions []struct {
			ID    int       `json:"id"`
			Lines *[]string `json:"lines"`
		} `json:"resolutions"`
	}
	if err := json.Unmarshal([]byte(output[start:end+1]), &answer); err != nil {
		return nil, fmt.Errorf("answer is not valid JSON: %w", err)
	}
	asked := make(map[int]bool, len(request.Conflicts))
	for _, c := range request.Conflicts {
		asked[c.ID] = true
	}
	result := make(map[int][]string, len(answer.Resolutions))
	for _, resolution := range answer.Resolutions {
		if !asked[resolution.ID] {
			return nil, fmt.Errorf("answer resolves unknown conflict %d", resolution.ID)
		}
		if resolution.Lines == nil {
			return nil, fmt.Errorf("conflict %d has no lines", resolution.ID)
		}
		result[resolution.ID] = *resolution.Lines
	}
	if len(result) == 0 {
		return nil, errors.New("answer resolves no conflict")
	}
	return result, nil
}
