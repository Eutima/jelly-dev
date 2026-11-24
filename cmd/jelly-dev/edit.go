package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/eutima/jelly-dev/internal/config"
	"github.com/eutima/jelly-dev/internal/dap"
	"github.com/eutima/jelly-dev/internal/launch"
	"github.com/eutima/jelly-dev/internal/lsp"
	"github.com/eutima/jelly-dev/internal/project"
	"github.com/eutima/jelly-dev/internal/ui"
)

// envNoAutoInstall turns off the installation of missing language servers and formatters when a window opens (for offline
// machines and for CI); the command :tools still installs them on request.
const envNoAutoInstall = "JD_NO_AUTO_INSTALL"

// target is what `jd <path>` opens: a project directory and optionally a file in it.
type target struct{ root, file string }

// resolveTarget turns a command line path into a project: a directory is a project itself, a file belongs to the
// project that holds it (the git root, else its directory), a path that does not exist yet is a new file in the
// project of its directory.
func resolveTarget(path string) (target, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return target{}, err
	}
	if _, err := os.Stat(abs); err == nil {
		root, err := project.Root(abs)
		if err != nil {
			return target{}, err
		}
		if isDirectory(abs) {
			return target{root: root}, nil
		}
		return target{root: root, file: abs}, nil
	}
	parent := filepath.Dir(abs)
	if !isDirectory(parent) {
		return target{}, fmt.Errorf("%s: no such file or directory", path)
	}
	root, err := project.Root(parent)
	if err != nil {
		return target{}, err
	}
	return target{root: root, file: abs}, nil
}

// updateProjects applies a change to the project list on disk. The list is read again first, so changes made by
// other windows in the meantime are kept.
func updateProjects(env environment, change func(*project.Store)) error {
	dir, err := project.ConfigDir(env.getenv)
	if err != nil {
		return err
	}
	store, err := project.Open(dir)
	if err != nil {
		return err
	}
	change(store)
	return store.Save()
}

// runIDE handles `jd`, `jd <path>` and `jd edit [<path>]`: the project window. Without a path the most recent
// project is opened (the welcome screen when there is none).
func runIDE(args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) > 1 {
		fmt.Fprintf(stderr, "%s\n", usage)
		return exitUsage
	}
	dir, err := project.ConfigDir(env.getenv)
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	store, err := project.Open(dir)
	if err != nil {
		printError(stderr, fmt.Errorf("%w (remove the file to start with an empty project list)", err))
		return exitFailure
	}
	var open target
	switch {
	case len(args) == 1:
		if open, err = resolveTarget(args[0]); err != nil {
			printError(stderr, err)
			return exitFailure
		}
	default:
		if latest, ok := store.Latest(); ok {
			open.root = latest.Path
		}
	}
	setupOnFirstStart(dir, stdout, stderr, env) // after the target is known: a mistyped folder fails before anything is installed
	if !useEditorStyle(stderr, env) {           // the setup may just have asked for the editor
		return exitUsage
	}
	newWindow := env.newWindow
	if newWindow == nil {
		newWindow = launch.NewWindow
	}
	if open.root == "" { // nothing to open: let the user pick
		welcome, outcome := ui.NewWelcomeModel(store.Recent(), newWindow, func(path string) {
			_ = updateProjects(env, func(s *project.Store) { s.Remove(path) })
		})
		if err := env.runUI(welcome); err != nil {
			printError(stderr, fmt.Errorf("run ui: %w", err))
			return exitFailure
		}
		if outcome.Path == "" {
			return exitOK
		}
		open.root = outcome.Path
	}
	if err := updateProjects(env, func(s *project.Store) { s.Touch(open.root) }); err != nil {
		// a read-only or missing config directory (containers) only means the project list is not remembered
		fmt.Fprintf(stderr, "jd: warning: the project list cannot be stored: %v\n", err)
	}
	servers, err := lsp.LoadServers(filepath.Join(dir, "lsp.json"))
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	navigator := newWindowNavigator(open.root, servers, filepath.Join(dir, "formatters.json"))
	defer navigator.Close()
	hooks := ui.ProjectHooks{
		Theme: func(name string) (ui.Theme, bool) { // :theme in the window: the attribute themes need their attributes kept as well
			theme, ok := env.namedTheme(name)
			if ok && (needsAttributes(name) || name == config.ThemeAuto) && env.enableAttributes != nil {
				env.enableAttributes()
			}
			return theme, ok
		},
		StartDebug: func(ctx context.Context, file string, breakpoints map[string][]int) (ui.DebugSession, error) {
			session, _, err := dap.Debug(ctx, dap.DefaultAdapters(), exec.LookPath, file, open.root, breakpoints)
			if err != nil {
				return nil, err
			}
			return session, nil
		},
		Navigator:   navigator,
		Tools:       newInstaller(dir, env),
		AutoInstall: env.getenv(envNoAutoInstall) == "", // language servers and formatters are installed when a window opens

		Recent: func() []project.Entry {
			current, err := project.Open(dir)
			if err != nil {
				return nil
			}
			return current.Recent()
		},
		Forget: func(path string) {
			_ = updateProjects(env, func(s *project.Store) { s.Remove(path) })
		},
		Open: func(root string) project.Session {
			if err := updateProjects(env, func(s *project.Store) { s.Touch(root) }); err != nil {
				fmt.Fprintf(stderr, "jd: cannot store the project list: %v\n", err)
			}
			navigator.switchTo(root) // the language servers belong to the project
			current, err := project.Open(dir)
			if err != nil {
				return project.Session{}
			}
			return current.Session(root)
		},
		NoteRoot: func() string {
			root, _ := project.NoteRoot(dir) // an unreadable file means: the notes of the open project (stderr would corrupt the screen)
			return root
		},
		SetNoteRoot: func(root string) {
			if err := project.SetNoteRoot(dir, root); err != nil {
				fmt.Fprintf(stderr, "jd: cannot store the project of the notes: %v\n", err)
			}
		},
		ScratchDir:     func() string { return project.ScratchDir(dir) },
		ReadClipboard:  env.readClipboard,
		WriteClipboard: env.writeClipboard,
		NewWindow:      newWindow,
		SetEditor: func(style config.EditorStyle) {
			if err := config.SaveEditorStyle(dir, style); err != nil {
				fmt.Fprintf(stderr, "jd: cannot remember the editor: %v\n", err)
			}
		},
		SaveSession: func(root string, session project.Session) {
			if err := updateProjects(env, func(s *project.Store) { s.SetSession(root, session) }); err != nil {
				fmt.Fprintf(stderr, "jd: cannot store the session: %v\n", err)
			}
		},
	}
	model, err := ui.NewProjectModel(open.root, open.file, store.Session(open.root), hooks)
	if err != nil {
		printError(stderr, err)
		return exitFailure
	}
	if err := env.runUI(model); err != nil {
		printError(stderr, fmt.Errorf("run ui: %w", err))
		return exitFailure
	}
	return exitOK
}

