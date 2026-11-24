package dap

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eutima/jelly-dev/internal/lsp"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPythonLaunch_AScriptRunsAsItIsWithTheInterpreterOfTheProject(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh") // any command: the interpreter the user named
	root := writeFiles(t, map[string]string{"tool.py": "print(1)\n"})
	file := filepath.Join(root, "tool.py")
	got := pythonLaunch(file, root)
	if got["request"] != "launch" || got["program"] != file || got["cwd"] != root || got["python"] != "sh" || got["console"] != "internalConsole" {
		t.Fatalf("script: %v", got)
	}
	if env, _ := got["env"].(map[string]string); env["PYTHONUNBUFFERED"] != "1" {
		t.Fatalf("output must not wait for the end of the program: %v", got["env"])
	}
	if _, there := got["args"]; there || got["django"] != nil {
		t.Fatalf("a script has no arguments and is no Django project: %v", got)
	}
}

func TestPythonLaunch_InADjangoProjectAViewRunsTheDevelopmentServerAndATestRunsItsTests(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh")
	root := writeFiles(t, map[string]string{
		"manage.py":                 "import sys\nif __name__ == '__main__':\n    pass\n",
		"shop/views.py":             "def index(request): ...\n",
		"shop/tests/test_orders.py": "def test_x(): ...\n",
		"shop/tests.py":             "x = 1\n",
		"shop/management/seed.py":   "if __name__ == \"__main__\":\n    print(1)\n",
		"other/run.py":              "print(1)\n",
	})
	manage := filepath.Join(root, "manage.py")
	serve := []string{"runserver", "--noreload"}

	for _, file := range []string{"shop/views.py", "manage.py"} {
		got := pythonLaunch(filepath.Join(root, filepath.FromSlash(file)), root)
		if got["program"] != manage || got["cwd"] != root || !reflect.DeepEqual(got["args"], serve) || got["django"] != true {
			t.Fatalf("%s runs the server: %v", file, got)
		}
	}
	for file, label := range map[string]string{"shop/tests/test_orders.py": "shop.tests.test_orders", "shop/tests.py": "shop.tests"} {
		got := pythonLaunch(filepath.Join(root, filepath.FromSlash(file)), root)
		if got["program"] != manage || !reflect.DeepEqual(got["args"], []string{"test", label}) {
			t.Fatalf("%s runs its tests: %v", file, got)
		}
	}
	script := filepath.Join(root, "shop", "management", "seed.py")
	if got := pythonLaunch(script, root); got["program"] != script || got["args"] != nil {
		t.Fatalf("a script with a main guard runs on its own, also in a Django project: %v", got)
	}
}

func TestPythonLaunch_ManagePyOfAnotherFolderOfTheProjectIsFoundAndOneOutsideIsNot(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh")
	root := writeFiles(t, map[string]string{"backend/manage.py": "", "backend/app/views.py": "x = 1\n", "docs/conf.py": "x = 1\n"})
	got := pythonLaunch(filepath.Join(root, "backend", "app", "views.py"), root)
	if got["program"] != filepath.Join(root, "backend", "manage.py") || got["cwd"] != filepath.Join(root, "backend") {
		t.Fatalf("manage.py in a subfolder: %v", got)
	}
	if conf := filepath.Join(root, "docs", "conf.py"); pythonLaunch(conf, root)["program"] != conf {
		t.Fatal("a file of a folder without manage.py runs as a script")
	}
	outside := writeFiles(t, map[string]string{"manage.py": "", "inner/app.py": "x = 1\n"})
	inner := filepath.Join(outside, "inner")
	if file := filepath.Join(inner, "app.py"); pythonLaunch(file, inner)["program"] != file {
		t.Fatal("a manage.py above the project folder does not count")
	}
}

func TestDebugpy_BelongsToPythonFiles(t *testing.T) {
	adapter, ok := AdapterFor(DefaultAdapters(), "/p/app/Views.PY")
	if !ok || adapter.Name != "debugpy" || adapter.Listens || adapter.Command[0] != "debugpy-adapter" {
		t.Fatalf("%+v %v", adapter, ok)
	}
}

// debugpyAdapter finds the adapter or skips the test.
func debugpyAdapter(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("debugpy-adapter"); err != nil {
		t.Skip("debugpy-adapter is not installed")
	}
}

