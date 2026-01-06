package netconf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nemith.io/netconf/testutil"
)

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

// echoHandler responds to hello and RPCs with appropriate replies
func echoHandler(req string) []string {
	// Check if it's a hello message (no message-id)
	if strings.Contains(req, "<hello") {
		return []string{helloGood}
	}
	// Otherwise it's an RPC - extract message-id and reply
	msgID := testutil.ExtractMessageID(req)
	if msgID != "" {
		return []string{testutil.OKReply(msgID)}
	}
	return nil
}

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
			tr := testutil.NewTransport(func(req string) []string {
				return []string{tc.serverHello}
			})
			sess := &Session{tr: tr}

			err := sess.handshake()
			if !tc.shouldError {
				assert.NoError(t, err)
			}

			assert.Len(t, tr.Outputs(), 1)
			assert.Equal(t, tc.wantID, sess.sessionID)
		})
	}
}

func TestWithCapability(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	customCaps := []string{"urn:custom:cap:1.0", "urn:custom:cap:2.0"}
	session, err := Open(tt, WithCapability(customCaps...))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Verify custom capabilities were set
	assert.True(t, session.ClientCaps().Has("urn:custom:cap:1.0"))
	assert.True(t, session.ClientCaps().Has("urn:custom:cap:2.0"))
	assert.False(t, session.ClientCaps().Has(CapNetConf10))
}

func TestWithLogger(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	// Create a custom logger that writes to a buffer
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	session, err := Open(tt, WithLogger(logger))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	assert.Equal(t, logger, session.logger)
}

func TestWithLoggerNil(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	// Nil logger should become DiscardHandler
	session, err := Open(tt, WithLogger(nil))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	assert.NotNil(t, session.logger)
}

func TestWithNotifHandlerInterface(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	// Test with interface-based handler
	handler := &testNotifHandler{}
	session, err := Open(tt, WithNotifHandler(handler))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	assert.Equal(t, handler, session.notifHandler)
}

type testNotifHandler struct {
	calls int
}

func (h *testNotifHandler) HandleNotification(ctx context.Context, msg *Message) {
	h.calls++
	_ = msg.Close()
}

func TestMultipleOptions(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	customCaps := []string{"urn:custom:cap:1.0"}
	handler := &testNotifHandler{}

	session, err := Open(tt,
		WithCapability(customCaps...),
		WithLogger(logger),
		WithNotifHandler(handler),
	)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	assert.True(t, session.ClientCaps().Has("urn:custom:cap:1.0"))
	assert.Equal(t, logger, session.logger)
	assert.Equal(t, handler, session.notifHandler)
}

func TestNewSession(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	assert.NotNil(t, session)
	assert.Equal(t, tt, session.tr)
	assert.NotNil(t, session.clientCaps)
	assert.NotNil(t, session.reqs)
	assert.NotNil(t, session.notifCtx)
	assert.NotNil(t, session.notifCancel)
	assert.NotNil(t, session.logger)
	assert.Equal(t, uint64(0), session.sessionID)
}

func TestOpenSuccess(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	require.NotNil(t, session)

	assert.Equal(t, uint64(42), session.sessionID)
	assert.NotNil(t, session.serverCaps)
	assert.True(t, session.ServerCaps().Has(CapNetConf10))
}

func TestOpenHandshakeFailure(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		return []string{helloBadXML}
	})

	session, err := Open(tt)
	assert.Error(t, err)
	assert.Nil(t, session)

	// Transport should be closed on error
	assert.True(t, tt.Closed())
}

func TestNextMessageID(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	// Should start at 1 and increment
	id1 := session.nextMessageID()
	id2 := session.nextMessageID()
	id3 := session.nextMessageID()

	assert.Equal(t, "1", id1)
	assert.Equal(t, "2", id2)
	assert.Equal(t, "3", id3)
}

func TestPrepareWithoutMessageID(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	rpc := &RPC{Operation: "test"}
	prepared := session.Prepare(rpc)

	assert.NotEmpty(t, prepared.MessageID)
	assert.Equal(t, "1", prepared.MessageID)
}

