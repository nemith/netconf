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
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/nemith/netconf/transport"
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

	clientCaps          capabilitySet
	serverCaps          capabilitySet
	notificationHandler NotificationHandler

	mu      sync.Mutex
	reqs    map[uint64]*req
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
		clientCaps:          newCapabilitySet(cfg.capabilities...),
		reqs:                make(map[uint64]*req),
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
		s.tr.Close()
		return nil, err
	}

	go s.recv()
	return s, nil
}

// handshake exchanges handshake messages and reports if there are any errors.
func (s *Session) handshake() error {
	clientMsg := helloMsg{
		Capabilities: s.clientCaps.All(),
	}
	if err := s.writeMsg(&clientMsg); err != nil {
		return fmt.Errorf("failed to write hello message: %w", err)
	}

	r, err := s.tr.MsgReader()
	if err != nil {
		return err
	}
	// TODO: capture this error some how (ah defer and errors)
	defer r.Close()

	var serverMsg helloMsg
	if err := xml.NewDecoder(r).Decode(&serverMsg); err != nil {
		return fmt.Errorf("failed to read server hello message: %w", err)
	}

	if serverMsg.SessionID == 0 {
		return fmt.Errorf("server did not return a session-id")
	}

	if len(serverMsg.Capabilities) == 0 {
		return fmt.Errorf("server did not return any capabilities")
	}

	s.serverCaps = newCapabilitySet(serverMsg.Capabilities...)
	s.sessionID = serverMsg.SessionID

	// upgrade the transport if we are on a larger version and the transport
	// supports it.
	const baseCap11 = baseCap + ":1.1"
	if s.serverCaps.Has(baseCap11) && s.clientCaps.Has(baseCap11) {
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
func (s *Session) ClientCapabilities() []string {
	return s.clientCaps.All()
}

// ServerCapabilities will return the capabilities returned by the server in
// it's hello message.
func (s *Session) ServerCapabilities() []string {
	return s.serverCaps.All()
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

type req struct {
	reply chan Reply
	ctx   context.Context
}

func (s *Session) recvMsg() error {
	r, err := s.tr.MsgReader()
	if err != nil {
		return err
	}
	defer r.Close()

	msg, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	return s.parseMsg(msg)
}

func (s *Session) parseMsg(msg []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(msg))

	root, err := startElement(dec)
	if err != nil {
		return err
	}

	switch root.Name {
	case RPCReplyName:
		reply := Reply{raw: msg}
		if err := dec.DecodeElement(&reply, root); err != nil {
			// What should we do here?  Kill the connection?
			return fmt.Errorf("failed to decode rpc-reply message: %w", err)
		}
		ok, req := s.req(reply.MessageID)
		if !ok {
			return fmt.Errorf("cannot find reply channel for message-id: %d", reply.MessageID)
		}

		select {
		case req.reply <- reply:
			return nil
		case <-req.ctx.Done():
			return fmt.Errorf("message %d context canceled: %s", reply.MessageID, req.ctx.Err().Error())
		}

	case NofificationName:
		if s.notificationHandler == nil {
			return nil
		}
		notif := Notification{raw: msg}
		if err := dec.DecodeElement(&notif, root); err != nil {
			return fmt.Errorf("failed to decode notification message: %w", err)
		}
		s.notificationHandler(notif)

	default:
		return fmt.Errorf("unknown message type: %q", root.Name.Local)
	}
	return nil
}

// recv is the main receive loop.  It runs concurrently to be able to handle
// interleaved messages (like notifications).
func (s *Session) recv() {
	var err error
	var opErr *net.OpError

	for {
		err = s.recvMsg()
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &opErr) {
			break
		}
		if err != nil {
			log.Printf("netconf: failed to read incoming message: %v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all outstanding requests
	for _, req := range s.reqs {
		close(req.reply)
	}

	if !s.closing {
		log.Printf("netconf: connection closed unexpectedly")
	}
}

func (s *Session) req(msgID uint64) (bool, *req) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.reqs[msgID]
	if !ok {
		return false, nil
	}
	delete(s.reqs, msgID)
	return true, req
}

func (s *Session) writeMsg(v any) error {
	w, err := s.tr.MsgWriter()
	if err != nil {
		return err
	}

	if err := xml.NewEncoder(w).Encode(v); err != nil {
		return err
	}
	return w.Close()
}

func (s *Session) send(ctx context.Context, msg *request) (chan Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeMsg(msg); err != nil {
		return nil, err
	}

	// cap of 1 makes sure we don't block on send
	ch := make(chan Reply, 1)
	s.reqs[msg.MessageID] = &req{
		reply: ch,
		ctx:   ctx,
	}

	return ch, nil
}

// Do issues a rpc call for the given NETCONF operation returning a Reply.  RPC
// errors (i.e erros in the `<rpc-errors>` section of the `<rpc-reply>`) are
// converted into go errors automatically.  Instead use `reply.Err()` or
// `reply.RPCErrors` to access the errors and/or warnings.
func (s *Session) Do(ctx context.Context, req any) (*Reply, error) {
	msg := &request{
		MessageID: s.seq.Add(1),
		Operation: req,
	}

	ch, err := s.send(ctx, msg)
	if err != nil {
		return nil, err
	}

	// wait for reply or context to be cancelled.
	select {
	case reply, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		return &reply, nil
	case <-ctx.Done():
		// remove any existing request
		s.mu.Lock()
		delete(s.reqs, msg.MessageID)
		s.mu.Unlock()

		return nil, ctx.Err()
	}
}

// Call issues a rpc message with `req` as the body and decodes the reponse into
// a pointer at `resp`.  Any Call errors are presented as a go error.
func (s *Session) Call(ctx context.Context, req any, resp any) error {
	reply, err := s.Do(ctx, &req)
	if err != nil {
		return err
	}

	// Return any <rpc-error>.  This defaults to a severity of `error` (warning
	// are omitted).
	if err := reply.Err(); err != nil {
		return err
	}

	if err := reply.Decode(&resp); err != nil {
		return err
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
	_, callErr := s.Do(ctx, &closeSession{})

	// Close the connection and ignore errors if the remote side hung up first.
	if err := s.tr.Close(); err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, syscall.EPIPE) {
		{
			return err
		}
	}

	// it's ok if we are already closed
	if !errors.Is(callErr, io.EOF) &&
		!errors.Is(callErr, ErrClosed) {
		return callErr
	}

	return nil
}
