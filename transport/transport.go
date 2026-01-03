package transport

import (
	"bytes"
	"errors"
	"io"
)

var (
	// ErrInvalidIO is returned when a write or read operation is called on
	// message io.Reader or a message io.Writer when they are no longer valid.
	// (i.e a new reader or writer has been obtained)
	ErrInvalidIO = errors.New("netconf: read/write on invalid io")
)

// Transport is used for a netconf.Session to talk to the device. It is message
// oriented to allow for framing and other details to happen on a per message
// basis.
type Transport interface {
	// MsgReader returns a reader for the next message.
	// The caller must close the reader when done.
	MsgReader() (io.ReadCloser, error)

	// MsgWriter returns a writer for a new message. Closing it will finalize
	// the message framing and flush to the underlying transport.
	MsgWriter() (io.WriteCloser, error)

	Close() error
}

// TestTransport mocks the underlying NETCONF transport layer.
// It allows us to queue up "Server Responses" and inspect "Client Requests".
type TestTransport struct {
	// inputs is a queue of messages the Server "sends" to the Client.
	// The Session calls ReadMsg() to pop from this queue.
	inputs [][]byte

	// outputs captures messages the Client "sends" to the Server.
	// The Session calls WriteMsg() to append to this list.
	outputs [][]byte
}

type readNoopCloser struct{ io.Reader }

func (r readNoopCloser) Close() error { return nil }

type testWriter struct {
	tt  *TestTransport
	buf *bytes.Buffer
}

func (w *testWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *testWriter) Close() error {
	w.tt.outputs = append(w.tt.outputs, w.buf.Bytes())
	return nil
}

func (t *TestTransport) MsgReader() (io.ReadCloser, error) {
	if len(t.inputs) == 0 {
		return nil, io.EOF
	}

	msg := t.inputs[0]
	t.inputs = t.inputs[1:]
	return readNoopCloser{bytes.NewReader(msg)}, nil
}

func (t *TestTransport) MsgWriter() (io.WriteCloser, error) {
	return &testWriter{tt: t, buf: &bytes.Buffer{}}, nil
}

func (t *TestTransport) Close() error { return nil }

// Helper to push a server response into the read queue
func (t *TestTransport) AddResponse(body string) {
	t.inputs = append(t.inputs, []byte(body))
}
