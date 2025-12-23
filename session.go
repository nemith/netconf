package netconf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"nemith.io/netconf/transport"
)

const (
	NetconfNamespace      = "urn:ietf:params:xml:ns:netconf:base:1.0"
	NotificationNamespace = "urn:ietf:params:xml:ns:netconf:notification:1.0"
)

var ErrClosed = errors.New("closed connection")

type sessionConfig struct {
	capabilities        []string
	notificationHandler NotificationHandler
}

type SessionOption interface {
	apply(*sessionConfig)
}

type capabilityOpt []string

func (o capabilityOpt) apply(cfg *sessionConfig) {
	for _, cap := range o {
		cfg.capabilities = append(cfg.capabilities, cap)
	}
}

func WithCapability(capabilities ...string) SessionOption {
	return capabilityOpt(capabilities)
}

type notificationHandlerOpt NotificationHandler

func (o notificationHandlerOpt) apply(cfg *sessionConfig) {
	cfg.notificationHandler = NotificationHandler(o)
}

func WithNotificationHandler(nh NotificationHandler) SessionOption {
	return notificationHandlerOpt(nh)
}

// Session is represents a netconf session to a one given device.
type Session struct {
	tr        transport.Transport
	sessionID uint64
	seq       atomic.Uint64

	clientCaps          CapabilitySet
	serverCaps          CapabilitySet
	notificationHandler NotificationHandler

	mu      sync.Mutex
	reqs    map[string]*pendingReq
	closing bool
}

// NotificationHandler function allows to work with received notifications.
// A NotificationHandler function can be passed in as an option when calling Open method of Session object
// A typical use of the NofificationHandler function is to retrieve notifications once they are received so
// that they can be parsed and/or stored somewhere.
type NotificationHandler func(msg Notification)

func newSession(transport transport.Transport, opts ...SessionOption) *Session {
	cfg := sessionConfig{
		capabilities: DefaultCapabilities,
	}

	for _, opt := range opts {
		opt.apply(&cfg)
	}

	s := &Session{
		tr:                  transport,
		clientCaps:          NewCapabilitySet(cfg.capabilities...),
		reqs:                make(map[string]*pendingReq),
		notificationHandler: cfg.notificationHandler,
	}
	return s
}

// Open will create a new Session with th=e given transport and open it with the
// necessary hello messages.
func Open(transport transport.Transport, opts ...SessionOption) (*Session, error) {
	s := newSession(transport, opts...)

	// this needs a timeout of some sort.
	if err := s.handshake(); err != nil {
		s.tr.Close() // nolint:errcheck // TODO: catch and log err
		return nil, err
	}

	go s.recv()
	return s, nil
}

// handshake exchanges handshake messages and reports if there are any errors.
func (s *Session) handshake() error {
	clientMsg := HelloMsg{
		Capabilities: slices.Collect(s.clientCaps.All()),
	}

	w, err := s.tr.MsgWriter()
	if err != nil {
		return fmt.Errorf("failed to get hello message writer: %w", err)
	}
	defer w.Close()

	if err := xml.NewEncoder(w).Encode(&clientMsg); err != nil {
		return fmt.Errorf("failed to write hello message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close hello message writer: %w", err)
	}

	r, err := s.tr.MsgReader()
	if err != nil {
		return fmt.Errorf("failed to get hello message reader: %w", err)
	}
	defer r.Close()

	var serverMsg HelloMsg
	if err := xml.NewDecoder(r).Decode(&serverMsg); err != nil {
		return fmt.Errorf("failed to read server hello message: %w", err)
	}

	if serverMsg.SessionID == 0 {
		return fmt.Errorf("server did not return a session-id")
	}

	if len(serverMsg.Capabilities) == 0 {
		return fmt.Errorf("server did not return any capabilities")
	}

	s.serverCaps = NewCapabilitySet(serverMsg.Capabilities...)
	s.sessionID = serverMsg.SessionID

	// upgrade the transport if we are on a larger version and the transport
	// supports it.
	if s.serverCaps.Has(CapNetConfig11) && s.clientCaps.Has(CapNetConfig11) {
		if upgrader, ok := s.tr.(interface{ Upgrade() }); ok {
			upgrader.Upgrade()
		}
	}

	return nil
}

// SessionID returns the current session ID exchanged in the hello messages.
// Will return 0 if there is no session ID.
func (s *Session) SessionID() uint64 {
	return s.sessionID
}

// ClientCapabilities will return the capabilities initialized with the session.
func (s *Session) ClientCapabilities() CapabilitySet {
	return s.clientCaps
}

// ServerCapabilities will return the capabilities returned by the server in
// it's hello message.
func (s *Session) ServerCapabilities() CapabilitySet {
	return s.serverCaps
}

// startElement will walk though a xml.Decode until it finds a start element
// and returns it.
func startElement(d *xml.Decoder) (*xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}

		if start, ok := tok.(xml.StartElement); ok {
			return &start, nil
		}
	}
}

type pendingReq struct {
	reply chan *Response
	ctx   context.Context
}

type replyReader struct {
	io.Reader
	io.Closer
}

