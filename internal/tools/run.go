package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	installTimeout    = 10 * time.Minute
	fetchTimeout      = 5 * time.Minute
	maxDownload       = 512 << 20
	maxUnpacked       = 2 << 30 // all files of one archive together
	maxArchiveEntries = 100000
	jdtlsVersion      = "1.63.0"
	jdtlsFile         = "jdt-language-server-1.63.0-202610191322.tar.gz"
	groovyURL         = "https://github.com/GroovyLanguageServer/groovy-language-server/releases/latest/download/groovy-language-server-all.jar"
)

// jdtlsURL and jdtlsSHA256 pin the jdtls download to one milestone; raise them together (the checksum is published next to
// the archive as <file>.sha256). They are variables so tests can pin the archive they build.
var (
	jdtlsURL    = "https://download.eclipse.org/jdtls/milestones/" + jdtlsVersion + "/" + jdtlsFile
	jdtlsSHA256 = "b1f2abe0b3cccb462cf64fb4a5587c7d09df60cf8be7748b152cbd89d018bdc4"
)

// runCommand runs an installer command; its error message ends with the first line of its output.
func runCommand(ctx context.Context, env []string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- the commands are fixed by this package
	cmd.Env = append(os.Environ(), env...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		if message := lastLines(output.String()); message != "" {
			return fmt.Errorf("%s %s: %w: %s", name, firstWord(args), err, message)
		}
		return fmt.Errorf("%s %s: %w", name, firstWord(args), err)
	}
	return nil
}

func firstWord(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// lastLines keeps the last non-empty line of a command's output, where tools print their error.
func lastLines(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// httpsClient follows redirects only to https addresses, so a download cannot be sent to an insecure one.
var httpsClient = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing the redirect to %s over an insecure connection", req.URL)
	}
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	return nil
}}

// copyLimited copies src to dst and fails when src is longer than maxDownload, instead of silently keeping a cut-off file.
func copyLimited(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, maxDownload+1))
	if err != nil {
		return err
	}
	if n > maxDownload {
		return fmt.Errorf("larger than the limit of %d MiB", maxDownload>>20)
	}
	return nil
}

// fetch downloads a URL into a file (HTTPS only).
func fetch(ctx context.Context, url, dest string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to download %s over an insecure connection", url)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpsClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := copyLimited(file, resp.Body); err != nil {
		file.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), dest)
}

// extractTarGz unpacks an archive into dir; entries that would leave dir are refused.
func extractTarGz(archive, dir string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	zr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	entries, unpacked := 0, int64(0)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if entries++; entries > maxArchiveEntries {
			return fmt.Errorf("archive has more than %d entries", maxArchiveEntries)
		}
		if unpacked += header.Size; unpacked > maxUnpacked {
			return fmt.Errorf("archive unpacks to more than %d MiB", maxUnpacked>>20)
		}
		target := filepath.Join(root, filepath.FromSlash(header.Name))
		if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q is outside the target folder", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			if err := copyLimited(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("unpack %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}
