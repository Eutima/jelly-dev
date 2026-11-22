package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrCannotRename is returned when the language server does not rename symbols.
var ErrCannotRename = errors.New("the language server cannot rename symbols")

// FileEdit is the text edits of one file.
type FileEdit struct {
	Path  string
	Edits []TextEdit
}

// FileRename moves a file.
type FileRename struct{ Old, New string }

// WorkspaceEdit is what a rename changes: text edits (paths are those before the renames) and file renames, which come last.
type WorkspaceEdit struct {
	Files   []FileEdit
	Renames []FileRename
}

// Empty reports whether the edit changes nothing.
func (e WorkspaceEdit) Empty() bool { return len(e.Files) == 0 && len(e.Renames) == 0 }

// EditCount is the number of text edits of all files.
func (e WorkspaceEdit) EditCount() int {
	count := 0
	for _, file := range e.Files {
		count += len(file.Edits)
	}
	return count
}

// workspaceEditJSON is the protocol form: `changes` (a map by URI) or `documentChanges` (text edits and resource operations).
type workspaceEditJSON struct {
	Changes         map[string][]TextEdit `json:"changes"`
	DocumentChanges []json.RawMessage     `json:"documentChanges"`
}

// ParseWorkspaceEdit decodes the answer of a rename request. Creating and deleting files is refused: a rename never needs it,
// and an edit that deletes files is not applied without the user seeing it.
func ParseWorkspaceEdit(raw json.RawMessage) (WorkspaceEdit, error) {
	var edit WorkspaceEdit
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return edit, nil
	}
	var wire workspaceEditJSON
	if err := json.Unmarshal(raw, &wire); err != nil {
		return edit, fmt.Errorf("unreadable workspace edit: %w", err)
	}
	add := func(uri string, edits []TextEdit) error {
		path, ok := URIToPath(uri)
		if !ok {
			return fmt.Errorf("the edit changes %s, which is not a file", uri)
		}
		for i := range edit.Files {
			if edit.Files[i].Path == path {
				edit.Files[i].Edits = append(edit.Files[i].Edits, edits...)
				return nil
			}
		}
		edit.Files = append(edit.Files, FileEdit{Path: path, Edits: edits})
		return nil
	}
	for uri, edits := range wire.Changes {
		if err := add(uri, edits); err != nil {
			return WorkspaceEdit{}, err
		}
	}
	for _, item := range wire.DocumentChanges {
		var change struct {
			Kind         string `json:"kind"`
			OldURI       string `json:"oldUri"`
			NewURI       string `json:"newUri"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		}
		if err := json.Unmarshal(item, &change); err != nil {
			return WorkspaceEdit{}, fmt.Errorf("unreadable workspace edit: %w", err)
		}
		switch change.Kind {
		case "":
			if err := add(change.TextDocument.URI, change.Edits); err != nil {
				return WorkspaceEdit{}, err
			}
		case "rename":
			oldPath, okOld := URIToPath(change.OldURI)
			newPath, okNew := URIToPath(change.NewURI)
			if !okOld || !okNew {
				return WorkspaceEdit{}, errors.New("the edit renames something that is not a file")
			}
			edit.Renames = append(edit.Renames, FileRename{Old: oldPath, New: newPath})
		default:
			return WorkspaceEdit{}, fmt.Errorf("the language server wants to %s a file: not supported", change.Kind)
		}
	}
	return edit, nil
}

func methodNotFound(err error) bool {
	var rpc *rpcError
	return errors.As(err, &rpc) && rpc.Code == -32601
}

// Rename asks the server to rename the symbol at a position.
func (c *Client) Rename(ctx context.Context, path string, pos Position, newName string) (WorkspaceEdit, error) {
	c.waitReady(ctx)
	params := map[string]any{"textDocument": map[string]string{"uri": PathToURI(path)}, "position": pos, "newName": newName}
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/rename", params, &raw); err != nil {
		if methodNotFound(err) {
			return WorkspaceEdit{}, ErrCannotRename
		}
		return WorkspaceEdit{}, err
	}
	return ParseWorkspaceEdit(raw)
}

// WillRenameFile asks the server which edits a file move needs (imports that name the file). A server that does not know the
// request answers with no edits.
func (c *Client) WillRenameFile(ctx context.Context, oldPath, newPath string) (WorkspaceEdit, error) {
	c.waitReady(ctx)
	params := map[string]any{"files": []map[string]string{{"oldUri": PathToURI(oldPath), "newUri": PathToURI(newPath)}}}
	var raw json.RawMessage
	if err := c.call(ctx, "workspace/willRenameFiles", params, &raw); err != nil {
		if methodNotFound(err) {
			return WorkspaceEdit{}, nil
		}
		return WorkspaceEdit{}, err
	}
	return ParseWorkspaceEdit(raw)
}

// Rename renames the symbol at a position (zero-based line, rune column) of text; text is what the editor holds.
func (m *Manager) Rename(ctx context.Context, path, text string, row, col int, newName string) (WorkspaceEdit, error) {
	c, languageID, err := m.client(ctx, path)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if err := c.Sync(path, languageID, text); err != nil {
		return WorkspaceEdit{}, err
	}
	lines := strings.Split(text, "\n")
	line := ""
	if row >= 0 && row < len(lines) {
		line = lines[row]
	}
	return c.Rename(ctx, path, Position{Line: row, Character: UTF16Column([]rune(line), col)}, newName)
}

// RenameFile returns the edits that keep the code working when a file moves (imports). There are none when no server knows the
// file type or none supports it; the move itself is made by the caller.
func (m *Manager) RenameFile(ctx context.Context, oldPath, newPath string) (WorkspaceEdit, error) {
	c, languageID, err := m.client(ctx, oldPath)
	if err != nil {
		if errors.Is(err, ErrNoServer) {
			return WorkspaceEdit{}, nil
		}
		return WorkspaceEdit{}, err
	}
	if data, err := os.ReadFile(oldPath); err == nil { // #nosec G304 -- a file of the user's own project
		if err := c.Sync(oldPath, languageID, string(data)); err != nil {
			return WorkspaceEdit{}, err
		}
	}
	return c.WillRenameFile(ctx, oldPath, newPath)
}
