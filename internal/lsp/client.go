package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrClosed is returned for requests to a server that has stopped.
var ErrClosed = errors.New("language server stopped")

// requestTimeout is how long a request waits for the answer when the caller gave no deadline.
const requestTimeout = 30 * time.Second

type message struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("language server error %d: %s", e.Code, e.Message)
}

type response struct {
	result json.RawMessage
	err    error
}

type document struct {
	version int
	text    string
}

// Client talks to one language server process.
type Client struct {
	name string
	cmd  *exec.Cmd
	tr   *transport

	mu      sync.Mutex
	nextID  int
	pending map[int]chan response
	docs    map[string]document // open documents by URI
	closed  bool
	done    chan struct{}
}

// Start runs a language server (command is its command line) for a project folder and performs the handshake.
func Start(ctx context.Context, name string, command []string, root string) (*Client, error) {
	if len(command) == 0 {
		return nil, errors.New("empty server command")
	}
	command = expandCommand(command, root)
	cmd := exec.Command(command[0], command[1:]...) // #nosec G204 -- the command comes from the user's own configuration
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	return handshake(ctx, newClient(name, cmd, stdout, stdin), root)
}

func newClient(name string, cmd *exec.Cmd, r io.Reader, w io.Writer) *Client {
	c := &Client{name: name, cmd: cmd, tr: newTransport(r, w), pending: map[int]chan response{}, docs: map[string]document{}, done: make(chan struct{})}
	go c.readLoop()
	return c
}

func handshake(ctx context.Context, c *Client, root string) (*Client, error) {
	params := map[string]any{
		"processId":        nil,
		"rootUri":          PathToURI(root),
		"rootPath":         root,
		"workspaceFolders": []map[string]string{{"uri": PathToURI(root), "name": "project"}},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": false},
				"definition":      map[string]any{"linkSupport": false},
				"references":      map[string]any{},
				"completion": map[string]any{"completionItem": map[string]any{
					"snippetSupport": false, "insertReplaceSupport": true, "documentationFormat": []string{"plaintext"},
				}},
			},
			"workspace": map[string]any{"workspaceFolders": true},
		},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		c.Close()
		return nil, fmt.Errorf("%s: initialize: %w", c.name, err)
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// readLoop dispatches what the server sends: answers to our requests, requests of the server and notifications.
func (c *Client) readLoop() {
	defer c.shutdown()
	for {
		body, err := c.tr.read()
		if err != nil {
			return
		}
		var msg message
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			c.answerServerRequest(msg)
		case msg.Method != "":
			// notifications (diagnostics, logs, progress) are not used yet
		case msg.ID != nil:
			var id int
			if json.Unmarshal(*msg.ID, &id) != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				var err error
				if msg.Error != nil {
					err = msg.Error
				}
				ch <- response{result: msg.Result, err: err}
			}
		}
	}
}

// answerServerRequest replies to a request of the server: servers wait for these answers, and null is a valid one for
// everything this client does not support.
func (c *Client) answerServerRequest(msg message) {
	var result any
	if msg.Method == "workspace/configuration" {
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		result = make([]any, len(params.Items)) // one null per requested section
	}
	_ = c.tr.write(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
}

// shutdown fails every request that is still waiting.
func (c *Client) shutdown() {
	c.mu.Lock()
	c.closed = true
	for id, ch := range c.pending {
		ch <- response{err: ErrClosed}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.tr.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.forget(id)
		return err
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if result != nil && len(res.result) > 0 {
			return json.Unmarshal(res.result, result)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		_ = c.tr.write(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]int{"id": id}})
		return fmt.Errorf("%s: %s: %w", c.name, method, ctx.Err())
	}
}

func (c *Client) forget(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) notify(method string, params any) error {
	return c.tr.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// Sync makes the server's copy of a file equal to text: it opens the file, or sends the change when the text differs
// from what the server has (the whole text is sent).
func (c *Client) Sync(path, languageID, text string) error {
	uri := PathToURI(path)
	c.mu.Lock()
	doc, open := c.docs[uri]
	if open && doc.text == text {
		c.mu.Unlock()
		return nil
	}
	doc.version++
	doc.text = text
	c.docs[uri] = doc
	c.mu.Unlock()
	if !open {
		return c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": doc.version, "text": text,
		}})
	}
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": doc.version},
		"contentChanges": []map[string]string{{"text": text}},
	})
}

// CloseDocument tells the server that a file is no longer open.
func (c *Client) CloseDocument(path string) error {
	uri := PathToURI(path)
	c.mu.Lock()
	_, open := c.docs[uri]
	delete(c.docs, uri)
	c.mu.Unlock()
	if !open {
		return nil
	}
	return c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}})
}

// locations decodes the answer of definition and references requests: null, one Location, a list of Locations or
// a list of LocationLinks.
func locations(raw json.RawMessage) ([]Location, error) {
	trimmed := string(raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var list []json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	} else {
		list = []json.RawMessage{raw}
	}
	out := make([]Location, 0, len(list))
	for _, item := range list {
		var link struct {
			Location
			TargetURI            string `json:"targetUri"`
			TargetSelectionRange Range  `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(item, &link); err != nil {
			return nil, err
		}
		if link.TargetURI != "" { // a LocationLink
			out = append(out, Location{URI: link.TargetURI, Range: link.TargetSelectionRange})
			continue
		}
		out = append(out, link.Location)
	}
	return out, nil
}

func (c *Client) positionRequest(ctx context.Context, method, path string, pos Position, extra map[string]any) ([]Location, error) {
	params := map[string]any{"textDocument": map[string]string{"uri": PathToURI(path)}, "position": pos}
	for key, value := range extra {
		params[key] = value
	}
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return locations(raw)
}

// Definition returns where the symbol at a position is defined.
func (c *Client) Definition(ctx context.Context, path string, pos Position) ([]Location, error) {
	return c.positionRequest(ctx, "textDocument/definition", path, pos, nil)
}

// References returns the uses of the symbol at a position, including its declaration.
func (c *Client) References(ctx context.Context, path string, pos Position) ([]Location, error) {
	return c.positionRequest(ctx, "textDocument/references", path, pos, map[string]any{"context": map[string]bool{"includeDeclaration": true}})
}

// Close stops the server: shutdown, exit, and a kill when it does not leave by itself.
func (c *Client) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.call(ctx, "shutdown", nil, nil)
	_ = c.notify("exit", nil)
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

// expandCommand replaces {root} (the folder the server is started in) and {home} (the user's home folder) in a server command.
func expandCommand(command []string, root string) []string {
	home, _ := os.UserHomeDir()
	replacer := strings.NewReplacer("{root}", filepath.ToSlash(root), "{home}", filepath.ToSlash(home))
	out := make([]string, len(command))
	for i, arg := range command {
		out[i] = replacer.Replace(arg)
	}
	return out
}
