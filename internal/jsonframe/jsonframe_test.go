package jsonframe

import (
	"bytes"
	"strings"
	"testing"
)

func TestTransport_RoundTripsAMessage(t *testing.T) {
	var wire bytes.Buffer
	if err := New(nil, &wire).Write(map[string]any{"seq": 1}); err != nil {
		t.Fatal(err)
	}
	body, err := New(&wire, nil).Read()
	if err != nil || string(body) != `{"seq":1}` {
		t.Fatalf("got %q, %v", body, err)
	}
}

func TestTransport_RefusesBrokenFrames(t *testing.T) {
	for name, input := range map[string]string{
		"no length":  "X: 1\r\n\r\n{}",
		"bad length": "Content-Length: x\r\n\r\n",
		"too large":  "Content-Length: 99999999999\r\n\r\n",
		"short body": "Content-Length: 10\r\n\r\n{}",
	} {
		if _, err := New(strings.NewReader(input), nil).Read(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