// TestDebugpy_DebugsARealScript runs the real adapter against a small script: breakpoint, stack, variables, evaluate, step, end.
func TestDebugpy_DebugsARealScript(t *testing.T) {
	debugpyAdapter(t)
	root := writeFiles(t, map[string]string{"main.py": "x = 21\ny = x * 2\nprint('answer', y)\n"})
	file := filepath.Join(root, "main.py")
	ctx := ctxT(t)
	session, verified, err := Debug(ctx, DefaultAdapters(), exec.LookPath, file, root, map[string][]int{file: {2}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(context.Background())
	if got := verified[file]; len(got) != 1 || !got[0].Verified {
		t.Fatalf("breakpoint: %+v", got)
	}
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctx)
	if err != nil || len(frames) == 0 || frames[0].Line != 2 || !strings.HasSuffix(frames[0].Path, "main.py") {
		t.Fatalf("stack: %+v %v", frames, err)
	}
	scopes, err := session.Scopes(ctx, frames[0].ID)
	if err != nil || len(scopes) == 0 {
		t.Fatalf("scopes: %+v %v", scopes, err)
	}
	var seen bool
	for _, scope := range scopes {
		vars, _ := session.Variables(ctx, scope.Reference)
		for _, v := range vars {
			seen = seen || (v.Name == "x" && v.Value == "21")
		}
	}
	if !seen {
		t.Fatal("x = 21 is not in the scopes")
	}
	if v, err := session.Evaluate(ctx, frames[0].ID, "x + 1"); err != nil || v.Value != "22" {
		t.Fatalf("evaluate: %+v %v", v, err)
	}
	if err := session.Next(ctx); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, session, "stopped")
}

