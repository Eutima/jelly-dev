// Package dap is a client of the Debug Adapter Protocol: it starts a debug adapter (delve, debugpy, js-debug), sets breakpoints, runs the
// program and steps through it, and reads the stack, the variables and the result of expressions.
package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/eutima/jelly-dev/internal/jsonframe"
)

// ErrClosed is returned for requests to an adapter that has stopped.
var ErrClosed = errors.New("debug adapter stopped")

// requestTimeout is how long a request waits for the answer when the caller gave no deadline.
const requestTimeout = 30 * time.Second

// eventBuffer is how many events wait for the reader before the read loop does.
const eventBuffer = 256

type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success"`
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Arguments  any             `json:"arguments,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Event is something the adapter reports: stopped, continued, output, terminated, exited, breakpoint, initialized ...
type Event struct {
	Name string
	Body json.RawMessage
}

// ResponseError is an adapter that refused a request.
type ResponseError struct{ Command, Message string }

func (e *ResponseError) Error() string {
	return fmt.Sprintf("debug adapter refused %s: %s", e.Command, e.Message)
}

type response struct {
	body json.RawMessage
	err  error
}

// Client talks to one debug adapter.
type Client struct {
	tr     *jsonframe.Transport
	closer func() error // ends the adapter: kills the process or closes the connection
	once   sync.Once
	ended  chan struct{} // closed when the read loop has finished
	err    error         // what closer returned
	events chan Event
	done   chan struct{}

	mu      sync.Mutex
	nextSeq int
	pending map[int]chan response
	closed  bool
	closing bool  // Close has begun: the read loop failing now is the expected end
	cause   error // why the read loop ended, when it was not asked to
}

// NewClient talks over a connection that already exists; closer is called by Close (may be nil).
func NewClient(r io.Reader, w io.Writer, closer func() error) *Client {
	c := &Client{tr: jsonframe.New(r, w), closer: closer, events: make(chan Event, eventBuffer), done: make(chan struct{}), ended: make(chan struct{}), pending: map[int]chan response{}}
	go c.readLoop()
	return c
}

// Start runs an adapter that talks over its standard input and output.
func Start(command []string, dir string) (*Client, error) {
	if len(command) == 0 {
		return nil, errors.New("empty adapter command")
	}
	cmd := exec.Command(command[0], command[1:]...) // #nosec G204 -- the command comes from the user's own configuration
	cmd.Dir = dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var client *Client
	client = NewClient(stdout, stdin, func() error {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		// Wait closes the pipe the read loop reads from: let the loop read what is left first (but do not hang on a grandchild that keeps
		// the pipe open)
		select {
		case <-client.ended:
		case <-time.After(2 * time.Second):
		}
		var exit *exec.ExitError
		if err := cmd.Wait(); err != nil && !errors.As(err, &exit) { // an exit status is the kill Close asked for
			return err
		}
		return nil
	})
	return client, nil
}

// Dial connects to an adapter that listens on a TCP address (delve, js-debug).
func Dial(address string, timeout time.Duration) (*Client, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			return NewClient(conn, conn, conn.Close), nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connect to the debug adapter at %s: %w", address, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Events delivers the events of the adapter in order; the channel is closed when the adapter has stopped. The caller must keep reading it:
// when its buffer is full the read loop waits, and with it the responses to every request.
func (c *Client) Events() <-chan Event { return c.events }

// Done is closed when the adapter has stopped.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) readLoop() {
	defer close(c.events) // only this goroutine sends, so only it may close
	defer close(c.ended)
	defer c.shutdown()
	for {
		body, err := c.tr.Read()
		if err != nil {
			c.fail(err)
			return
		}
		var msg message
		if err := json.Unmarshal(body, &msg); err != nil {
			c.fail(fmt.Errorf("unreadable message: %w", err)) // a broken protocol: do not guess what comes next
			return
		}
		switch msg.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[msg.RequestSeq]
			delete(c.pending, msg.RequestSeq)
			c.mu.Unlock()
			if ch == nil {
				continue
			}
			if msg.Success {
				ch <- response{body: msg.Body}
			} else {
				ch <- response{err: &ResponseError{Command: msg.Command, Message: msg.Message}}
			}
		case "event":
			select {
			case c.events <- Event{Name: msg.Event, Body: msg.Body}:
			case <-c.done:
				return
			}
		case "request":
			// a request of the adapter (runInTerminal, startDebugging): not supported yet, say so instead of leaving it waiting
			go c.refuse(msg) // not in the read loop: writing there could deadlock with an adapter that is writing to us
		}
	}
}

// fail remembers why the adapter is gone, unless Close ended it.
func (c *Client) fail(cause error) {
	c.mu.Lock()
	if !c.closed && !c.closing && !errors.Is(cause, io.EOF) {
		c.cause = cause
	}
	c.mu.Unlock()
}

// closedError is what a request gets once the adapter is gone: ErrClosed, with the reason when there is one. Call with c.mu held.
func (c *Client) closedError() error {
	if c.cause != nil {
		return fmt.Errorf("%w: %v", ErrClosed, c.cause)
	}
	return ErrClosed
}

func (c *Client) refuse(msg message) {
	c.mu.Lock()
	c.nextSeq++
	seq := c.nextSeq
	c.mu.Unlock()
	_ = c.tr.Write(message{Seq: seq, Type: "response", RequestSeq: msg.Seq, Command: msg.Command, Success: false, Message: "not supported by jd"})
}

func (c *Client) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	pending := c.pending
	c.pending = map[int]chan response{}
	close(c.done)
	err := c.closedError()
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- response{err: err}
	}
}

// Close ends the adapter.
func (c *Client) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		if c.closer != nil {
			c.err = c.closer()
		}
		c.shutdown()
	})
	return c.err
}

// send writes a request and returns the channel its response arrives on.
func (c *Client) send(command string, arguments any) (int, chan response, error) {
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.closed {
		err := c.closedError()
		c.mu.Unlock()
		return 0, nil, err
	}
	c.nextSeq++
	seq := c.nextSeq
	c.pending[seq] = ch
	c.mu.Unlock()
	if err := c.tr.Write(message{Seq: seq, Type: "request", Command: command, Arguments: arguments}); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return 0, nil, err
	}
	return seq, ch, nil
}

// wait returns the answer of a request, or an error when the context ends or the adapter stops first.
func (c *Client) wait(ctx context.Context, seq int, ch chan response, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if result != nil && len(res.body) > 0 {
			return json.Unmarshal(res.body, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, seq) // a late answer has nobody to go to
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Call sends a request and waits for the response; result (a pointer, may be nil) receives its body.
func (c *Client) Call(ctx context.Context, command string, arguments, result any) error {
	seq, ch, err := c.send(command, arguments)
	if err != nil {
		return err
	}
	return c.wait(ctx, seq, ch, result)
}
