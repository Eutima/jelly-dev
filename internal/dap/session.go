package dap

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Breakpoint is a line the program should stop at (1-based, as the user sees it).
type Breakpoint struct {
	Line     int
	Verified bool   // the adapter found code there
	Message  string // why it did not
}

// Frame is one entry of the call stack.
type Frame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"-"`
	Line   int    `json:"line"` // 1-based
	Column int    `json:"column"`
}

// Scope is a group of variables of a frame (locals, globals).
type Scope struct {
	Name      string `json:"name"`
	Reference int    `json:"variablesReference"`
}

// Variable is a name with a value; a Reference above zero means it has children that Variables can fetch.
type Variable struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Type      string `json:"type"`
	Reference int    `json:"variablesReference"`
}

// Stop describes why the program stopped.
type Stop struct {
	Reason string `json:"reason"` // breakpoint, step, pause, exception ...
	Thread int    `json:"threadId"`
	Text   string `json:"text"`
}

// Session is one debugging run: the adapter, the state of the program, and the commands that control it.
type Session struct {
	client *Client
	events chan Event

	mu          sync.Mutex
	lastThread  int // the thread that stopped last, kept while the program runs (Pause needs one)
	thread      int // the thread that is stopped, 0 while the program runs
	stopped     bool
	initialized chan struct{}
	initOnce    sync.Once
}

// NewSession wraps a client. The events of the session are the events of the adapter, in order, minus the ones the session handles itself
// ("initialized"); stopped state is updated before the event is delivered.
func NewSession(client *Client) *Session {
	s := &Session{client: client, events: make(chan Event, eventBuffer), initialized: make(chan struct{})}
	go s.pump()
	return s
}

// Events delivers what the adapter reports; it is closed when the adapter has stopped.
func (s *Session) Events() <-chan Event { return s.events }

func (s *Session) pump() {
	defer close(s.events)
	for ev := range s.client.Events() {
		switch ev.Name {
		case "initialized":
			s.initOnce.Do(func() { close(s.initialized) })
			continue
		case "stopped":
			var stop Stop
			_ = json.Unmarshal(ev.Body, &stop)
			s.mu.Lock()
			s.thread, s.lastThread, s.stopped = stop.Thread, stop.Thread, true
			s.mu.Unlock()
		case "continued", "terminated", "exited":
			s.mu.Lock()
			s.thread, s.stopped = 0, false
			s.mu.Unlock()
		}
		select {
		case s.events <- ev:
		case <-s.client.Done():
			return
		}
	}
}

// Stopped tells whether the program is waiting at a stop, and on which thread.
func (s *Session) Stopped() (thread int, stopped bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread, s.stopped
}

// Start runs the opening sequence: initialize, launch (arguments are the launch configuration of the adapter), send the breakpoints
// (path to lines) and let the program run. The adapter answers launch only after configurationDone, so it is not waited for earlier.
func (s *Session) Start(ctx context.Context, launch map[string]any, breakpoints map[string][]int) (map[string][]Breakpoint, error) {
	if err := s.client.Call(ctx, "initialize", map[string]any{
		"clientID": "jd", "clientName": "Jelly Dev", "adapterID": "jd", "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path",
	}, nil); err != nil {
		return nil, err
	}
	command := "launch"
	if request, _ := launch["request"].(string); request == "attach" {
		command = "attach"
	}
	launchSeq, launched, err := s.client.send(command, launch)
	if err != nil {
		return nil, err
	}
	// an adapter that refuses the launch never sends "initialized": the refusal ends the wait
	launchAnswered := false
	for ready := false; !ready; {
		select {
		case <-s.initialized:
			ready = true
		case res := <-launched:
			if res.err != nil {
				return nil, res.err
			}
			launchAnswered, launched = true, nil // a nil channel is never ready again
		case <-s.client.Done():
			return nil, ErrClosed
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	verified := map[string][]Breakpoint{}
	for path, lines := range breakpoints {
		set, err := s.SetBreakpoints(ctx, path, lines)
		if err != nil {
			return nil, err
		}
		verified[path] = set
	}
	if err := s.client.Call(ctx, "configurationDone", map[string]any{}, nil); err != nil {
		return nil, err
	}
	if !launchAnswered {
		if err := s.client.wait(ctx, launchSeq, launched, nil); err != nil {
			return nil, err
		}
	}
	return verified, nil
}

// SetBreakpoints replaces the breakpoints of one file (an empty list clears them).
func (s *Session) SetBreakpoints(ctx context.Context, path string, lines []int) ([]Breakpoint, error) {
	points := make([]map[string]int, 0, len(lines))
	for _, line := range lines {
		points = append(points, map[string]int{"line": line})
	}
	var result struct {
		Breakpoints []struct {
			Line     int    `json:"line"`
			Verified bool   `json:"verified"`
			Message  string `json:"message"`
		} `json:"breakpoints"`
	}
	if err := s.client.Call(ctx, "setBreakpoints", map[string]any{"source": map[string]string{"path": path}, "breakpoints": points}, &result); err != nil {
		return nil, err
	}
	set := make([]Breakpoint, 0, len(result.Breakpoints))
	for i, b := range result.Breakpoints {
		if b.Line == 0 && i < len(lines) {
			b.Line = lines[i]
		}
		set = append(set, Breakpoint{Line: b.Line, Verified: b.Verified, Message: b.Message})
	}
	return set, nil
}

var errNotStopped = errors.New("the program is not stopped")

func (s *Session) control(ctx context.Context, command string) error {
	thread, stopped := s.Stopped()
	if !stopped {
		return errNotStopped
	}
	return s.client.Call(ctx, command, map[string]any{"threadId": thread}, nil)
}

// Continue lets the stopped program run on.
func (s *Session) Continue(ctx context.Context) error { return s.control(ctx, "continue") }

// Next steps over the current line; StepIn steps into a call; StepOut runs until the function returns.
func (s *Session) Next(ctx context.Context) error    { return s.control(ctx, "next") }
func (s *Session) StepIn(ctx context.Context) error  { return s.control(ctx, "stepIn") }
func (s *Session) StepOut(ctx context.Context) error { return s.control(ctx, "stepOut") }

// Pause stops a running program.
func (s *Session) Pause(ctx context.Context) error {
	s.mu.Lock()
	thread := s.lastThread
	s.mu.Unlock()
	if thread == 0 { // nothing has stopped yet: pause the first thread the adapter knows
		var result struct {
			Threads []struct {
				ID int `json:"id"`
			} `json:"threads"`
		}
		if err := s.client.Call(ctx, "threads", map[string]any{}, &result); err != nil {
			return err
		}
		if len(result.Threads) == 0 {
			return errors.New("the program has no thread to pause")
		}
		thread = result.Threads[0].ID
	}
	return s.client.Call(ctx, "pause", map[string]any{"threadId": thread}, nil)
}

// Stack returns the call stack of the stopped thread, innermost first.
func (s *Session) Stack(ctx context.Context) ([]Frame, error) {
	thread, stopped := s.Stopped()
	if !stopped {
		return nil, errNotStopped
	}
	var result struct {
		StackFrames []struct {
			Frame
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"stackFrames"`
	}
	if err := s.client.Call(ctx, "stackTrace", map[string]any{"threadId": thread}, &result); err != nil {
		return nil, err
	}
	frames := make([]Frame, 0, len(result.StackFrames))
	for _, f := range result.StackFrames {
		f.Frame.Path = f.Source.Path
		frames = append(frames, f.Frame)
	}
	return frames, nil
}

