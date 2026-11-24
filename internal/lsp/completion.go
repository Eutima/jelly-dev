package lsp

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// CompletionItem is one suggestion of a language server.
type CompletionItem struct {
	Label      string
	Detail     string
	Kind       int
	InsertText string    // what to insert; the label when empty
	FilterText string    // what the typed text is matched against; the label when empty
	SortText   string    // the server's ordering; the label when empty
	Edit       *TextEdit // replaces a range of the line instead of the typed word, when the server says so
	// Additional are further edits that come with the item, such as the import line of an auto import: a server sends them with
	// the item or, like typescript-language-server, only when the item is resolved (see Manager.ResolveCompletion).
	Additional []TextEdit
	raw        json.RawMessage // the item as the server sent it: resolving sends it back
}

// rawCompletionItem is the item as it is sent.
type rawCompletionItem struct {
	Label            string          `json:"label"`
	Detail           string          `json:"detail"`
	Kind             int             `json:"kind"`
	InsertText       string          `json:"insertText"`
	InsertTextFormat int             `json:"insertTextFormat"`
	FilterText       string          `json:"filterText"`
	SortText         string          `json:"sortText"`
	TextEdit         json.RawMessage `json:"textEdit"`
	Additional       []TextEdit      `json:"additionalTextEdits"`
}

// completionItems decodes the answer of a completion request: null, a list, or a list with "items".
func completionItems(raw json.RawMessage) ([]CompletionItem, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var list []rawCompletionItem
	if trimmed[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	} else {
		var wrapped struct {
			Items []rawCompletionItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil, err
		}
		list = wrapped.Items
	}
	var rawItems []json.RawMessage
	if trimmed[0] == '[' {
		_ = json.Unmarshal(raw, &rawItems)
	} else {
		var wrapped struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(raw, &wrapped)
		rawItems = wrapped.Items
	}
	items := make([]CompletionItem, 0, len(list))
	for i, r := range list {
		if r.Label == "" {
			continue
		}
		item := CompletionItem{Label: r.Label, Detail: r.Detail, Kind: r.Kind, InsertText: r.InsertText, FilterText: r.FilterText, SortText: r.SortText, Additional: r.Additional}
		if i < len(rawItems) {
			item.raw = rawItems[i]
		}
		if r.InsertTextFormat == 2 { // a snippet: keep the text, drop the placeholders
			item.InsertText = SnippetText(item.InsertText)
		}
		if len(r.TextEdit) > 0 && string(r.TextEdit) != "null" {
			var edit struct {
				Range   *Range `json:"range"`
				Insert  *Range `json:"insert"`
				Replace *Range `json:"replace"`
				NewText string `json:"newText"`
			}
			if err := json.Unmarshal(r.TextEdit, &edit); err == nil {
				chosen := edit.Range
				if chosen == nil {
					chosen = edit.Replace
				}
				if chosen == nil {
					chosen = edit.Insert
				}
				if chosen != nil {
					text := edit.NewText
					if r.InsertTextFormat == 2 {
						text = SnippetText(text)
					}
					item.Edit = &TextEdit{Range: *chosen, NewText: text}
				}
			}
		}
		items = append(items, item)
	}
	return items, nil
}

var (
	snippetPlaceholder = regexp.MustCompile(`\$\{\d+:([^}]*)\}`)
	snippetChoice      = regexp.MustCompile(`\$\{\d+\|([^,}|]*)[^}]*\}`)
	snippetTabstop     = regexp.MustCompile(`\$\{?\d+\}?`)
)

// SnippetText turns snippet syntax into plain text: placeholders keep their default text, tab stops disappear, escaped
// characters ($, } and \) stay.
func SnippetText(snippet string) string {
	const dollar, brace, backslash = "\x00", "\x01", "\x02" // escaped characters must not look like syntax
	text := strings.NewReplacer(`\$`, dollar, `\}`, brace, `\\`, backslash).Replace(snippet)
	text = snippetPlaceholder.ReplaceAllString(text, "$1")
	text = snippetChoice.ReplaceAllString(text, "$1")
	text = snippetTabstop.ReplaceAllString(text, "")
	return strings.NewReplacer(dollar, "$", brace, "}", backslash, `\`).Replace(text)
}

// Complete asks the server for suggestions at a position.
func (c *Client) Complete(ctx context.Context, path string, pos Position) ([]CompletionItem, error) {
	params := map[string]any{
		"textDocument": map[string]string{"uri": PathToURI(path)},
		"position":     pos,
		"context":      map[string]int{"triggerKind": 1}, // invoked by the user
	}
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/completion", params, &raw); err != nil {
		return nil, err
	}
	return completionItems(raw)
}

// Complete returns the suggestions at a position (zero-based line, rune column) of text.
func (m *Manager) Complete(ctx context.Context, path, text string, row, col int) ([]CompletionItem, error) {
	c, languageID, err := m.client(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := c.Sync(path, languageID, text); err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	line := ""
	if row >= 0 && row < len(lines) {
		line = lines[row]
	}
	return c.Complete(ctx, path, Position{Line: row, Character: UTF16Column([]rune(line), col)})
}

// Warm starts the server of a file and opens the file in it. A TypeScript server loads the project (and the names that can be imported)
// after a file is opened, which takes seconds in a big project: warming at the time the editor opens the file means the first completion
// does not come before the server knows the names of other modules.
func (m *Manager) Warm(ctx context.Context, path, text string) error {
	c, languageID, err := m.client(ctx, path)
	if err != nil {
		return err
	}
	return c.Sync(path, languageID, text)
}

// ResolveCompletion asks the server for the rest of an item: a server such as typescript-language-server sends the edits of an auto
// import (the import line) only for the item that was chosen. An item the server cannot resolve comes back as it was.
func (m *Manager) ResolveCompletion(ctx context.Context, path string, item CompletionItem) (CompletionItem, error) {
	if len(item.raw) == 0 {
		return item, nil
	}
	c, _, err := m.client(ctx, path)
	if err != nil {
		return item, err
	}
	var resolved rawCompletionItem
	if err := c.call(ctx, "completionItem/resolve", item.raw, &resolved); err != nil {
		return item, err
	}
	item.Additional = append(item.Additional, resolved.Additional...)
	return item, nil
}

// kindNames are the short names of the completion item kinds of the protocol.
var kindNames = map[int]string{
	1: "text", 2: "method", 3: "func", 4: "ctor", 5: "field", 6: "var", 7: "class", 8: "iface", 9: "module", 10: "prop",
	11: "unit", 12: "value", 13: "enum", 14: "keyword", 15: "snippet", 16: "color", 17: "file", 18: "ref", 19: "folder",
	20: "member", 21: "const", 22: "struct", 23: "event", 24: "op", 25: "type",
}

// KindName is a short name for a completion item kind ("" when unknown).
func KindName(kind int) string { return kindNames[kind] }
