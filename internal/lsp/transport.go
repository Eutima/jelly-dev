package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// maxMessage is the largest message that is accepted from a server.
const maxMessage = 64 << 20

// transport reads and writes the framed JSON messages of the protocol ("Content-Length: n" header, blank line, body).
type transport struct {
	reader  *bufio.Reader
	writer  io.Writer
	writeMu sync.Mutex
}

func newTransport(r io.Reader, w io.Writer) *transport {
	return &transport{reader: bufio.NewReader(r), writer: w}
}

// write sends one message.
func (t *transport) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	var frame bytes.Buffer
	fmt.Fprintf(&frame, "Content-Length: %d\r\n\r\n", len(body))
	frame.Write(body)
	_, err = t.writer.Write(frame.Bytes())
	return err
}

// read returns the body of the next message.
func (t *transport) read() ([]byte, error) {
	length := -1
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(name, "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("message without Content-Length")
	}
	if length > maxMessage {
		return nil, fmt.Errorf("message of %d bytes is too large", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(t.reader, body); err != nil {
		return nil, err
	}
	return body, nil
}
