//go:build !windows

package dap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sessionOf asks ps for the session id of a process.
func sessionOf(t *testing.T, pid int) int {
	t.Helper()
	out, err := exec.Command("ps", "-o", "sid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Skip("ps is not available:", err)
	}
	sid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("ps said %q", out)
	}
	return sid
}

// TestDetach_TheAdapterLeadsASessionOfItsOwn guards the terminal: a process in the session of jd could take the terminal away from it (debugpy
// makes the program the foreground group), which stops jd with SIGTTIN when the program ends.
func TestDetach_TheAdapterLeadsASessionOfItsOwn(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { killAdapter(cmd); _ = cmd.Wait() }()
	sid := sessionOf(t, cmd.Process.Pid)
	if sid != cmd.Process.Pid {
		t.Fatalf("the adapter must be the leader of a new session: sid %d, pid %d", sid, cmd.Process.Pid)
	}
	if mine := sessionOf(t, os.Getpid()); sid == mine {
		t.Fatal("it shares the session of jd")
	}
}

func TestStart_TheAdapterProcessHasNoControllingTerminal(t *testing.T) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err != nil {
		t.Skip("this test run has no controlling terminal itself: the check would pass without the fix")
	} else {
		tty.Close()
	}
	// the adapter tries to open /dev/tty, as debugpy's launcher does, and writes what happened to a file
	result := filepath.Join(t.TempDir(), "result")
	script := `if (exec 3<>/dev/tty) 2>/dev/null; then echo tty > "$1"; else echo notty > "$1"; fi; cat`
	client, err := Start([]string{"sh", "-c", script, "sh", result}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var got []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got, _ = os.ReadFile(result); len(got) > 0 {
			break
		}
	}
	if strings.TrimSpace(string(got)) != "notty" {
		t.Fatalf("the adapter must not be able to open the terminal of jd: %q", got)
	}
}
