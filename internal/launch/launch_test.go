package launch

import (
	"errors"
	"reflect"
	"testing"
)

func fake(goos string, env map[string]string, found ...string) Env {
	return Env{
		GOOS:   goos,
		Getenv: func(key string) string { return env[key] },
		LookPath: func(name string) (string, error) {
			for _, f := range found {
				if f == name {
					return "/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

func TestCommand(t *testing.T) {
	exe, dir := "/usr/local/bin/jd", "/work/my project"
	wsl := fake("linux", map[string]string{"PATH": "/home/u/.local/bin:/usr/bin"}, "wt.exe", "cmd.exe")
	wsl.WSL, wsl.Distro = true, "Ubuntu"
	wslNoWT := fake("linux", map[string]string{}, "cmd.exe")
	wslNoWT.WSL = true
	tests := []struct {
		name string
		env  Env
		want []string
	}{
		{"custom terminal", fake("linux", map[string]string{"JD_TERMINAL": "kitty -1"}), []string{"kitty", "-1", exe, dir}},
		{"tmux", fake("linux", map[string]string{"TMUX": "/tmp/tmux"}, "tmux"), []string{"tmux", "new-window", "-c", dir, exe, dir}},
		{"wsl with windows terminal", wsl, []string{"wt.exe", "-w", "new", "wsl.exe", "-d", "Ubuntu", "--cd", dir, "-e", "env", "PATH=/home/u/.local/bin:/usr/bin", exe, dir}},
		{"wsl without windows terminal", wslNoWT, []string{"cmd.exe", "/c", "start", "", "wsl.exe", "--cd", dir, "-e", exe, dir}},
		{"windows terminal", fake("windows", nil, "wt.exe"), []string{"wt.exe", "-w", "new", exe, dir}},
		{"windows console", fake("windows", nil), []string{"cmd", "/c", "start", "", exe, dir}},
		{"gnome terminal", fake("linux", nil, "gnome-terminal", "xterm"), []string{"gnome-terminal", "--", exe, dir}},
		{"xterm last", fake("linux", nil, "xterm"), []string{"xterm", "-e", exe, dir}},
		{"kitty", fake("linux", nil, "kitty"), []string{"kitty", exe, dir}},
	}
	for _, tt := range tests {
		got, err := Command(tt.env, exe, dir)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
	}
}

func TestCommand_QuotesForMacTerminal(t *testing.T) {
	got, err := Command(fake("darwin", nil), "/Applications/jd", `/Users/o'neil/"x"`)
	if err != nil || got[0] != "osascript" {
		t.Fatalf("%q, %v", got, err)
	}
	want := `tell application "Terminal" to do script "'/Applications/jd' '/Users/o'\\''neil/\"x\"'"`
	if got[2] != want {
		t.Fatalf("script\n got %s\nwant %s", got[2], want)
	}
}

func TestCommand_FailsWithoutTerminal(t *testing.T) {
	if _, err := Command(fake("linux", nil), "/jd", "/d"); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("got %v", err)
	}
	noTmux := fake("linux", map[string]string{"TMUX": "x"})
	if _, err := Command(noTmux, "/jd", "/d"); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("tmux set but not installed must fall through: %v", err)
	}
}
