// Package netconftest provides test utilities for NETCONF sessions.
package testutil

import (
	"bytes"
	"encoding/xml"
	"io"
	"slices"
	"strings"
	"sync"
)

// RequestHandler is called for each message written by the client.
// It receives the request and returns zero or more responses.
// Returning nil or empty slice means no response.
type RequestHandler func(req string) []string

// Transport mocks the underlying NETCONF transport layer.
// Messages flow synchronously: when the client writes a request,
// the handler is called and its responses are queued for reading.
// Use Push() to queue unsolicited messages like notifications.
type Transport struct {
	mu       sync.Mutex
	handler  RequestHandler
	msgs     []string // pending messages to read
	msgReady chan struct{}
	outputs  []string
	closed   bool
}

// NewTransport creates a new test Transport with a handler function.
// The handler is called synchronously for each client write.
func NewTransport(handler RequestHandler) *Transport {
	return &Transport{
		handler:  handler,
		msgReady: make(chan struct{}, 1),
	}
}

func (t *Transport) MsgReader() (io.ReadCloser, error) {
	for {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil, io.EOF
		}
		if len(t.msgs) > 0 {
			msg := t.msgs[0]
			t.msgs = t.msgs[1:]
			t.mu.Unlock()
			return &testReader{tt: t, r: strings.NewReader(msg)}, nil
		}
		t.mu.Unlock()

		// Wait for messages or close
		<-t.msgReady
	}
}

func (t *Transport) MsgWriter() (io.WriteCloser, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, io.EOF
	}
	return &testWriter{tt: t, buf: &bytes.Buffer{}}, nil
}

func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	// Signal any waiting readers
	select {
	case t.msgReady <- struct{}{}:
	default:
	}
	return nil
}

// addResponses adds messages to the read queue (called internally after write)
func (t *Transport) addResponses(msgs []string) {
	t.mu.Lock()
	t.msgs = append(t.msgs, msgs...)
	t.mu.Unlock()

	// Signal that messages are ready
	select {
	case t.msgReady <- struct{}{}:
	default:
	}
}

// Push queues unsolicited messages (like notifications) for reading.
// These messages are delivered independently of any request/response flow.
func (t *Transport) Push(msgs ...string) {
	t.addResponses(msgs)
}

// Outputs returns the messages the client sent to the server.
func (t *Transport) Outputs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.outputs)
}

// Closed returns whether the transport has been closed.
func (t *Transport) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

type testReader struct {
	tt *Transport
	r  *strings.Reader
}

func (tr *testReader) Read(p []byte) (int, error) {
	tr.tt.mu.Lock()
	closed := tr.tt.closed
	tr.tt.mu.Unlock()

	if closed {
		return 0, io.EOF
	}
	return tr.r.Read(p)
}

func (tr *testReader) Close() error {
	return nil
}

type testWriter struct {
	tt     *Transport
	buf    *bytes.Buffer
	closed bool
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.tt.mu.Lock()
	defer w.tt.mu.Unlock()

	if w.tt.closed || w.closed {
		return 0, io.EOF
	}
	return w.buf.Write(p)
}

func (w *testWriter) Close() error {
	w.tt.mu.Lock()
	if w.closed {
		w.tt.mu.Unlock()
		return nil
	}
	w.closed = true
	msg := w.buf.String()
	w.tt.outputs = append(w.tt.outputs, msg)
	handler := w.tt.handler
	closed := w.tt.closed
	w.tt.mu.Unlock()

	// Call handler synchronously and queue responses
	if handler != nil && !closed {
		if responses := handler(msg); len(responses) > 0 {
			w.tt.addResponses(responses)
		}
	}
	return nil
}

// ExtractMessageID extracts the message-id attribute from an XML message.
func ExtractMessageID(msg string) string {
	decoder := xml.NewDecoder(strings.NewReader(msg))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := tok.(xml.StartElement); ok {
			for _, attr := range start.Attr {
				if attr.Name.Local == "message-id" {
					return attr.Value
				}
			}
			return ""
		}
	}
}

// Reply creates an rpc-reply with the given message-id and body.
func Reply(msgID, body string) string {
	return `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="` + msgID + `">` + body + `</rpc-reply>`
}

// OKReply creates an rpc-reply with <ok/> for the given message-id.
func OKReply(msgID string) string {
	return Reply(msgID, "<ok/>")
}