func TestPrepareWithMessageID(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	rpc := &RPC{Operation: "test", MessageID: "custom-id"}
	prepared := session.Prepare(rpc)

	assert.Equal(t, "custom-id", prepared.MessageID)
}

func TestPrepareDoesNotMutate(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	rpc := &RPC{Operation: "test"}
	prepared := session.Prepare(rpc)

	// Original should not have message ID
	assert.Empty(t, rpc.MessageID)
	// Prepared should have message ID
	assert.NotEmpty(t, prepared.MessageID)
}

func TestDoSuccess(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	rpc := NewRPC("test-op")
	ctx := context.Background()
	msg, err := session.Do(ctx, rpc)

	require.NoError(t, err)
	require.NotNil(t, msg)
	defer func() {
		_ = msg.Close()
	}()

	assert.Equal(t, "1", msg.MessageID())
}

func TestDoDuplicateMessageID(t *testing.T) {
	tt := testutil.NewTransport(nil)
	session := newSession(tt)

	rpc := &RPC{Operation: "test", MessageID: "duplicate-id"}

	// First request should register
	ctx := context.Background()

	// Create a pending request manually
	session.mu.Lock()
	session.reqs["duplicate-id"] = &pendingReq{
		msg: make(chan *Message, 1),
		ctx: ctx,
	}
	session.mu.Unlock()

	// Second request with same ID should fail
	_, err := session.Do(ctx, rpc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already pending")
}

func TestDoContextCanceled(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	rpc := NewRPC("test-op")
	msg, err := session.Do(ctx, rpc)

	assert.Error(t, err)
	assert.Nil(t, msg)
	assert.Equal(t, context.Canceled, err)
}

func TestDoSessionClosed(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)

	// Close the transport to simulate connection loss
	_ = tt.Close()

	// Give recvLoop time to detect closure
	time.Sleep(50 * time.Millisecond)

	rpc := NewRPC("test-op")
	ctx := context.Background()
	msg, err := session.Do(ctx, rpc)

	assert.Error(t, err)
	assert.Nil(t, msg)
}

func TestExecSuccess(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		if strings.Contains(req, "<hello") {
			return []string{helloGood}
		}
		msgID := testutil.ExtractMessageID(req)
		return []string{testutil.Reply(msgID, `<data><result>success</result></data>`)}
	})

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	type TestReply struct {
		XMLName xml.Name `xml:"rpc-reply"`
		Data    struct {
			Result string `xml:"result"`
		} `xml:"data"`
	}

	var reply TestReply
	err = session.Exec(context.Background(), "test-op", &reply)
	require.NoError(t, err)
	assert.Equal(t, "success", reply.Data.Result)
}

func TestExecWithRPCError(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		if strings.Contains(req, "<hello") {
			return []string{helloGood}
		}
		msgID := testutil.ExtractMessageID(req)
		return []string{testutil.Reply(msgID, `<rpc-error>
			<error-type>application</error-type>
			<error-tag>operation-failed</error-tag>
			<error-severity>error</error-severity>
			<error-message>Test error</error-message>
		</rpc-error>`)}
	})

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	err = session.Exec(context.Background(), "test-op", nil)
	assert.Error(t, err)

	var rpcErrors RPCErrors
	assert.True(t, errors.As(err, &rpcErrors))
	assert.Len(t, rpcErrors, 1)
	assert.Equal(t, "Test error", rpcErrors[0].Message)
}

func TestExecWithRPCWarning(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		if strings.Contains(req, "<hello") {
			return []string{helloGood}
		}
		msgID := testutil.ExtractMessageID(req)
		return []string{testutil.Reply(msgID, `<rpc-error>
			<error-type>application</error-type>
			<error-tag>operation-failed</error-tag>
			<error-severity>warning</error-severity>
			<error-message>Test warning</error-message>
		</rpc-error>
		<ok/>`)}
	})

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	err = session.Exec(context.Background(), "test-op", nil)
	assert.NoError(t, err)
}

func TestExecNilReply(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Should not panic with nil reply
	err = session.Exec(context.Background(), "test-op", nil)
	assert.NoError(t, err)
}

