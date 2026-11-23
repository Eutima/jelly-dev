package dap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
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

// stopTimeout is how long Stop waits for the adapter to end the program before it kills the adapter.
const stopTimeout = 3 * time.Second

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
	stopOrder   int64    // when it stopped last, to find the session that stopped most recently
	inspected   *Session // the session the last Stack came from: frame and variable numbers belong to it

	// Some adapters (js-debug for every program, debugpy for sub processes) ask the client to start another session with them for the program
	// that was started: the children. Their events join the events of this session and the commands go to the session that is stopped.
	address     string // where the adapter listens, for the sessions it asks for
	breakpoints map[string][]int
	children    []*Session
	forwarders  sync.WaitGroup
	gone        bool // the adapter has stopped: no more children
}

// stopCounter orders the stops of all sessions.
var stopCounter atomic.Int64

// NewSession wraps a client. The events of the session are the events of the adapter, in order, minus the ones the session handles itself
// ("initialized"); stopped state is updated before the event is delivered.
func NewSession(client *Client) *Session { return newSession(client, client.Address()) }

// newSession is NewSession for an adapter that listens at address (a child session is told where by its parent).
func newSession(client *Client, address string) *Session {
	s := &Session{client: client, events: make(chan Event, eventBuffer), initialized: make(chan struct{}), address: address, breakpoints: map[string][]int{}}
	client.SetRequestHandler(s.answerRequest)
	go s.pump()
	return s
}

// Events delivers what the adapter reports; it is closed when the adapter has stopped.
func (s *Session) Events() <-chan Event { return s.events }

func (s *Session) pump() {
	defer func() {
		s.mu.Lock()
		s.gone = true
		children := slices.Clone(s.children)
		s.mu.Unlock()
		for _, child := range children {
			_ = child.client.Close()
		}
		s.forwarders.Wait() // the children forward into the channel that is closed now
		close(s.events)
	}()
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
			s.stopOrder = stopCounter.Add(1)
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
	return s.active().own()
}

// own is the state of this session alone.
func (s *Session) own() (thread int, stopped bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thread, s.stopped
}

// active is the session the commands go to: the child that stopped most recently, else this session.
func (s *Session) active() *Session {
	s.mu.Lock()
	children := slices.Clone(s.children)
	s.mu.Unlock()
	best, order := s, int64(0)
	for _, child := range children {
		candidate := child.active() // a child may have children of its own
		candidate.mu.Lock()
		stopped, when := candidate.stopped, candidate.stopOrder
		candidate.mu.Unlock()
		if stopped && when > order {
			best, order = candidate, when
		}
	}
	return best
}

// inspectTarget is the session that frames and variables are asked from: the one the last stack came from (its numbers belong to it, even
// when another session has stopped since), else the one that is stopped.
func (s *Session) inspectTarget() *Session {
	s.mu.Lock()
	target := s.inspected
	s.mu.Unlock()
	if target != nil {
		return target
	}
	return s.active()
}

// answerRequest answers what the adapter asks the client to do. Only startDebugging is supported: it starts a child session, for the target
// of the configuration the adapter sends (a "connect" address of its own, else the address this adapter listens on).
func (s *Session) answerRequest(command string, arguments json.RawMessage) (any, error) {
	if command != "startDebugging" {
		return nil, errors.New("not supported by jd")
	}
	var request struct {
		Request       string         `json:"request"`
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil || request.Configuration == nil {
		return nil, errors.New("startDebugging without a configuration")
	}
	address := s.address
	if connect, ok := request.Configuration["connect"].(map[string]any); ok {
		host, _ := connect["host"].(string)
		if port, ok := connect["port"].(float64); ok && host != "" {
			address = net.JoinHostPort(host, strconv.Itoa(int(port)))
		}
	}
	if address == "" {
		return nil, errors.New("this adapter has no address a second session could connect to")
	}
	configuration := request.Configuration
	if request.Request != "" {
		configuration["request"] = request.Request
	}
	s.mu.Lock()
	if s.gone {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	s.forwarders.Add(1) // counted before the answer: the session cannot close its events while the child is still being set up
	snapshot := make(map[string][]int, len(s.breakpoints))
	for path, lines := range s.breakpoints {
		snapshot[path] = slices.Clone(lines)
	}
	s.mu.Unlock()
	go s.runChild(address, configuration, snapshot)
	return nil, nil
}

// childStartTimeout is how long a child session gets to start.
const childStartTimeout = time.Minute

// runChild starts a child session, forwards its events to this session and removes it when it is over.
func (s *Session) runChild(address string, configuration map[string]any, breakpoints map[string][]int) {
	defer s.forwarders.Done()
	client, err := Dial(address, 10*time.Second)
	if err != nil {
		s.emitOutput("stderr", "debug session could not connect: "+err.Error()+"\n")
		return
	}
	child := newSession(client, address)
	s.mu.Lock()
	if s.gone {
		s.mu.Unlock()
		_ = client.Close()
		return
	}
	s.children = append(s.children, child)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.children = slices.DeleteFunc(s.children, func(c *Session) bool { return c == child })
		s.mu.Unlock()
		_ = client.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), childStartTimeout)
	_, err = child.Start(ctx, configuration, breakpoints)
	cancel()
	if err != nil {
		s.emitOutput("stderr", "debug session could not start: "+err.Error()+"\n")
		return
	}
	for ev := range child.Events() {
		if ev.Name == "terminated" || ev.Name == "exited" {
			s.mu.Lock()
			s.children = slices.DeleteFunc(s.children, func(c *Session) bool { return c == child })
			others := len(s.children) > 0 // another child is still alive: this one ending is not the end of the program
			s.mu.Unlock()
			if others {
				continue
			}
		}
		select {
		case s.events <- ev:
		case <-s.client.Done():
			return
		}
	}
}

