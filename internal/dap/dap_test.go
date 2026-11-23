package dap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/eutima/jelly-dev/internal/jsonframe"
)

// TestHelperAdapter is not a test: it is the fake debug adapter, started by fakeAdapter in a child process (GO_FAKE_DAP=1). It logs the
// commands it gets to the file in GO_FAKE_LOG, answers launch only after configurationDone (as delve does) and stops once the run starts.
func TestHelperAdapter(t *testing.T) {
	if os.Getenv("GO_FAKE_DAP") != "1" {
		t.Skip("helper process")
	}
	tr := jsonframe.New(os.Stdin, os.Stdout)
	logFile, _ := os.OpenFile(os.Getenv("GO_FAKE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	seq := 100
	send := func(m map[string]any) {
		seq++
		m["seq"] = seq
		_ = tr.Write(m)
	}
	respond := func(req message, body any) {
		send(map[string]any{"type": "response", "request_seq": req.Seq, "command": req.Command, "success": true, "body": body})
	}
	launchSeq := 0
	for {
		raw, err := tr.Read()
		if err != nil {
			return
		}
		var req struct {
			message
			Args json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(raw, &req)
		if logFile != nil {
			logFile.WriteString(req.Command + " " + string(req.Args) + "\n")
		}
		switch req.Command {
		case "initialize":
			respond(req.message, map[string]any{"supportsConfigurationDoneRequest": true})
			send(map[string]any{"type": "event", "event": "initialized"})
		case "launch":
			if strings.Contains(string(req.Args), "refuse") { // refused, and no "initialized" follows
				send(map[string]any{"type": "response", "request_seq": req.Seq, "command": "launch", "success": false, "message": "program not found"})
				continue
			}
			launchSeq = req.Seq // answered after configurationDone
			send(map[string]any{"type": "event", "event": "output", "body": map[string]any{"category": "console", "output": "hello\n"}})
		case "setBreakpoints":
			var args struct {
				Breakpoints []struct{ Line int } `json:"breakpoints"`
			}
			_ = json.Unmarshal(req.Args, &args)
			points := []map[string]any{}
			for _, b := range args.Breakpoints {
				points = append(points, map[string]any{"line": b.Line, "verified": b.Line != 99, "message": map[bool]string{true: "", false: "no code"}[b.Line != 99]})
			}
			respond(req.message, map[string]any{"breakpoints": points})
		case "configurationDone":
			respond(req.message, nil)
			send(map[string]any{"type": "response", "request_seq": launchSeq, "command": "launch", "success": true})
			send(map[string]any{"type": "event", "event": "stopped", "body": map[string]any{"reason": "breakpoint", "threadId": 7}})
		case "stackTrace":
			respond(req.message, map[string]any{"stackFrames": []map[string]any{{"id": 1, "name": "main.main", "line": 12, "column": 1, "source": map[string]any{"path": "/p/main.go"}}}})
		case "scopes":
			respond(req.message, map[string]any{"scopes": []map[string]any{{"name": "Locals", "variablesReference": 5}}})
		case "variables":
			respond(req.message, map[string]any{"variables": []map[string]any{{"name": "x", "value": "42", "type": "int"}}})
		case "evaluate":
			respond(req.message, map[string]any{"result": "84", "type": "int"})
		case "next", "stepIn", "stepOut", "continue":
			respond(req.message, nil)
			send(map[string]any{"type": "event", "event": "continued", "body": map[string]any{"threadId": 7}})
			send(map[string]any{"type": "event", "event": "stopped", "body": map[string]any{"reason": "step", "threadId": 7}})
		case "threads":
			respond(req.message, map[string]any{"threads": []map[string]any{{"id": 3, "name": "main"}}})
		case "pause":
			respond(req.message, nil)
			send(map[string]any{"type": "event", "event": "stopped", "body": map[string]any{"reason": "pause", "threadId": 3}})
		case "wrong":
			send(map[string]any{"type": "response", "request_seq": req.Seq, "command": req.Command, "success": false, "message": "no way"})
		case "ask":
			send(map[string]any{"type": "request", "command": "runInTerminal", "arguments": map[string]any{}})
			respond(req.message, nil)
		case "disconnect":
			respond(req.message, nil)
			return
		}
	}
}

func fakeAdapter(t *testing.T) (*Client, string) {
	t.Helper()
	log := t.TempDir() + "/log"
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperAdapter$")
	cmd.Env = append(os.Environ(), "GO_FAKE_DAP=1", "GO_FAKE_LOG="+log)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := NewClient(stdout, stdin, func() error { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait(); return nil })
	t.Cleanup(func() { _ = c.Close() })
	return c, log
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func nextEvent(t *testing.T, s *Session, name string) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("adapter stopped before %s", name)
			}
			if ev.Name == name {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event", name)
		}
	}
}

func TestSession_StartsSendsBreakpointsAndStopsAtThem(t *testing.T) {
	c, log := fakeAdapter(t)
	s := NewSession(c)
	set, err := s.Start(ctxT(t), map[string]any{"request": "launch", "program": "/p"}, map[string][]int{"/p/main.go": {12, 99}})
	if err != nil {
		t.Fatal(err)
	}
	got := set["/p/main.go"]
	if len(got) != 2 || !got[0].Verified || got[1].Verified || got[1].Message != "no code" || got[1].Line != 99 {
		t.Fatalf("breakpoints: %+v", got)
	}
	ev := nextEvent(t, s, "stopped")
	var stop Stop
	_ = json.Unmarshal(ev.Body, &stop)
	if stop.Reason != "breakpoint" || stop.Thread != 7 {
		t.Fatalf("stop: %+v", stop)
	}
	if thread, stopped := s.Stopped(); !stopped || thread != 7 {
		t.Fatalf("Stopped: %d %v", thread, stopped)
	}
	data, _ := os.ReadFile(log)
	order := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		order = append(order, strings.Fields(line)[0])
	}
	if strings.Join(order, ",") != "initialize,launch,setBreakpoints,configurationDone" {
		t.Fatalf("opening sequence: %v", order)
	}
}

