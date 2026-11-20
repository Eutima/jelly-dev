package source

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func found(string) (string, error)    { return "/bin/x", nil }
func notFound(string) (string, error) { return "", errors.New("not found") }

func TestReadClipboard_ReturnsText_WhenFirstToolWorks(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) { return []byte("hello\n"), nil }
	got, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, run, found)
	if err != nil || got != "hello\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboard_FallsBackToNextTool_WhenFirstFails(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "a" {
			return nil, errors.New("boom")
		}
		return []byte("from b"), nil
	}
	got, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}, {"b", nil}}, run, found)
	if err != nil || got != "from b" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboard_SkipsMissingTools_WhenNotOnPath(t *testing.T) {
	lookPath := func(name string) (string, error) {
		if name == "missing" {
			return notFound(name)
		}
		return found(name)
	}
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) { return []byte(name), nil }
	got, err := readClipboard(context.Background(), []clipboardCommand{{"missing", nil}, {"present", nil}}, run, lookPath)
	if err != nil || got != "present" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboard_ReturnsNoToolError_WhenNothingInstalled(t *testing.T) {
	_, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, nil, notFound)
	if !errors.Is(err, ErrNoClipboardTool) {
		t.Fatalf("err %v", err)
	}
}

func TestReadClipboard_ReturnsToolError_WhenAllToolsFail(t *testing.T) {
	boom := errors.New("boom")
	run := func(context.Context, string, ...string) ([]byte, error) { return nil, boom }
	_, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, run, found)
	if !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

func TestReadClipboard_ReturnsEmptyError_WhenClipboardBlank(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte(" \r\n"), nil }
	_, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, run, found)
	if !errors.Is(err, ErrEmptyClipboard) {
		t.Fatalf("err %v", err)
	}
}

func TestReadClipboard_RejectsBinary_WhenClipboardHasNulBytes(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("a\x00b"), nil }
	_, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, run, found)
	if !errors.Is(err, ErrBinary) {
		t.Fatalf("err %v", err)
	}
}

func TestReadClipboard_StripsByteOrderMark_WhenOutputStartsWithBOM(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("\ufeffhello"), nil }
	got, err := readClipboard(context.Background(), []clipboardCommand{{"a", nil}}, run, found)
	if err != nil || got != "hello" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboard_ReturnsEmptyError_WhenWlPasteReportsNothingCopied(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("exit status 1: Nothing is copied")
	}
	_, err := readClipboard(context.Background(), []clipboardCommand{{"wl-paste", nil}, {"xclip", nil}}, run, found)
	if !errors.Is(err, ErrEmptyClipboard) {
		t.Fatalf("err %v", err)
	}
}

func TestExecRunner_ReturnsStderrText_WhenCommandFails(t *testing.T) {
	_, err := execRunner(context.Background(), "sh", "-c", "echo broken >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err %v", err)
	}
}

func TestClipboardCandidates_ListsAtLeastOneTool_OnThisSystem(t *testing.T) {
	if len(clipboardCandidates()) == 0 {
		t.Fatal("no candidates")
	}
}
