package lsp

import (
	"io"

	"github.com/eutima/jelly-dev/internal/jsonframe"
)

// transport reads and writes the framed JSON messages of the protocol ("Content-Length: n" header, blank line, body).
type transport struct{ *jsonframe.Transport }

func newTransport(r io.Reader, w io.Writer) *transport { return &transport{jsonframe.New(r, w)} }

// write sends one message.
func (t *transport) write(message any) error { return t.Write(message) }

// read returns the body of the next message.
func (t *transport) read() ([]byte, error) { return t.Read() }
