package lsp

import (
	"context"
	"encoding/json"
	"strings"
)

// hoverText decodes the answer of a hover request into plain text: null, a string, a MarkedString, a MarkupContent or a list of
// them. Markdown code fences are dropped, the text inside stays.
func hoverText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var hover struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return ""
	}
	return plainDoc(markedText(hover.Contents))
}

// markedText joins the parts of the contents of a hover.
func markedText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var text string
		_ = json.Unmarshal(raw, &text)
		return text
	case '[':
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return ""
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if text := strings.TrimSpace(markedText(part)); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n\n")
	}
	var object struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(raw, &object) // a MarkedString {language, value} and a MarkupContent {kind, value} both carry the text in value
	return object.Value
}

// plainDoc drops the fence lines of markdown code blocks and the blank lines at both ends.
func plainDoc(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// Hover returns the documentation of the symbol at a position ("" when the server has none).
func (c *Client) Hover(ctx context.Context, path string, pos Position) (string, error) {
	c.waitReady(ctx)
	params := map[string]any{"textDocument": map[string]string{"uri": PathToURI(path)}, "position": pos}
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/hover", params, &raw); err != nil {
		return "", err
	}
	return hoverText(raw), nil
}

// Hover returns the documentation of the symbol at a position (zero-based line, rune column) of text.
func (m *Manager) Hover(ctx context.Context, path, text string, row, col int) (string, error) {
	c, languageID, err := m.client(ctx, path)
	if err != nil {
		return "", err
	}
	if err := c.Sync(path, languageID, text); err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	line := ""
	if row >= 0 && row < len(lines) {
		line = lines[row]
	}
	return c.Hover(ctx, path, Position{Line: row, Character: UTF16Column([]rune(line), col)})
}
