package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyLimited_FailsInsteadOfTruncating(t *testing.T) {
	var out bytes.Buffer
	if err := copyLimited(&out, strings.NewReader("small")); err != nil || out.String() != "small" {
		t.Fatalf("a small input is copied: %v %q", err, out.String())
	}
	if err := copyLimited(&out, zeros(maxDownload+1)); err == nil {
		t.Fatal("an input over the limit must fail")
	}
	if err := copyLimited(&bytes.Buffer{}, zeros(maxDownload)); err != nil {
		t.Fatalf("exactly the limit is fine: %v", err)
	}
}

type zeroReader struct{ left int64 }

func zeros(n int64) *zeroReader { return &zeroReader{left: n} }

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.left == 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > z.left {
		n = z.left
	}
	z.left -= n
	return int(n), nil
}

func TestFetch_RefusesRedirectToHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()
	old := httpsClient.Transport
	httpsClient.Transport = secure.Client().Transport
	defer func() { httpsClient.Transport = old }()
	err := fetch(context.Background(), secure.URL, filepath.Join(t.TempDir(), "file"))
	if err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("a redirect to http must be refused: %v", err)
	}
}