func TestExecContextCanceled(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = session.Exec(ctx, "test-op", nil)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestRecvMsgMissingMessageID(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	_, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Push reply without message-id (should be logged and ignored)
	tt.Push(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`)

	// Give recvLoop time to process
	time.Sleep(50 * time.Millisecond)
}

func TestRecvMsgUnexpectedReply(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	_, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Push reply with message-id that doesn't match any pending request
	tt.Push(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="999"><ok/></rpc-reply>`)

	// Give recvLoop time to process
	time.Sleep(50 * time.Millisecond)
}

func TestRecvMsgUnknownMessageType(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	_, err := Open(tt)
	require.NoError(t, err)

	// Push unknown message type
	tt.Push(`<unknown-message xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"/>`)

	// Give recvLoop time to process and exit
	time.Sleep(100 * time.Millisecond)

	// Transport should be closed
	assert.True(t, tt.Closed())
}

func TestCloseSuccess(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)

	err = session.Close(context.Background())
	assert.NoError(t, err)
	assert.True(t, tt.Closed())
}

func TestCloseWithPendingRequests(t *testing.T) {
	// Handler that only responds to hello, not to RPCs
	tt := testutil.NewTransport(func(req string) []string {
		if strings.Contains(req, "<hello") {
			return []string{helloGood}
		}
		return nil // No response for RPCs
	})

	session, err := Open(tt)
	require.NoError(t, err)

	// Start a request in background
	done := make(chan error, 1)
	go func() {
		rpc := NewRPC("test-op")
		_, err := session.Do(context.Background(), rpc)
		done <- err
	}()

	// Give request time to register
	time.Sleep(50 * time.Millisecond)

	// Close without responding
	_ = tt.Close()

	// Request should receive ErrClosed
	select {
	case err := <-done:
		assert.Error(t, err)
		assert.Equal(t, ErrClosed, err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for request to fail")
	}
}

func TestCloseNotifContextCanceled(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)

	notifCtx := session.notifCtx

	err = session.Close(context.Background())
	assert.NoError(t, err)

	// Notification context should be canceled
	select {
	case <-notifCtx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("notification context not canceled")
	}
}

func TestConcurrentDo(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)

	const numRequests = 10

	var wg sync.WaitGroup
	wg.Add(numRequests)
	results := make([]error, numRequests)

	// Send concurrent requests
	for i := range numRequests {
		go func() {
			defer wg.Done()
			rpc := NewRPC("test-op")
			msg, err := session.Do(context.Background(), rpc)
			if err == nil && msg != nil {
				_ = msg.Close()
			}
			results[i] = err
		}()
	}

	wg.Wait()

	// All requests should succeed
	for i, err := range results {
		assert.NoError(t, err, "request %d failed", i)
	}

	_ = tt.Close()
}

func TestConcurrentDoAndNotifications(t *testing.T) {
	received := make(chan string, 10)
	handler := func(ctx context.Context, msg *Message) {
		defer func() {
			_ = msg.Close()
		}()
		received <- "notification"
	}

	rpcCount := 0
	tt := testutil.NewTransport(func(req string) []string {
		if strings.Contains(req, "<hello") {
			return []string{helloGood}
		}
		msgID := testutil.ExtractMessageID(req)
		rpcCount++
		// Interleave notifications with RPC replies
		if rpcCount == 1 {
			return []string{
				`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><eventTime>2024-01-05T12:34:56Z</eventTime></notification>`,
				testutil.OKReply(msgID),
				`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><eventTime>2024-01-05T12:34:57Z</eventTime></notification>`,
			}
		}
		return []string{testutil.OKReply(msgID)}
	})

	session, err := Open(tt, WithNotifHandlerFunc(handler))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Send RPC
	rpc := NewRPC("test-op")
	msg, err := session.Do(context.Background(), rpc)
	require.NoError(t, err)
	_ = msg.Close()

	// Should have received both notifications
	select {
	case <-received:
		// Got first notification
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for first notification")
	}

	select {
	case <-received:
		// Got second notification
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for second notification")
	}
}

