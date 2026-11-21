package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigDir_FallsBackToPrivateTempFolder_WhenThereIsNoHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permissions")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir, err := ConfigDir(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != tmp {
		t.Fatalf("the folder belongs in the temp dir: %q", dir)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the folder must be private: %v %v", info, err)
	}
}

func TestConfigDir_RefusesTempFolderOthersCanWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permissions")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if err := os.Mkdir(filepath.Join(tmp, fmt.Sprintf("jd-%d", os.Getuid())), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(tmp, fmt.Sprintf("jd-%d", os.Getuid())), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigDir(func(string) string { return "" }); err == nil {
		t.Fatal("a folder that others can write to must be refused")
	}
}