// recv is the main receive loop.  It runs concurrently to be able to handle
// interleaved messages (like notifications).
func (s *Session) recv() {
	peekBuf := make([]byte, 4096)

	for {
		r, err := s.tr.MsgReader()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("netconf: failed to get message reader: %v", err)
			}
			break
		}

		n, err := r.Read(peekBuf)
		if err != nil {
			r.Close() // Clean up transport
			break
		}

		chunk := peekBuf[:n]
		decoder := xml.NewDecoder(bytes.NewReader(chunk))

		startElem, err := startElement(decoder)
		if err != nil {
			log.Printf("netconf: failed to parse message: %v", err)
			break
		}

		msgReader := io.MultiReader(bytes.NewReader(chunk), r)

		switch startElem.Name {
		case xml.Name{Space: NetconfNamespace, Local: "notification"}:
			// Notifications are handled internally. We must consume the stream
			// so the transport is ready for the next message.
			if s.notificationHandler != nil {
				// Buffer the notification in memory to unmarshal it
				var buf bytes.Buffer
				buf.ReadFrom(msgReader) // Drain everything

				var notif Notification
				if err := xml.Unmarshal(buf.Bytes(), &notif); err == nil {
					s.notificationHandler(notif)
				}
			} else {
				// No handler? Discard to clear the pipe.
				io.Copy(io.Discard, msgReader)
			}
			r.Close()
		case xml.Name{Space: NetconfNamespace, Local: "rpc-reply"}:
			// Extract message-id
			var msgID string
			for _, attr := range startElem.Attr {
				if attr.Name.Local == "message-id" {
					msgID = attr.Value
					break
				}
			}
			if msgID == "" {
				log.Printf("netconf: rpc-reply missing message-id")
				r.Close()
				continue
			}

			s.mu.Lock()
			req, ok := s.reqs[msgID]
			delete(s.reqs, msgID)
			s.mu.Unlock()
			if !ok {
				log.Printf("netconf: unexpected rpc-reply with message-id %s", msgID)
				// Unsolicited/Timed-out reply. Drain and ignore.
				r.Close()
				continue
			}

			// Create a "tracker" that blocks THIS loop until the user closes the stream.
			reader := &replyReader{
				Reader: msgReader,
				Closer: r,
			}

			// Hand off to the user
			select {
			case req.reply <- &Response{
				ReadCloser: reader,
				MessageID:  msgID,
				Attributes: startElem.Attr,
			}:
			case <-req.ctx.Done():
				// User gave up. Drain the stream ourselves.
				r.Close()
			}

		default:
			log.Printf("netconf: unknown message type: %s", startElem.Name.Local)
			r.Close()
		}
	}

	// Cleanup
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, req := range s.reqs {
		close(req.reply)
	}

	if !s.closing {
		log.Printf("netconf: connection closed unexpectedly")
	}
}

func (s *Session) removeReq(msgID string) {
	s.mu.Lock()
	delete(s.reqs, msgID)
	s.mu.Unlock()
}

// Do issues a rpc message for the given Request.  This is a low-level method
// that doesn't try to decode the response including any rpc-errors.
func (s *Session) Do(ctx context.Context, req *Request) (*Response, error) {
	msgID := strconv.FormatUint(s.seq.Add(1), 10)
	req.RPC.MessageID = msgID

	// Setup channel
	ch := make(chan *Response, 1)
	s.mu.Lock()
	s.reqs[msgID] = &pendingReq{
		reply: ch,
		ctx:   ctx,
	}
	s.mu.Unlock()

	// Cleanup if context triggers before send/recv
	defer func() {
		s.mu.Lock()
		delete(s.reqs, msgID)
		s.mu.Unlock()
	}()

	w, err := s.tr.MsgWriter()
	if err != nil {
		return nil, fmt.Errorf("failed to get message writer: %w", err)
	}
	if err := xml.NewEncoder(w).Encode(req); err != nil {
		w.Close() // try to close anyway
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to flush request: %w", err)
	}

	// Wait
	select {
	case reply, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		return reply, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Exec issues a rpc message with `req` as the body and decodes the reponse into
// a pointer at `resp`.  Resp must include the full <rpc-reply> structure.
func (s *Session) Exec(ctx context.Context, operation any, reply any) error {
	req := Request{RPC: RPC{Operation: operation}}

	resp, err := s.Do(ctx, &req)
	if err != nil {
		return err
	}
	defer resp.Close()

	raw, err := io.ReadAll(resp)
	if err != nil {
		return fmt.Errorf("failed to read reply: %w", err)
	}

	// TODO: check for rpc errors

	if reply != nil {
		if err := xml.Unmarshal(raw, reply); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// Close will gracefully close the sessions first by sending a `close-session`
// operation to the remote and then closing the underlying transport
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()

	type closeSession struct {
		XMLName xml.Name `xml:"close-session"`
	}

	// This may fail so save the error but still close the underlying transport.
	req := NewRequest(&closeSession{})
	reply, callErr := s.Do(ctx, req)
	reply.Close()

	// Close the connection and ignore errors if the remote side hung up first.
	if err := s.tr.Close(); err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, syscall.EPIPE) {
		{
			return err
		}
	}

	if !errors.Is(callErr, io.EOF) {
		return callErr
	}

	return nil
}
