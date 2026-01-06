package netconf

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nemith.io/netconf/transport"
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
			tr := &transport.TestTransport{}
			sess := &Session{tr: tr}

			tr.AddResponse(tc.serverHello)

			err := sess.handshake()
			if !tc.shouldError {
				assert.NoError(t, err)
			}

			assert.Len(t, tr.Outputs(), 1)
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
	tt.AddResponse(helloGood)

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
