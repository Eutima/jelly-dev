package source

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestWriteClipboard_UsesFirstWorkingTool(t *testing.T) {
	var calls []string
	run := func(_ context.Context, stdin, name string, _ ...string) error {
		calls = append(calls, name+":"+stdin)
		if name == "broken" {
			return errors.New("failed")
		}
		return nil
	}
	look := func(name string) (string, error) {
		if name == "missing" {
			return "", errors.New("not found")
		}
		return "/bin/" + name, nil
	}
	candidates := []clipboardCommand{{"missing", nil}, {"broken", nil}, {"good", nil}, {"unused", nil}}
	if err := writeClipboard(context.Background(), "text", candidates, run, look); err != nil {
		t.Fatal(err)
	}
	if want := []string{"broken:text", "good:text"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
}

func TestWriteClipboard_ReportsFailureAndMissingTools(t *testing.T) {
	look := func(string) (string, error) { return "/bin/x", nil }
	failing := func(context.Context, string, string, ...string) error { return errors.New("denied") }
	if err := writeClipboard(context.Background(), "x", []clipboardCommand{{"a", nil}}, failing, look); err == nil || err.Error() != "write clipboard: a: denied" {
		t.Fatalf("got %v", err)
	}
	none := func(string) (string, error) { return "", errors.New("no") }
	if err := writeClipboard(context.Background(), "x", []clipboardCommand{{"a", nil}}, failing, none); !errors.Is(err, ErrNoClipboardWriter) {
		t.Fatalf("got %v", err)
	}
}
