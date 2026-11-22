package lsp

import (
	"encoding/json"
	"testing"
)

func TestHoverTextDecodesEveryShapeOfContents(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"null":          {`null`, ""},
		"plain string":  {`{"contents":"adds two numbers"}`, "adds two numbers"},
		"markup":        {`{"contents":{"kind":"markdown","value":"` + "```go\\nfunc Add(a, b int) int\\n```" + `\n\nAdd adds."}}`, "func Add(a, b int) int\n\nAdd adds."},
		"marked string": {`{"contents":{"language":"python","value":"def f()"}}`, "def f()"},
		"list":          {`{"contents":["one",{"language":"go","value":"two"}]}`, "one\n\ntwo"},
		"empty":         {`{"contents":""}`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := hoverText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("hoverText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
