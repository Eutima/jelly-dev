package ui

import "testing"

func TestCommentPrefixFor_ReturnsMarker_WhenFileTypeIsKnown(t *testing.T) {
	for path, want := range map[string]string{
		"main.go": "//", "/x/app.PY": "#", "Dockerfile": "#", "query.sql": "--", "a.json": "", "readme.md": "", "": "",
	} {
		if got := commentPrefixFor(path); got != want {
			t.Errorf("%q: got %q, want %q", path, got, want)
		}
	}
}
