package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompletionItems_KeepAdditionalEditsAndTheRawItemForResolving(t *testing.T) {
	raw := json.RawMessage(`{"items":[{"label":"signal","data":{"x":1},"additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import { signal } from 'a';\n"}]}]}`)
	items, err := completionItems(raw)
	if err != nil || len(items) != 1 {
		t.Fatalf("one item: %v %v", items, err)
	}
	if len(items[0].Additional) != 1 || !strings.HasPrefix(items[0].Additional[0].NewText, "import { signal }") {
		t.Fatalf("the import edit is kept: %+v", items[0].Additional)
	}
	if !strings.Contains(string(items[0].raw), `"data":{"x":1}`) {
		t.Fatalf("the item is kept as sent, so resolving can send it back: %s", items[0].raw)
	}
}
