package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	serveFake(jsonframe.New(os.Stdin, os.Stdout), false)
}

// TestHelperListeningAdapter is the same fake adapter, but it listens on a TCP port and prints where, as delve does.
func TestHelperListeningAdapter(t *testing.T) {
	if os.Getenv("GO_FAKE_DAP") != "1" {
		t.Skip("helper process")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("DAP server listening at: " + listener.Addr().String())
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	serveFake(jsonframe.New(conn, conn), false)
}

// TestHelperTreeAdapter is a fake adapter like js-debug: the first connection is the parent session, which asks the client for a child session
// (startDebugging) and has no program of its own; every later connection is a child that runs the program.
func TestHelperTreeAdapter(t *testing.T) {
	if os.Getenv("GO_FAKE_DAP") != "1" {
		t.Skip("helper process")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("Debug server listening at " + listener.Addr().String())
	for first := true; ; first = false {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go serveFake(jsonframe.New(conn, conn), first)
	}
}

// serveFake answers like a debug adapter. As a parent it starts no program: it asks for a child session once the configuration is done and
// refuses the commands that need a program.
func serveFake(tr *jsonframe.Transport, parent bool) {
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
		if parent && strings.Contains(" stackTrace next stepIn stepOut continue scopes variables evaluate ", " "+req.Command+" ") {
			send(map[string]any{"type": "response", "request_seq": req.Seq, "command": req.Command, "success": false, "message": "the parent has no program"})
			continue
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
			if parent {
				send(map[string]any{"type": "request", "command": "startDebugging", "arguments": map[string]any{"request": "launch", "configuration": map[string]any{"type": "child", "name": "child"}}})
				continue
			}
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

func fakeListeningAdapter(t *testing.T) Adapter {
	t.Helper()
	t.Setenv("GO_FAKE_DAP", "1")
	t.Setenv("GO_FAKE_LOG", t.TempDir()+"/log")
	return Adapter{
		Name: "fake", Command: []string{os.Args[0], "-test.run=^TestHelperListeningAdapter$"}, Listens: true, Extensions: []string{".fake"},
		Launch: func(file, root string) map[string]any { return map[string]any{"request": "launch", "program": file} },
	}
}

func lookPathOK(string) (string, error) { return "/bin/x", nil }

func TestDebug_StartsAnAdapterThatListensAndRunsToTheBreakpoint(t *testing.T) {
	adapter := fakeListeningAdapter(t)
	root := t.TempDir()
	session, verified, err := Debug(ctxT(t), []Adapter{adapter}, lookPathOK, root+"/main.fake", root, map[string][]int{root + "/main.fake": {12}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(context.Background())
	if got := verified[root+"/main.fake"]; len(got) != 1 || !got[0].Verified {
		t.Fatalf("breakpoints: %+v", got)
	}
	nextEvent(t, session, "stopped")
}

func TestDebug_SaysWhyItCannotStart(t *testing.T) {
	adapter := fakeListeningAdapter(t)
	root := t.TempDir()
	if _, _, err := Debug(ctxT(t), []Adapter{adapter}, lookPathOK, root+"/main.txt", root, nil); err != ErrNoAdapter {
		t.Fatalf("an unknown file type: %v", err)
	}
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	_, _, err := Debug(ctxT(t), []Adapter{adapter}, missing, root+"/main.fake", root, nil)
	var notInstalled *NotInstalledError
	if !errors.As(err, &notInstalled) || notInstalled.Adapter != "fake" || !strings.Contains(err.Error(), "jd install-lsp debug") {
		t.Fatalf("a missing adapter: %v", err)
	}
	silent := Adapter{Name: "silent", Command: []string{os.Args[0], "-test.run=^$"}, Listens: true, Extensions: []string{".s"}}
	if _, _, err := Debug(ctxT(t), []Adapter{silent}, lookPathOK, root+"/a.s", root, nil); err == nil || !strings.Contains(err.Error(), "before it said where it listens") {
		t.Fatalf("an adapter that never says where it listens: %v", err)
	}
}

func TestDelve_LaunchesTheFolderOfAFileAndTestFilesAsTests(t *testing.T) {
	adapter, ok := AdapterFor(DefaultAdapters(), "/proj/cmd/app/Main.GO")
	if !ok || adapter.Name != "delve" {
		t.Fatalf("a Go file belongs to delve: %+v %v", adapter, ok)
	}
	if got := adapter.Launch("/proj/cmd/app/main.go", "/proj"); got["mode"] != "debug" || got["program"] != "/proj/cmd/app" || got["cwd"] != "/proj" || got["request"] != "launch" {
		t.Fatalf("program: %v", got)
	}
	if got := adapter.Launch("/proj/pkg/x_test.go", "/proj"); got["mode"] != "test" || got["program"] != "/proj/pkg" {
		t.Fatalf("test: %v", got)
	}
	if _, ok := AdapterFor(DefaultAdapters(), "/proj/readme.md"); ok {
		t.Fatal("markdown is not debugged")
	}
}

// TestDelve_DebugsARealProgram runs the real delve against a tiny program; it is skipped where delve is not installed.
func TestDelve_DebugsARealProgram(t *testing.T) {
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv is not installed")
	}
	root := t.TempDir()
	source := "package main\n\nfunc main() {\n\tx := 21\n\ty := x * 2\n\tprintln(y)\n}\n"
	file := root + "/main.go"
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/go.mod", []byte("module demo\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	session, verified, err := Debug(ctx, DefaultAdapters(), exec.LookPath, file, root, map[string][]int{file: {5}})
	if err != nil && strings.Contains(err.Error(), "too old for this version of Delve") {
		t.Skip("this Go is older than the delve that is installed:", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(context.Background())
	if got := verified[file]; len(got) != 1 || !got[0].Verified {
		t.Fatalf("breakpoint: %+v", got)
	}
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctx)
	if err != nil || len(frames) == 0 || frames[0].Line != 5 || !strings.HasSuffix(frames[0].Path, "main.go") {
		t.Fatalf("stack: %+v %v", frames, err)
	}
	scopes, err := session.Scopes(ctx, frames[0].ID)
	if err != nil || len(scopes) == 0 {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
	found := false
	vars, _ := session.Variables(ctx, scopes[0].Reference)
	for _, v := range vars {
		found = found || (v.Name == "x" && v.Value == "21")
	}
	if !found {
		t.Fatalf("x = 21 not in %+v", vars)
	}
	if v, err := session.Evaluate(ctx, frames[0].ID, "x + 1"); err != nil || v.Value != "22" {
		t.Fatalf("evaluate: %+v %v", v, err)
	}
	if err := session.Next(ctx); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, session, "stopped")
}

func TestSession_StopDoesNotHangOnAnAdapterThatNeverAnswers(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	go func() { _, _ = io.Copy(io.Discard, server) }() // reads the disconnect request, never answers it
	s := NewSession(NewClient(client, client, client.Close))
	finished := make(chan error, 1)
	go func() { finished <- s.Stop(context.Background()) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(stopTimeout + 5*time.Second):
		t.Fatal("Stop must give up on the adapter and close it")
	}
}

func fakeTreeAdapter(t *testing.T) (Adapter, string) {
	t.Helper()
	t.Setenv("GO_FAKE_DAP", "1")
	log := t.TempDir() + "/log"
	t.Setenv("GO_FAKE_LOG", log)
	return Adapter{
		Name: "tree", Command: []string{os.Args[0], "-test.run=^TestHelperTreeAdapter$"}, Listens: true, Extensions: []string{".tree"},
		Launch: func(file, root string) map[string]any { return map[string]any{"request": "launch", "program": file} },
	}, log
}

func TestSession_AChildSessionOfTheAdapterRunsTheProgramAndTakesTheCommands(t *testing.T) {
	adapter, log := fakeTreeAdapter(t)
	root := t.TempDir()
	file := root + "/main.tree"
	session, _, err := Debug(ctxT(t), []Adapter{adapter}, lookPathOK, file, root, map[string][]int{file: {12}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(context.Background())
	// the stop happens in the child: its event reaches the events of the session, and the commands go to the child (the parent refuses them)
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctxT(t))
	if err != nil || len(frames) != 1 || frames[0].Name != "main.main" {
		t.Fatalf("the stack comes from the child: %+v %v", frames, err)
	}
	if v, err := session.Evaluate(ctxT(t), frames[0].ID, "x"); err != nil || v.Value != "84" {
		t.Fatalf("evaluate goes to the child: %+v %v", v, err)
	}
	if err := session.Next(ctxT(t)); err != nil {
		t.Fatalf("a step goes to the child: %v", err)
	}
	nextEvent(t, session, "stopped")
	if thread, stopped := session.Stopped(); !stopped || thread != 7 {
		t.Fatalf("the state is the one of the child: %d %v", thread, stopped)
	}
	// breakpoints that change later reach the parent and the child
	if _, err := session.SetBreakpoints(ctxT(t), file, []int{3, 4}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if got := strings.Count(string(data), "setBreakpoints"); got < 3 { // the start sent them to the parent and the child, now both again
		t.Fatalf("the breakpoints must reach both sessions (%d requests):\n%s", got, data)
	}
	if got := strings.Count(string(data), `"breakpoints":[{"line":3},{"line":4}]`); got != 2 {
		t.Fatalf("both sessions get the new lines: %d\n%s", got, data)
	}
}

func TestSession_AnAdapterThatCannotBeReachedAgainRefusesChildSessions(t *testing.T) {
	// a stdio adapter has no address: the request for a child session is refused, as before
	c, _ := fakeAdapter(t)
	s := NewSession(c)
	args, _ := json.Marshal(map[string]any{"request": "launch", "configuration": map[string]any{"type": "x"}})
	if _, err := s.answerRequest("startDebugging", args); err == nil || !strings.Contains(err.Error(), "no address") {
		t.Fatalf("no address: %v", err)
	}
	if _, err := s.answerRequest("runInTerminal", nil); err == nil {
		t.Fatal("runInTerminal is not supported")
	}
	if _, err := s.answerRequest("startDebugging", []byte(`{"request":"launch"}`)); err == nil {
		t.Fatal("a request without a configuration is refused")
	}
	// the adapter names its own address (debugpy): the session would connect there
	connect, _ := json.Marshal(map[string]any{"request": "attach", "configuration": map[string]any{"connect": map[string]any{"host": "127.0.0.1", "port": 1}}})
	if body, err := s.answerRequest("startDebugging", connect); err != nil || body != nil {
		t.Fatalf("a connect address is enough: %v", err)
	}
}

func TestSession_EvaluateKnowsFrameZeroAndNoFrame(t *testing.T) {
	c, log := fakeAdapter(t)
	s := NewSession(c)
	if _, err := s.Start(ctxT(t), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, s, "stopped")
	if _, err := s.Evaluate(ctxT(t), 0, "a"); err != nil { // js-debug numbers its frames from 0
		t.Fatal(err)
	}
	if _, err := s.Evaluate(ctxT(t), -1, "b"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), `"expression":"a"`) || !strings.Contains(string(data), `"frameId":0`) {
		t.Fatalf("frame 0 is a frame:\n%s", data)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"expression":"b"`) && strings.Contains(line, "frameId") {
			t.Fatalf("a negative id sends no frame: %s", line)
		}
	}
}

func TestSession_ActiveFindsTheNewestStopAmongChildrenAndTheirChildren(t *testing.T) {
	grandchild := &Session{stopped: true, stopOrder: 30}
	older := &Session{stopped: true, stopOrder: 10}
	child := &Session{children: []*Session{grandchild}}
	root := &Session{children: []*Session{older, child}}
	if got := root.active(); got != grandchild {
		t.Fatalf("the newest stop is in the grandchild: %p", got)
	}
	grandchild.stopped = false
	if got := root.active(); got != older {
		t.Fatal("the grandchild runs again: the older stop is the one that waits")
	}
	older.stopped = false
	if got := root.active(); got != root {
		t.Fatal("nothing stopped: the session itself")
	}
	// the frames of the last stack belong to the session they came from, also when another one stops after that
	older.stopped = true
	root.inspected = older
	newer := &Session{stopped: true, stopOrder: 99}
	root.children = append(root.children, newer)
	if got := root.inspectTarget(); got != older {
		t.Fatal("variables are asked from the session the stack came from")
	}
	if got := root.active(); got != newer {
		t.Fatal("the commands go to the newest stop")
	}
}