func TestSessionID(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	assert.Equal(t, uint64(42), session.SessionID())
}

func TestClientCaps(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	caps := session.ClientCaps()
	assert.NotNil(t, caps)
	assert.True(t, caps.Has(CapNetConf10))
}

func TestServerCaps(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	caps := session.ServerCaps()
	assert.NotNil(t, caps)
	assert.True(t, caps.Has(CapNetConf10))
	assert.True(t, caps.Has(CapNetConf11))
}

func TestStartElement(t *testing.T) {
	xmlData := `<?xml version="1.0"?>
	<!-- comment -->
	<root xmlns="test">content</root>`

	decoder := xml.NewDecoder(strings.NewReader(xmlData))
	elem, err := startElement(decoder)

	require.NoError(t, err)
	assert.Equal(t, "root", elem.Name.Local)
	assert.Equal(t, "test", elem.Name.Space)
}

func TestStartElementEOF(t *testing.T) {
	xmlData := ``

	decoder := xml.NewDecoder(strings.NewReader(xmlData))
	elem, err := startElement(decoder)

	assert.Error(t, err)
	assert.Nil(t, elem)
	assert.Equal(t, io.EOF, err)
}

func TestGetMessageID(t *testing.T) {
	attrs := []xml.Attr{
		{Name: xml.Name{Local: "xmlns"}, Value: "urn:test"},
		{Name: xml.Name{Local: "message-id"}, Value: "12345"},
		{Name: xml.Name{Local: "other"}, Value: "value"},
	}

	msgID := getMessageID(attrs)
	assert.Equal(t, "12345", msgID)
}

func TestGetMessageIDMissing(t *testing.T) {
	attrs := []xml.Attr{
		{Name: xml.Name{Local: "xmlns"}, Value: "urn:test"},
		{Name: xml.Name{Local: "other"}, Value: "value"},
	}

	msgID := getMessageID(attrs)
	assert.Equal(t, "", msgID)
}

func TestMsgReaderClose(t *testing.T) {
	buf := strings.NewReader("test content")
	closed := false
	closer := &testCloser{closeFn: func() error {
		closed = true
		return nil
	}}

	reader := &msgReader{
		Reader: buf,
		closer: closer,
		done:   make(chan struct{}),
	}

	err := reader.Close()
	assert.NoError(t, err)
	assert.True(t, closed)

	// Check that done channel is closed
	select {
	case <-reader.done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel not closed")
	}
}

func TestMsgReaderDoubleClose(t *testing.T) {
	buf := strings.NewReader("test content")
	closeCount := 0
	closer := &testCloser{closeFn: func() error {
		closeCount++
		return nil
	}}

	reader := &msgReader{
		Reader: buf,
		closer: closer,
		done:   make(chan struct{}),
	}

	_ = reader.Close()
	_ = reader.Close()

	// Should only close once due to sync.Once
	assert.Equal(t, 1, closeCount)
}

type testCloser struct {
	closeFn func() error
}

func (tc *testCloser) Close() error {
	if tc.closeFn != nil {
		return tc.closeFn()
	}
	return nil
}

func TestNotificationMessageNotClosed(t *testing.T) {
	handlerCalled := make(chan bool, 1)
	handler := func(ctx context.Context, msg *Message) {
		// Intentionally don't close the message
		handlerCalled <- true
	}

	tt := testutil.NewTransport(echoHandler)

	_, err := Open(tt, WithNotifHandlerFunc(handler))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Push a notification
	tt.Push(`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
		<eventTime>2024-01-05T12:34:56Z</eventTime>
	</notification>`)

	// Wait for handler to be called
	select {
	case <-handlerCalled:
		// Handler was called, message should be auto-closed
	case <-time.After(1 * time.Second):
		t.Fatal("handler not called")
	}

	// Give time for auto-close
	time.Sleep(50 * time.Millisecond)
}

