package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// installPses downloads PowerShell Editor Services (the PowerShell language server) and writes the launcher that starts it with pwsh.
func (e Env) installPses(ctx context.Context, t Tool) error {
	pwsh, err := e.need(t, "PowerShell 7 (pwsh)", "pwsh")
	if err != nil {
		return err
	}
	target := filepath.Join(e.DataDir(), "powershell-editor-services")
	holder, err := os.CreateTemp("", "jd-pses-*.zip") // a name of its own: another window may install at the same time
	if err != nil {
		return err
	}
	archive := holder.Name()
	holder.Close()
	defer os.Remove(archive)
	if err := e.Fetch(ctx, psesURL, archive); err != nil {
		return fmt.Errorf("download PowerShell Editor Services: %w", err)
	}
	if err := verifySHA256(archive, psesSHA256); err != nil {
		return fmt.Errorf("PowerShell Editor Services %s: %w", psesVersion, err)
	}
	if err := os.RemoveAll(target); err != nil { // an update: the old files go
		return err
	}
	if err := os.MkdirAll(filepath.Join(target, "logs"), 0o755); err != nil {
		return err
	}
	if err := extractZip(archive, target); err != nil {
		return fmt.Errorf("unpack PowerShell Editor Services: %w", err)
	}
	script := filepath.Join(target, "PowerShellEditorServices", "Start-EditorServices.ps1")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("PowerShell Editor Services: %s was not in the download", script)
	}
	command := "& " + psQuote(script) +
		" -BundledModulesPath " + psQuote(target) +
		" -LogPath " + psQuote(filepath.Join(target, "logs", "jd.log")) +
		" -SessionDetailsPath " + psQuote(filepath.Join(target, "logs", "session.json")) +
		" -HostName jd -HostProfileId jd -HostVersion 1.0.0 -LogLevel Warning -Stdio"
	return e.writeLauncher("powershell-editor-services",
		"#!/bin/sh\nexec "+shQuote(pwsh)+" -NoLogo -NoProfile -NonInteractive -Command "+shQuote(command)+"\n",
		"@"+cmdQuote(pwsh)+" -NoLogo -NoProfile -NonInteractive -Command "+cmdQuote(command)+"\r\n")
}

// psQuote is a PowerShell string literal: single quotes, a quote inside is doubled.
func psQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }

// shQuote is a shell word: single quotes, a quote inside is written '\”.
func shQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }

// cmdQuote is a word of a .cmd file: double quotes, a quote inside is doubled.
func cmdQuote(text string) string { return `"` + strings.ReplaceAll(text, `"`, `""`) + `"` }

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
