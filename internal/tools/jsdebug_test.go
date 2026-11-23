package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsDebugArchive builds a tar.gz and pins the installer's checksum to it (restored when the test ends).
func jsDebugArchive(t *testing.T, files map[string]string) func(string) error {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	previous := jsDebugSHA256
	jsDebugSHA256 = hex.EncodeToString(sum[:])
	t.Cleanup(func() { jsDebugSHA256 = previous })
	return func(dest string) error { return os.WriteFile(dest, buf.Bytes(), 0o644) }
}

func TestInstall_JsDebugNeedsNodeUnpacksTheArchiveAndWritesALauncher(t *testing.T) {
	_, env := newMachine(t)
	err := env.Install(context.Background(), tool("js-debug"))
	var missing *MissingRuntimeError
	if !errors.As(err, &missing) || !strings.Contains(missing.Needs, "Node.js") {
		t.Fatalf("without Node.js the error must name it: %v", err)
	}
	m, env := newMachine(t, "node")
	m.archive = jsDebugArchive(t, map[string]string{"js-debug/src/dapDebugServer.js": "// server", "js-debug/package.nls.json": "{}"})
	if err := env.Install(context.Background(), tool("js-debug")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(env.DataDir(), "js-debug", "js-debug", "src", "dapDebugServer.js")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the archive must be unpacked: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(env.BinDir(), "js-debug-adapter"))
	if !strings.Contains(string(data), "'node' '"+script+"' \"$@\"") {
		t.Fatalf("launcher %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(env.DataDir(), "js-debug.new-*")); len(left) != 0 {
		t.Fatalf("no staging folder is left behind: %v", left)
	}
}

func TestInstall_JsDebugRefusesBadDownloadsAndKeepsTheWorkingInstall(t *testing.T) {
	m, env := newMachine(t, "node")
	m.archive = jsDebugArchive(t, map[string]string{"js-debug/src/dapDebugServer.js": "// old"})
	if err := env.Install(context.Background(), tool("js-debug")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(env.DataDir(), "js-debug", "js-debug", "src", "dapDebugServer.js")

	m.archive = jsDebugArchive(t, map[string]string{"readme.txt": "no server"})
	if err := env.Update(context.Background(), tool("js-debug")); err == nil || !strings.Contains(err.Error(), "was not in the download") {
		t.Fatalf("an archive without the server must fail: %v", err)
	}
	m.archive = jsDebugArchive(t, map[string]string{"../../evil": "x"})
	if err := env.Update(context.Background(), tool("js-debug")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("an archive that escapes the folder must be refused: %v", err)
	}
	m.archive = jsDebugArchive(t, map[string]string{"js-debug/src/dapDebugServer.js": "// new"})
	jsDebugSHA256 = strings.Repeat("0", 64)
	if err := env.Update(context.Background(), tool("js-debug")); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("a download that differs from the pin must be refused: %v", err)
	}
	if data, err := os.ReadFile(script); err != nil || string(data) != "// old" {
		t.Fatalf("a failed update leaves the working install alone: %q %v", data, err)
	}
	if !strings.Contains(jsDebugURL, "/download/v"+jsDebugVersion+"/") || strings.Contains(jsDebugURL, "latest") {
		t.Fatalf("the download must be a fixed release: %s", jsDebugURL)
	}
	m.archive = jsDebugArchive(t, map[string]string{"js-debug/src/dapDebugServer.js": "// new"})
	if err := env.Update(context.Background(), tool("js-debug")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(script); string(data) != "// new" {
		t.Fatalf("a good update replaces the install: %q", data)
	}
}