// Scopes returns the variable groups of a frame.
func (s *Session) Scopes(ctx context.Context, frame int) ([]Scope, error) {
	var result struct {
		Scopes []Scope `json:"scopes"`
	}
	err := s.client.Call(ctx, "scopes", map[string]any{"frameId": frame}, &result)
	return result.Scopes, err
}

// Variables returns the children of a scope or of a variable that has a Reference.
func (s *Session) Variables(ctx context.Context, reference int) ([]Variable, error) {
	var result struct {
		Variables []Variable `json:"variables"`
	}
	err := s.client.Call(ctx, "variables", map[string]any{"variablesReference": reference}, &result)
	return result.Variables, err
}

// Evaluate runs an expression in the context of a frame (0: no frame) the way a debug console does, and returns its value.
func (s *Session) Evaluate(ctx context.Context, frame int, expression string) (Variable, error) {
	args := map[string]any{"expression": expression, "context": "repl"}
	if frame != 0 {
		args["frameId"] = frame
	}
	var result struct {
		Result    string `json:"result"`
		Type      string `json:"type"`
		Reference int    `json:"variablesReference"`
	}
	if err := s.client.Call(ctx, "evaluate", args, &result); err != nil {
		return Variable{}, err
	}
	return Variable{Name: expression, Value: result.Result, Type: result.Type, Reference: result.Reference}, nil
}

// Stop ends the run: the adapter terminates the program it started (or detaches from one it attached to) and the adapter itself is closed.
func (s *Session) Stop(ctx context.Context) error {
	_ = s.client.Call(ctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil) // an adapter that already stopped is fine
	return s.client.Close()
}
