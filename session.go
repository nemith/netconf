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
	clientCaps []string
}

type SessionOption interface {
	apply(*sessionConfig)
}

type capabilityOpt []string

func (o capabilityOpt) apply(cfg *sessionConfig) {
	cfg.clientCaps = []string(o)
}

func WithCapability(capabilities ...string) SessionOption {
	return capabilityOpt(capabilities)
}

// Session is represents a netconf session to a one given device.
type Session struct {
	tr        transport.Transport
	sessionID uint64
	seq       atomic.Uint64

	clientCaps CapabilitySet
	serverCaps CapabilitySet

	mu      sync.Mutex
	reqs    map[string]*pendingReq
	closing bool
}

func newSession(transport transport.Transport, opts ...SessionOption) *Session {
	cfg := sessionConfig{
		clientCaps: DefaultCapabilities,
	}

	for _, opt := range opts {
		opt.apply(&cfg)
	}

	s := &Session{
		tr:         transport,
		clientCaps: NewCapabilitySet(cfg.clientCaps...),
		reqs:       make(map[string]*pendingReq),
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

	go s.recvLoop()
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
	defer func() {
		// TODO: expose this error
		_ = w.Close()
	}()

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
	defer func() {
		// TODO: expose this error
		_ = r.Close()
	}()

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
	if s.serverCaps.Has(CapNetConf11) && s.clientCaps.Has(CapNetConf11) {
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

// ClientCaps will return the capabilities initialized with the session.
func (s *Session) ClientCaps() *CapabilitySet {
	return &s.clientCaps
}

// ServerCaps will return the capabilities returned by the server in
// it's hello message.
func (s *Session) ServerCaps() *CapabilitySet {
	return &s.serverCaps
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
	closer io.Closer

	done chan struct{}
	once sync.Once
}

func (r *replyReader) Close() error {
	var err error
	r.once.Do(func() {
		err = r.closer.Close()
		close(r.done)
	})
	return err
}

// recvLoop is the main receive loop.  It runs concurrently to be able to handle
// interleaved messages (like notifications).
func (s *Session) recvLoop() {
	// buffer used to "peel" into the message enough to read the first element
	// (i.e <rpc-reply> or <notification>)
	buf := make([]byte, 4096)
	for {
		if err := s.recvMsg(buf); err != nil {
			log.Printf("netconf: failed to receive message: %v", err)
			break
		}
	}

	// Final cleanup when the loop exits
	s.mu.Lock()
	for _, req := range s.reqs {
		close(req.reply)
	}
	s.mu.Unlock()
	// TODO: expose this error
	_ = s.tr.Close()

	if !s.closing {
		log.Printf("netconf: connection closed unexpectedly")
	}
}

func getMessageID(attrs []xml.Attr) string {
	for _, attr := range attrs {
		if attr.Name.Local == "message-id" {
			return attr.Value
		}
	}
	return ""
}

func (s *Session) recvMsg(buf []byte) error {
	r, err := s.tr.MsgReader()
	if err != nil {
		return err
	}
	defer func() {
		// TODO: expose this error
		_ = r.Close()
	}()

	// 3. Peek/Read the start of the message
	n, err := r.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		// It is okay to return EOF here; recv() handles the check.
		return err
	}

	chunk := buf[:n]
	decoder := xml.NewDecoder(bytes.NewReader(chunk))

	startElem, err := startElement(decoder)
	if err != nil {
		return fmt.Errorf("failed to parse message start: %w", err)
	}

	msgReader := io.MultiReader(bytes.NewReader(chunk), r)

	switch startElem.Name {
	case xml.Name{Space: NetconfNamespace, Local: "rpc-reply"}:
		msgID := getMessageID(startElem.Attr)
		if msgID == "" {
			log.Printf("netconf: rpc-reply missing message-id")
			return nil // Continue loop
		}

		s.mu.Lock()
		req, ok := s.reqs[msgID]
		delete(s.reqs, msgID)
		s.mu.Unlock()

		if !ok {
			log.Printf("netconf: unexpected rpc-reply with message-id %s (possible timeout?)", msgID)
			return nil // Continue loop
		}

		readDone := make(chan struct{})
		reader := &replyReader{
			Reader: msgReader,
			closer: r, // The raw transport reader
			done:   readDone,
		}

		select {
		case req.reply <- &Response{
			ReadCloser: reader,
			MessageID:  msgID,
			Attributes: startElem.Attr,
		}:
			// We wait for the user to call Close() on the replyReader.
			<-readDone
			return nil

		case <-req.ctx.Done():
			return nil
		}

	default:
		return fmt.Errorf("netconf: unknown message type: %s", startElem.Name.Local)
	}
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
	if err := xml.NewEncoder(w).Encode(req.RPC); err != nil {
		_ = w.Close() // try to close anyway
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to flush request: %w", err)
	}

	// Wait for the response
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Exec issues a rpc message with `req` as the body and decodes the response into
// a pointer at `resp`.  Resp must include the full <rpc-reply> structure.
func (s *Session) Exec(ctx context.Context, operation any, reply any) error {
	req := Request{RPC: RPC{Operation: operation}}

	resp, err := s.Do(ctx, &req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Close()
	}()

	raw, err := io.ReadAll(resp)
	if err != nil {
		return fmt.Errorf("failed to read reply: %w", err)
	}

	var rpcReply RPCReply
	if err := xml.Unmarshal(raw, &rpcReply); err != nil {
		return fmt.Errorf("failed to parse rpc-reply: %w", err)
	}
	// filter out warnings
	rpcErrors := rpcReply.RPCErrors.Filter(SevError)
	if len(rpcErrors) > 0 {
		return rpcErrors
	}

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
	resp, _ := s.Do(ctx, req)
	if resp != nil {
		_ = resp.Close()
	}

	// Close the connection and ignore errors if the remote side hung up first.
	if err := s.tr.Close(); err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, syscall.EPIPE) {
		{
			return err
		}
	}

	return nil
}