func TestSession_InspectsStackVariablesAndEvaluates(t *testing.T) {
	c, _ := fakeAdapter(t)
	s := NewSession(c)
	if _, err := s.Start(ctxT(t), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, s, "stopped")
	frames, err := s.Stack(ctxT(t))
	if err != nil || len(frames) != 1 || frames[0].Path != "/p/main.go" || frames[0].Line != 12 || frames[0].Name != "main.main" {
		t.Fatalf("stack: %+v %v", frames, err)
	}
	scopes, err := s.Scopes(ctxT(t), frames[0].ID)
	if err != nil || len(scopes) != 1 || scopes[0].Reference != 5 {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
	vars, err := s.Variables(ctxT(t), scopes[0].Reference)
	if err != nil || len(vars) != 1 || vars[0].Name != "x" || vars[0].Value != "42" {
		t.Fatalf("variables: %+v %v", vars, err)
	}
	if v, err := s.Evaluate(ctxT(t), frames[0].ID, "x*2"); err != nil || v.Value != "84" {
		t.Fatalf("evaluate: %+v %v", v, err)
	}
}

func TestSession_SteppingNeedsAStoppedProgramAndTracksTheStops(t *testing.T) {
	c, _ := fakeAdapter(t)
	s := NewSession(c)
	if err := s.Next(ctxT(t)); err == nil {
		t.Fatal("stepping before the run must fail")
	}
	if _, err := s.Start(ctxT(t), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, s, "stopped")
	for name, step := range map[string]func(context.Context) error{"next": s.Next, "stepIn": s.StepIn, "stepOut": s.StepOut, "continue": s.Continue} {
		if err := step(ctxT(t)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		nextEvent(t, s, "stopped") // the fake stops again after every step
	}
}

func TestClient_ReportsRefusalsAndAnswersRequestsOfTheAdapter(t *testing.T) {
	c, _ := fakeAdapter(t)
	err := c.Call(ctxT(t), "wrong", nil, nil)
	var refused *ResponseError
	if !asResponseError(err, &refused) || refused.Message != "no way" || refused.Command != "wrong" {
		t.Fatalf("want the refusal of the adapter: %v", err)
	}
	// the fake sends a runInTerminal request before it answers: the client must not hang on it
	if err := c.Call(ctxT(t), "ask", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestClient_FailsPendingCallsWhenTheAdapterStops(t *testing.T) {
	c, _ := fakeAdapter(t)
	s := NewSession(c)
	if err := s.Stop(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctxT(t), "threads", nil, nil); err != ErrClosed {
		t.Fatalf("after Stop calls fail with ErrClosed: %v", err)
	}
	select {
	case _, ok := <-s.Events():
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event channel must close when the adapter has stopped")
	}
}

func asResponseError(err error, target **ResponseError) bool {
	re, ok := err.(*ResponseError)
	*target = re
	return ok
}

func TestSession_StartReturnsTheRefusalOfALaunchThatNeverInitializes(t *testing.T) {
	c, _ := fakeAdapter(t)
	s := NewSession(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.Start(ctx, map[string]any{"program": "refuse"}, nil)
	var refused *ResponseError
	if !asResponseError(err, &refused) || refused.Message != "program not found" {
		t.Fatalf("want the refusal of the launch: %v", err)
	}
}

func TestSession_PauseUsesTheFirstThreadWhenNothingStoppedYet(t *testing.T) {
	c, log := fakeAdapter(t)
	s := NewSession(c)
	if _, err := s.Start(ctxT(t), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, s, "stopped")
	if err := s.Pause(ctxT(t)); err != nil { // thread 7 stopped before, so it is the one to pause
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), `pause {"threadId":7}`) {
		t.Fatalf("pause must name the last stopped thread:\n%s", data)
	}
	fresh, freshLog := fakeAdapter(t)
	if err := NewSession(fresh).Pause(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(freshLog)
	if !strings.Contains(string(data), `pause {"threadId":3}`) {
		t.Fatalf("without a stop, pause names the first thread:\n%s", data)
	}
}

func TestClient_ForgetsACallThatTimedOut(t *testing.T) {
	c, _ := fakeAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.Call(ctx, "silent", nil, nil); err == nil { // the fake never answers unknown commands
		t.Fatal("want the timeout")
	}
	c.mu.Lock()
	left := len(c.pending)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d calls still wait for an answer", left)
	}
}

func TestStart_RunsTheAdapterProcessAndCloseEndsIt(t *testing.T) {
	t.Setenv("GO_FAKE_DAP", "1")
	t.Setenv("GO_FAKE_LOG", t.TempDir()+"/log")
	c, err := Start([]string{os.Args[0], "-test.run=^TestHelperAdapter$"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctxT(t), "initialize", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("a normal Close is not an error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
	if _, err := Start(nil, ""); err == nil {
		t.Fatal("an empty command must fail")
	}
	if _, err := Start([]string{"/no/such/adapter"}, t.TempDir()); err == nil {
		t.Fatal("a missing adapter must fail")
	}
}

func TestDial_ConnectsOverTCPAndGivesUpWhenNothingListens(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no local TCP:", err)
	}
	address := listener.Addr().String()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tr := jsonframe.New(conn, conn)
		raw, err := tr.Read()
		if err != nil {
			return
		}
		var req message
		_ = json.Unmarshal(raw, &req)
		_ = tr.Write(map[string]any{"seq": 1, "type": "response", "request_seq": req.Seq, "command": req.Command, "success": true, "body": map[string]any{"ok": true}})
	}()
	c, err := Dial(address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ OK bool }
	if err := c.Call(ctxT(t), "initialize", nil, &body); err != nil || !body.OK {
		t.Fatalf("%+v %v", body, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctxT(t), "threads", nil, nil); err != ErrClosed {
		t.Fatalf("a deliberate Close leaves no other reason behind: %v", err)
	}
	listener.Close()
	if _, err := Dial(address, 200*time.Millisecond); err == nil {
		t.Fatal("nothing listens any more: Dial must give up")
	}
}

func TestClient_NamesTheReasonWhenTheAdapterSendsGarbage(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	c := NewClient(client, client, client.Close)
	defer c.Close()
	go func() { _, _ = server.Write([]byte("Content-Length: 3\r\n\r\nxyz")) }()
	<-c.Done()
	err := c.Call(ctxT(t), "threads", nil, nil)
	if !errors.Is(err, ErrClosed) || !strings.Contains(err.Error(), "unreadable message") {
		t.Fatalf("want ErrClosed with the reason: %v", err)
	}
}