func TestNotificationConcurrent(t *testing.T) {
	var count atomic.Int32
	handler := func(ctx context.Context, msg *Message) {
		defer func() {
			_ = msg.Close()
		}()
		count.Add(1)
	}

	tt := testutil.NewTransport(echoHandler)

	_, err := Open(tt, WithNotifHandlerFunc(handler))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Push 5 notifications
	for range 5 {
		tt.Push(`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
			<eventTime>2024-01-05T12:34:56Z</eventTime>
		</notification>`)
	}

	// Wait for all notifications to be processed
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, int32(5), count.Load())
}

// upgradableTransport is a test transport that supports Upgrade()
type upgradableTransport struct {
	*testutil.Transport
	upgraded bool
}

func (u *upgradableTransport) Upgrade() {
	u.upgraded = true
}

func TestHandshakeUpgrade(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		return []string{`<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
			<capabilities>
				<capability>urn:ietf:params:netconf:base:1.0</capability>
				<capability>urn:ietf:params:netconf:base:1.1</capability>
			</capabilities>
			<session-id>42</session-id>
		</hello>`}
	})
	ut := &upgradableTransport{Transport: tt}

	// Client also supports 1.1 by default
	session, err := Open(ut)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	assert.NotNil(t, session)

	// Transport should have been upgraded
	assert.True(t, ut.upgraded)
}

func TestHandshakeNoUpgradeWhenServerLacks11(t *testing.T) {
	tt := testutil.NewTransport(func(req string) []string {
		return []string{`<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
			<capabilities>
				<capability>urn:ietf:params:netconf:base:1.0</capability>
			</capabilities>
			<session-id>42</session-id>
		</hello>`}
	})
	ut := &upgradableTransport{Transport: tt}

	session, err := Open(ut)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	assert.NotNil(t, session)

	// Transport should NOT have been upgraded
	assert.False(t, ut.upgraded)
}

// errorTransport returns an error on Close()
type errorTransport struct {
	*testutil.Transport
	closeErr error
}

func (e *errorTransport) Close() error {
	_ = e.Transport.Close()
	return e.closeErr
}

func TestCloseTransportError(t *testing.T) {
	tt := testutil.NewTransport(echoHandler)
	customErr := errors.New("custom transport error")
	et := &errorTransport{Transport: tt, closeErr: customErr}

	session, err := Open(et)
	require.NoError(t, err)

	err = session.Close(context.Background())
	assert.Error(t, err)
	assert.Equal(t, customErr, err)
}

func TestNotificationHandler(t *testing.T) {
	received := make(chan string, 10)

	handler := func(ctx context.Context, msg *Message) {
		defer func() { _ = msg.Close() }()

		var notif Notification
		if err := msg.Decode(&notif); err != nil {
			t.Logf("notification handler decode failure: %v", err)
			return
		}

		received <- notif.EventTime.Format(time.RFC3339)
	}

	tt := testutil.NewTransport(echoHandler)

	// Open session
	session, err := Open(tt, WithNotifHandlerFunc(handler))
	require.NoError(t, err)
	require.NotNil(t, session.notifHandler)

	// Push an unsolicited notification
	tt.Push(`<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2024-01-05T12:34:56Z</eventTime>
</notification>`)

	// Wait for notification to be received
	select {
	case eventTime := <-received:
		assert.Equal(t, "2024-01-05T12:34:56Z", eventTime)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for notification")
	}

	_ = tt.Close()
}

func TestNotificationHandlerContextCanceled(t *testing.T) {
	handler := func(ctx context.Context, msg *Message) {
		defer func() { _ = msg.Close() }()
	}

	tt := testutil.NewTransport(echoHandler)

	session, err := Open(tt, WithNotifHandlerFunc(handler))
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()

	// Verify context is not cancelled initially
	select {
	case <-session.notifCtx.Done():
		t.Fatal("notification context should not be cancelled initially")
	default:
		// Expected
	}

	// Close session and verify context is cancelled
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
	tt := testutil.NewTransport(echoHandler)

	// Create session WITHOUT notification handler
	session, err := Open(tt)
	require.NoError(t, err)
	defer func() { _ = tt.Close() }()
	require.Nil(t, session.notifHandler)

	// Verify we can still use the session normally
	assert.NotNil(t, session.notifCtx)
	assert.NotNil(t, session.notifCancel)
}
