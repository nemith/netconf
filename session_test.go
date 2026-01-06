package netconf

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nemith.io/netconf/transport"
)

type testServer struct {
	t   *testing.T
	in  chan []byte
	out chan []byte
}

func newTestServer(t *testing.T) *testServer {
	return &testServer{
		t:   t,
		in:  make(chan []byte),
		out: make(chan []byte),
	}
}

func (s *testServer) handle(r io.ReadCloser, w io.WriteCloser) {
	in, err := io.ReadAll(r)
	if err != nil {
		panic(fmt.Sprintf("testerver: failed to read incomming message: %v", err))
	}

	s.t.Logf("testserver recv: %s", in)
	go func() { s.in <- in }()

	out, ok := <-s.out
	if !ok {
		panic("testserver: no message to send")
	}
	s.t.Logf("tesserver send: %s", out)

	_, err = w.Write(out)
	if err != nil {
		panic(fmt.Sprintf("testserver: failed to write message: %v", err))
	}

	if err := w.Close(); err != nil {
		panic("tesserver: failed to close outbound message")
	}
}

func (s *testServer) queueResp(p []byte)         { go func() { s.out <- p }() }
func (s *testServer) queueRespString(str string) { s.queueResp([]byte(str)) }
func (s *testServer) popReq() ([]byte, error) {
	msg, ok := <-s.in
	if !ok {
		return nil, fmt.Errorf("testserver: no message to read:")
	}
	return msg, nil
}

func (s *testServer) popReqString() (string, error) {
	p, err := s.popReq()
	return string(p), err
}

func (s *testServer) transport() *testTransport { return newTestTransport(s.handle) }

type testTransport struct {
	handler func(r io.ReadCloser, w io.WriteCloser)
	out     chan io.ReadCloser
}

func newTestTransport(handler func(r io.ReadCloser, w io.WriteCloser)) *testTransport {
	return &testTransport{
		handler: handler,
		out:     make(chan io.ReadCloser),
	}
}

func (s *testTransport) MsgReader() (io.ReadCloser, error) {
	return <-s.out, nil
}

func (s *testTransport) MsgWriter() (io.WriteCloser, error) {
	inr, inw := io.Pipe()
	outr, outw := io.Pipe()

	go func() { s.out <- outr }()
	go s.handler(inr, outw)

	return inw, nil
}

func (s *testTransport) Close() error {
	if len(s.out) > 0 {
		return fmt.Errorf("testtransport: remaining outboard messages not sent at close")
	}
	return nil
}

const (
	helloGood = `
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
	<capability>urn:ietf:params:netconf:base:1.0</capability>
	<capability>urn:ietf:params:netconf:base:1.1</capability>
  </capabilities>
  <session-id>42</session-id>
</hello>`

	helloBadXML = `
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities//>
</hello>`

	helloNoSessID = `
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
	<capability>urn:ietf:params:netconf:base:1.0</capability>
	<capability>urn:ietf:params:netconf:base:1.1</capability>
  </capabilities>
</hello>`

	helloNoCaps = `
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities></capabilities>
  <session-id>42</session-id>
</hello>`
)

func TestHello(t *testing.T) {
	tt := []struct {
		name        string
		serverHello string
		shouldError bool
		wantID      uint64
	}{
		{"good", helloGood, false, 42},
		{"bad xml", helloBadXML, true, 0},
		{"no capabilities", helloNoCaps, true, 0},
		{"no session-id", helloNoSessID, true, 0},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			sess := &Session{tr: ts.transport()}

			ts.queueRespString(tc.serverHello)

			err := sess.handshake()
			if !tc.shouldError {
				assert.NoError(t, err)
			}

			_, err = ts.popReqString()
			assert.NoError(t, err)
			assert.Equal(t, tc.wantID, sess.sessionID)
		})
	}
}

func TestNotificationHandler(t *testing.T) {
	received := make(chan string, 10)

	handler := func(ctx context.Context, msg *Message) error {
		defer func() { _ = msg.Close() }()

		var notif Notification
		if err := msg.Decode(&notif); err != nil {
			return err
		}

		received <- notif.EventTime.Format(time.RFC3339)
		return nil
	}

	tt := &transport.TestTransport{}

	// Queue hello response
	tt.AddResponse(`<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
  <session-id>42</session-id>
</hello>`)

	// Open session
	session, err := Open(tt, WithNotificationHandler(handler))
	require.NoError(t, err)
	require.NotNil(t, session.notifHandler)

	// Queue a notification
	tt.AddResponse(`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2024-01-05T12:34:56Z</eventTime>
</notification>`)

	// Wait for notification to be received
	select {
	case eventTime := <-received:
		assert.Equal(t, "2024-01-05T12:34:56Z", eventTime)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for notification")
	}

	// Clean up
	tt.AddResponse(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><ok/></rpc-reply>`)
	_ = session.Close(context.Background())
}

func TestNotificationHandlerContextCanceled(t *testing.T) {
	handler := func(ctx context.Context, msg *Message) error {
		defer func() { _ = msg.Close() }()
		return nil
	}

	tt := &transport.TestTransport{}
	tt.AddResponse(helloGood)

	session, err := Open(tt, WithNotificationHandler(handler))
	require.NoError(t, err)

	// Verify context is not cancelled initially
	select {
	case <-session.notifCtx.Done():
		t.Fatal("notification context should not be cancelled initially")
	default:
		// Expected
	}

	// Close session and verify context is cancelled
	tt.AddResponse(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><ok/></rpc-reply>`)
	_ = session.Close(context.Background())

	select {
	case <-session.notifCtx.Done():
		// Expected - context should be cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("notification context not cancelled after session close")
	}
}

func TestNotificationDecoding(t *testing.T) {
	// Test that Notification struct properly decodes
	xmlData := `<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2024-01-05T12:34:56Z</eventTime>
  <event xmlns="urn:example:event">
    <severity>critical</severity>
  </event>
</notification>`

	var notif Notification
	err := xml.Unmarshal([]byte(xmlData), &notif)
	require.NoError(t, err)
	assert.Equal(t, "2024-01-05T12:34:56Z", notif.EventTime.Format(time.RFC3339))
}

func TestNotificationWithoutHandler(t *testing.T) {
	tt := &transport.TestTransport{}
	tt.AddResponse(helloGood)

	// Create session WITHOUT notification handler
	session, err := Open(tt)
	require.NoError(t, err)
	require.Nil(t, session.notifHandler)

	// Verify we can still use the session normally
	assert.NotNil(t, session.notifCtx)
	assert.NotNil(t, session.notifCancel)
}
