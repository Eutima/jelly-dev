package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeLauncher writes the command that starts a downloaded tool: a shell script, or a .cmd file on Windows.
func (e Env) writeLauncher(name, unixScript, windowsScript string) error {
	if err := os.MkdirAll(e.BinDir(), 0o755); err != nil {
		return err
	}
	if e.GOOS == "windows" {
		return os.WriteFile(filepath.Join(e.BinDir(), name+".cmd"), []byte(windowsScript), 0o644)
	}
	return os.WriteFile(filepath.Join(e.BinDir(), name), []byte(unixScript), 0o755) // #nosec G306 -- it must be executable
}

// installJdtls downloads the Eclipse JDT language server (Java); it starts through a Python script.
func (e Env) installJdtls(ctx context.Context, t Tool) error {
	if _, err := e.need(t, "a Java JDK", "java"); err != nil {
		return err
	}
	python, err := e.need(t, "Python 3 (it starts jdtls)", "python3", "python", "py")
	if err != nil {
		return err
	}
	target := filepath.Join(e.DataDir(), "jdtls")
	holder, err := os.CreateTemp("", "jd-jdtls-*.tar.gz") // a name of its own: another window may install at the same time
	if err != nil {
		return err
	}
	archive := holder.Name()
	holder.Close()
	defer os.Remove(archive)
	if err := e.Fetch(ctx, jdtlsURL, archive); err != nil {
		return fmt.Errorf("download jdtls: %w", err)
	}
	if err := verifySHA256(archive, jdtlsSHA256); err != nil {
		return fmt.Errorf("jdtls %s: %w", jdtlsVersion, err)
	}
	// an update: the old files go (the launcher looks for its jar by a pattern, a stale one next to the new one could be taken)
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := extractTarGz(archive, target); err != nil {
		return fmt.Errorf("unpack jdtls: %w", err)
	}
	script := filepath.Join(target, "bin", "jdtls")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("jdtls: %s was not in the download", script)
	}
	return e.writeLauncher("jdtls",
		fmt.Sprintf("#!/bin/sh\nexec %s %q \"$@\"\n", python, script),
		fmt.Sprintf("@%s \"%s\" %%*\r\n", python, script))
}

// verifySHA256 refuses a file whose SHA-256 differs from the expected hex digest.
func verifySHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch (got %s, want %s): the download is not the pinned release", got, want)
	}
	return nil
}