// windowNavigator is the code navigator of a project window. It follows the window when it switches to another project:
// the language servers of the old project are stopped and new ones start in the new project.
type windowNavigator struct {
	servers       []lsp.ServerSpec
	formatterFile string

	mu      sync.Mutex
	manager *lsp.Manager
}

func newWindowNavigator(root string, servers []lsp.ServerSpec, formatterFile string) *windowNavigator {
	w := &windowNavigator{servers: servers, formatterFile: formatterFile}
	w.manager = w.newManager(root)
	return w
}

// newManager starts a manager for a project; the formatters the user picked are kept in formatterFile (a broken file is
// ignored: the choices only make :format more convenient).
func (w *windowNavigator) newManager(root string) *lsp.Manager {
	manager := lsp.NewManager(root, w.servers)
	if w.formatterFile != "" {
		_ = manager.UseFormatterFile(w.formatterFile)
	}
	return manager
}

func (w *windowNavigator) current() *lsp.Manager {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.manager
}

// switchTo points the navigator at another project; the old servers are stopped in the background.
func (w *windowNavigator) switchTo(root string) {
	w.mu.Lock()
	old := w.manager
	w.manager = w.newManager(root)
	w.mu.Unlock()
	go old.Close()
}

// Close stops the language servers.
func (w *windowNavigator) Close() { w.current().Close() }

func (w *windowNavigator) Definition(ctx context.Context, path, text string, row, col int) ([]lsp.Place, error) {
	return w.current().Definition(ctx, path, text, row, col)
}

func (w *windowNavigator) References(ctx context.Context, path, text string, row, col int) ([]lsp.Place, error) {
	return w.current().References(ctx, path, text, row, col)
}

func (w *windowNavigator) Format(ctx context.Context, path, text string) (string, error) {
	return w.current().Format(ctx, path, text)
}

func (w *windowNavigator) FormatterChoices(path string) []lsp.FormatterChoice {
	return w.current().FormatterChoices(path)
}

func (w *windowNavigator) FormatWith(ctx context.Context, path, text string, choice lsp.FormatterChoice) (string, error) {
	return w.current().FormatWith(ctx, path, text, choice)
}

func (w *windowNavigator) RememberFormatter(path string, choice lsp.FormatterChoice) error {
	return w.current().RememberFormatter(path, choice)
}

func (w *windowNavigator) Complete(ctx context.Context, path, text string, row, col int) ([]lsp.CompletionItem, error) {
	return w.current().Complete(ctx, path, text, row, col)
}

func (w *windowNavigator) ResolveCompletion(ctx context.Context, path string, item lsp.CompletionItem) (lsp.CompletionItem, error) {
	return w.current().ResolveCompletion(ctx, path, item)
}

func (w *windowNavigator) Warm(ctx context.Context, path, text string) error {
	return w.current().Warm(ctx, path, text)
}

func (w *windowNavigator) Hover(ctx context.Context, path, text string, row, col int) (string, error) {
	return w.current().Hover(ctx, path, text, row, col)
}

func (w *windowNavigator) Rename(ctx context.Context, path, text string, row, col int, newName string) (lsp.WorkspaceEdit, error) {
	return w.current().Rename(ctx, path, text, row, col, newName)
}

func (w *windowNavigator) RenameFile(ctx context.Context, oldPath, newPath string) (lsp.WorkspaceEdit, error) {
	return w.current().RenameFile(ctx, oldPath, newPath)
}
