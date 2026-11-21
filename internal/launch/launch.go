// Package launch opens a project in a new terminal window: it starts a terminal emulator that runs jd on the
// project. It knows tmux, WSL (through Windows Terminal), macOS Terminal, Windows Terminal and the common Linux
// terminals; $JD_TERMINAL overrides the choice.
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EnvTerminal names the terminal command used for new windows, for example "kitty" or "wezterm start --"; jd and the
// project directory are appended to it.
const EnvTerminal = "JD_TERMINAL"

// ErrNoTerminal is returned when no terminal that can be started was found.
var ErrNoTerminal = errors.New("no terminal found to open a new window (set " + EnvTerminal + ")")

// Env is what Command needs to know about the machine; tests replace it.
type Env struct {
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	WSL      bool   // running in the Windows Subsystem for Linux
	Distro   string // the WSL distribution name
}

// CurrentEnv describes the machine jd is running on.
func CurrentEnv() Env {
	env := Env{GOOS: runtime.GOOS, Getenv: os.Getenv, LookPath: exec.LookPath, Distro: os.Getenv("WSL_DISTRO_NAME")}
	if runtime.GOOS == "linux" {
		if env.Distro != "" {
			env.WSL = true
		} else if data, err := os.ReadFile("/proc/version"); err == nil {
			env.WSL = strings.Contains(strings.ToLower(string(data)), "microsoft")
		}
	}
	return env
}

// linuxTerminals are terminals that are tried one after the other, with the arguments that make them run a command.
var linuxTerminals = []struct {
	name string
	args []string
}{
	{"x-terminal-emulator", []string{"-e"}},
	{"gnome-terminal", []string{"--"}},
	{"konsole", []string{"-e"}},
	{"xfce4-terminal", []string{"-x"}},
	{"kitty", nil},
	{"wezterm", []string{"start", "--"}},
	{"alacritty", []string{"-e"}},
	{"foot", nil},
	{"xterm", []string{"-e"}},
}

// Command returns the command line that opens dir in a new window; exe is the jd binary to run in it.
func Command(env Env, exe, dir string) ([]string, error) {
	if custom := strings.Fields(env.Getenv(EnvTerminal)); len(custom) > 0 {
		return append(custom, exe, dir), nil
	}
	if env.Getenv("TMUX") != "" {
		if _, err := env.LookPath("tmux"); err == nil {
			return []string{"tmux", "new-window", "-c", dir, exe, dir}, nil
		}
	}
	switch {
	case env.WSL:
		return wslCommand(env, exe, dir)
	case env.GOOS == "darwin":
		script := fmt.Sprintf(`tell application "Terminal" to do script "%s"`, appleScriptEscape(shellQuote(exe)+" "+shellQuote(dir)))
		return []string{"osascript", "-e", script, "-e", `tell application "Terminal" to activate`}, nil
	case env.GOOS == "windows":
		if _, err := env.LookPath("wt.exe"); err == nil {
			return []string{"wt.exe", "-w", "new", exe, dir}, nil
		}
		return []string{"cmd", "/c", "start", "", exe, dir}, nil
	}
	for _, terminal := range linuxTerminals {
		if _, err := env.LookPath(terminal.name); err == nil {
			return append(append([]string{terminal.name}, terminal.args...), exe, dir), nil
		}
	}
	return nil, ErrNoTerminal
}

// wslCommand opens Windows Terminal (or a console window) that runs jd inside the WSL distribution.
func wslCommand(env Env, exe, dir string) ([]string, error) {
	inside := []string{"wsl.exe"}
	if env.Distro != "" {
		inside = append(inside, "-d", env.Distro)
	}
	inside = append(inside, "--cd", dir, "-e", exe, dir)
	if _, err := env.LookPath("wt.exe"); err == nil {
		return append([]string{"wt.exe", "-w", "new"}, inside...), nil
	}
	if _, err := env.LookPath("cmd.exe"); err == nil {
		return append([]string{"cmd.exe", "/c", "start", ""}, inside...), nil
	}
	return nil, ErrNoTerminal
}

func shellQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }

func appleScriptEscape(text string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text)
}

// NewWindow opens dir in a new terminal window with the running jd binary.
func NewWindow(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find jd: %w", err)
	}
	argv, err := Command(CurrentEnv(), exe, dir)
	if err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...) // #nosec G204 -- the terminal comes from PATH or the user's own setting
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	go func() { _ = cmd.Wait() }() // reap the terminal when it closes; jd does not wait for it
	return nil
}