// TestDebugpy_DjangoViewBreakpointIsStoppedAtByARequest runs a small Django project (manage.py runserver under debugpy), requests the page
// and expects the stop in the view. Needs the adapter and a Python with Django (JD_PYTHON, VIRTUAL_ENV or .venv); skipped otherwise.
func TestDebugpy_DjangoViewBreakpointIsStoppedAtByARequest(t *testing.T) {
	debugpyAdapter(t)
	root := writeFiles(t, map[string]string{
		"manage.py": "import os, sys\nif __name__ == '__main__':\n    os.environ.setdefault('DJANGO_SETTINGS_MODULE', 'site_.settings')\n" +
			"    from django.core.management import execute_from_command_line\n    execute_from_command_line(sys.argv)\n",
		"site_/__init__.py": "",
		"site_/settings.py": "SECRET_KEY = 'x'\nDEBUG = True\nALLOWED_HOSTS = ['*']\nROOT_URLCONF = 'site_.urls'\nINSTALLED_APPS = []\nUSE_TZ = True\n",
		"site_/urls.py":     "from django.urls import path\nfrom . import views\nurlpatterns = [path('', views.index)]\n",
		"site_/views.py":    "from django.http import HttpResponse\n\n\ndef index(request):\n    greeting = 'hello'\n    return HttpResponse(greeting)\n",
	})
	python := pythonInterpreter(root)
	if python == "" || exec.Command(python, "-c", "import django").Run() != nil {
		t.Skip("no Python with Django (set JD_PYTHON)")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no local TCP:", err)
	}
	address := listener.Addr().String()
	listener.Close()
	adapters := DefaultAdapters()
	for i := range adapters {
		if adapters[i].Name == "debugpy" {
			launch := adapters[i].Launch
			adapters[i].Launch = func(file, root string) map[string]any {
				config := launch(file, root)
				config["args"] = append(config["args"].([]string), address) // a port that is free
				return config
			}
		}
	}
	view := filepath.Join(root, "site_", "views.py")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, _, err := Debug(ctx, adapters, exec.LookPath, view, root, map[string][]int{view: {6}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop(context.Background())
	body := make(chan string, 1)
	go func() {
		for ctx.Err() == nil { // the server needs a moment to start
			response, err := http.Get("http://" + address + "/")
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			body <- string(data)
			return
		}
	}()
	nextEvent(t, session, "stopped")
	frames, err := session.Stack(ctx)
	if err != nil || len(frames) == 0 || frames[0].Line != 6 || frames[0].Name != "index" || !strings.HasSuffix(frames[0].Path, "views.py") {
		t.Fatalf("the stop is in the view: %+v %v", frames, err)
	}
	if v, err := session.Evaluate(ctx, frames[0].ID, "greeting"); err != nil || v.Value != "'hello'" {
		t.Fatalf("evaluate in the view: %+v %v", v, err)
	}
	if err := session.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-body:
		if got != "hello" {
			t.Fatalf("the page: %q", got)
		}
	case <-ctx.Done():
		t.Fatal("the request must finish after continue")
	}
}

func TestPythonLaunch_ATestsFolderAboveTheProjectDoesNotMakeEveryFileATestAndOddNamesFallBackToTheFolder(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh")
	base := writeFiles(t, map[string]string{
		"tests/shop/manage.py": "", "tests/shop/app/views.py": "x = 1\n",
		"tests/shop/app/tests/test-odd.py": "x = 1\n",
	})
	root := filepath.Join(base, "tests", "shop")
	got := pythonLaunch(filepath.Join(root, "app", "views.py"), root)
	if !reflect.DeepEqual(got["args"], []string{"runserver", "--noreload"}) {
		t.Fatalf("a project below a tests folder still runs the server: %v", got["args"])
	}
	odd := pythonLaunch(filepath.Join(root, "app", "tests", "test-odd.py"), root)
	if !reflect.DeepEqual(odd["args"], []string{"test", "app/tests"}) {
		t.Fatalf("a file name that is no module gives its folder: %v", odd["args"])
	}
}

func TestDjangoRoot_AFolderBesideTheProjectIsNotTheProject(t *testing.T) {
	base := writeFiles(t, map[string]string{"proj2/manage.py": "", "proj2/app.py": "x = 1\n", "proj/app.py": "x = 1\n"})
	if got := djangoRoot(filepath.Join(base, "proj2", "app.py"), filepath.Join(base, "proj")); got != "" {
		t.Fatalf("a file outside the project has no Django root: %q", got)
	}
	if got := djangoRoot(filepath.Join(base, "proj2", "app.py"), filepath.Join(base, "proj2")); got != filepath.Join(base, "proj2") {
		t.Fatalf("inside its own project: %q", got)
	}
}

// A scratch file lives outside the project: it runs in the folder of the project with the environment of the project.
func TestPythonLaunch_AFileOutsideTheProjectRunsWithTheEnvironmentAndFolderOfTheProject(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh") // stands for the environment of the project
	root := writeFiles(t, map[string]string{"manage.py": "", "app.py": ""})
	scratch := filepath.Join(t.TempDir(), "scratches", "try.py")
	got := pythonLaunch(scratch, root)
	if got["program"] != scratch || got["cwd"] != root || got["python"] != "sh" || got["django"] != nil {
		t.Fatalf("scratch file: %v", got)
	}
}

func TestPythonLaunch_AScratchFileFindsTheModulesOfTheProject(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh")
	t.Setenv("PYTHONPATH", "/extra")
	root := writeFiles(t, map[string]string{"config/settings.py": "X = 1\n"})
	scratch := filepath.Join(writeFiles(t, map[string]string{"try.py": "import config.settings\n"}), "try.py")

	env, _ := pythonLaunch(scratch, root)["env"].(map[string]string)
	if want := root + string(os.PathListSeparator) + "/extra"; env["PYTHONPATH"] != want || env["PYTHONUNBUFFERED"] != "1" {
		t.Fatalf("a file outside the project gets the project on the path: %v, want PYTHONPATH %q", env, want)
	}
	inside, _ := pythonLaunch(filepath.Join(root, "config", "settings.py"), root)["env"].(map[string]string)
	if _, there := inside["PYTHONPATH"]; there {
		t.Fatalf("a file of the project is left alone: %v", inside)
	}
}

func TestRunCommand_KeepsTheReloaderOfTheServer_WhenRunningADjangoView(t *testing.T) {
	t.Setenv(lsp.EnvPython, "sh")
	root := writeFiles(t, map[string]string{"manage.py": "x = 1\n", "shop/views.py": "x = 1\n", "tool.py": "if __name__ == '__main__':\n    pass\n"})
	got, err := RunCommand(filepath.Join(root, "shop", "views.py"), root)
	if err != nil || !reflect.DeepEqual(got.Argv, []string{"sh", filepath.Join(root, "manage.py"), "runserver"}) || got.Dir != root {
		t.Fatalf("run a view: %v %v", got, err)
	}
	debug := pythonLaunch(filepath.Join(root, "shop", "views.py"), root)
	if !reflect.DeepEqual(debug["args"], []string{"runserver", "--noreload"}) {
		t.Fatalf("the debugger still runs without the reloader: %v", debug["args"])
	}
	script, _ := RunCommand(filepath.Join(root, "tool.py"), root)
	if !reflect.DeepEqual(script.Argv, []string{"sh", filepath.Join(root, "tool.py")}) {
		t.Fatalf("run a script: %v", script)
	}
	if _, err := RunCommand(filepath.Join(root, "notes.txt"), root); err != ErrNoRunner {
		t.Fatalf("err %v", err)
	}
}