// emitOutput shows a message of jd in the output of the session.
func (s *Session) emitOutput(category, text string) {
	body, _ := json.Marshal(map[string]string{"category": category, "output": text})
	select {
	case s.events <- Event{Name: "output", Body: body}:
	case <-s.client.Done():
	}
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
	s.mu.Lock()
	if len(lines) == 0 {
		delete(s.breakpoints, path)
	} else {
		s.breakpoints[path] = slices.Clone(lines)
	}
	children := slices.Clone(s.children)
	s.mu.Unlock()
	for _, child := range children { // the program runs in the children: they need the breakpoints as well
		if _, err := child.SetBreakpoints(ctx, path, lines); err != nil {
			s.emitOutput("stderr", "the breakpoints of "+filepath.Base(path)+" could not be set in a debug session: "+err.Error()+"\n")
		}
	}
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
	target := s.active()
	thread, stopped := target.own()
	if !stopped {
		return errNotStopped
	}
	return target.client.Call(ctx, command, map[string]any{"threadId": thread}, nil)
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
	target := s
	if len(s.children) > 0 { // the program runs in the newest child
		target = s.children[len(s.children)-1]
	}
	s.mu.Unlock()
	return target.pauseOwn(ctx)
}

func (s *Session) pauseOwn(ctx context.Context) error {
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
	target := s.active()
	thread, stopped := target.own()
	if !stopped {
		return nil, errNotStopped
	}
	s.mu.Lock()
	s.inspected = target
	s.mu.Unlock()
	var result struct {
		StackFrames []struct {
			Frame
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"stackFrames"`
	}
	if err := target.client.Call(ctx, "stackTrace", map[string]any{"threadId": thread}, &result); err != nil {
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
	err := s.inspectTarget().client.Call(ctx, "scopes", map[string]any{"frameId": frame}, &result)
	return result.Scopes, err
}

// Variables returns the children of a scope or of a variable that has a Reference.
func (s *Session) Variables(ctx context.Context, reference int) ([]Variable, error) {
	var result struct {
		Variables []Variable `json:"variables"`
	}
	err := s.inspectTarget().client.Call(ctx, "variables", map[string]any{"variablesReference": reference}, &result)
	return result.Variables, err
}

// Completion is one suggestion for the text of the debug console. Start and Length (counted in characters of the text that was sent) say
// which part of it the suggestion replaces; Start is -1 when the adapter did not say (the word before the cursor is replaced then).
type Completion struct {
	Label  string
	Text   string // what is inserted; the label when empty
	Start  int
	Length int
}

// Insert is the text a completion puts into the line.
func (c Completion) Insert() string {
	if c.Text != "" {
		return c.Text
	}
	return c.Label
}

// Completions asks the adapter for suggestions for text, with the cursor at column (0-based, in characters), in the context of a frame (a
// negative id: no frame). An adapter that does not complete (no supportsCompletionsRequest) refuses: the error says so.
func (s *Session) Completions(ctx context.Context, frame int, text string, column int) ([]Completion, error) {
	args := map[string]any{"text": text, "column": column + 1, "line": 1} // DAP columns start at 1, as initialize said
	if frame >= 0 {
		args["frameId"] = frame
	}
	var result struct {
		Targets []struct {
			Label  string `json:"label"`
			Text   string `json:"text"`
			Start  *int   `json:"start"`
			Length int    `json:"length"`
		} `json:"targets"`
	}
	if err := s.inspectTarget().client.Call(ctx, "completions", args, &result); err != nil {
		return nil, err
	}
	items := make([]Completion, 0, len(result.Targets))
	for _, target := range result.Targets {
		item := Completion{Label: target.Label, Text: target.Text, Start: -1, Length: target.Length}
		if target.Start != nil {
			item.Start = *target.Start
		}
		items = append(items, item)
	}
	return items, nil
}

// Evaluate runs an expression in the context of a frame (a negative id: no frame; adapters number frames from 0 or from higher numbers) the way a debug console does, and returns its value.
func (s *Session) Evaluate(ctx context.Context, frame int, expression string) (Variable, error) {
	args := map[string]any{"expression": expression, "context": "repl"}
	if frame >= 0 {
		args["frameId"] = frame
	}
	var result struct {
		Result    string `json:"result"`
		Type      string `json:"type"`
		Reference int    `json:"variablesReference"`
	}
	if err := s.inspectTarget().client.Call(ctx, "evaluate", args, &result); err != nil {
		return Variable{}, err
	}
	return Variable{Name: expression, Value: result.Result, Type: result.Type, Reference: result.Reference}, nil
}

// Stop ends the run: the adapter terminates the program it started (or detaches from one it attached to) and the adapter itself is closed.
func (s *Session) Stop(ctx context.Context) error {
	// an adapter that already stopped is fine, and one that hangs must not keep Stop from killing it
	ctx, cancel := context.WithTimeout(ctx, stopTimeout)
	defer cancel()
	s.mu.Lock()
	children := slices.Clone(s.children)
	s.mu.Unlock()
	for _, child := range children {
		_ = child.client.Call(ctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	}
	_ = s.client.Call(ctx, "disconnect", map[string]any{"terminateDebuggee": true}, nil)
	return s.client.Close()
}
