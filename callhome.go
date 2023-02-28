package netconf

import (
	"crypto/tls"
	"fmt"
	"github.com/nemith/netconf/transport"
	ncssh "github.com/nemith/netconf/transport/ssh"
	nctls "github.com/nemith/netconf/transport/tls"
	"golang.org/x/crypto/ssh"
	"net"
)

type TransportType uint

const (
	TransportTypeSSH TransportType = iota
	TransportTypeTLS
)

/*
CallHome implements netconf callhome procedure as specified in RFC 8071
*/
type CallHome struct {
	listener  net.Listener
	network   string
	addr      string
	transport TransportType
	configSSH *ssh.ClientConfig
	configTLS *tls.Config
	session   *Session
}

type CallHomeOption func(*CallHome)

// WithAddress sets the address (as required by net.Listen) the CallHome server listen to
func WithAddress(addr string) CallHomeOption {
	return func(ch *CallHome) {
		ch.addr = addr
	}
}

// WithNetwork set the network (as required by net.Listen) the CallHome server listen to
func WithNetwork(network string) CallHomeOption {
	return func(ch *CallHome) {
		ch.network = network
	}
}

// WithTransport sets the transport type used by the CallHome server
func WithTransport(t TransportType) CallHomeOption {
	return func(ch *CallHome) {
		ch.transport = t
	}
}

// WithConfigSSH sets the ssh.ClientConfig used by the CallHome server to upgrade incoming connections
func WithConfigSSH(sc *ssh.ClientConfig) CallHomeOption {
	return func(ch *CallHome) {
		ch.configSSH = sc
	}
}

// WithConfigTLS sets the tls.Config used by the CallHome server to upgrade incoming connections
func WithConfigTLS(tc *tls.Config) CallHomeOption {
	return func(ch *CallHome) {
		ch.configTLS = tc
	}
}

// NewCallHome creates a CallHome client
func NewCallHome(opts ...CallHomeOption) (*CallHome, error) {
	const (
		defaultAddress   = "0.0.0.0:4334"
		defaultNetwork   = "tcp"
		defaultTransport = TransportTypeSSH
	)

	ch := &CallHome{
		addr:      defaultAddress,
		network:   defaultNetwork,
		transport: defaultTransport,
	}

	for _, opt := range opts {
		opt(ch)
	}

	if ch.configTLS == nil && ch.configSSH == nil {
		return nil, fmt.Errorf("one of TLS or SSH configuration must be specified, depending on selected transport")
	}

	if ch.network != "tcp" && ch.network != "tcp4" && ch.network != "tcp6" {
		return nil, fmt.Errorf("invalid network, must be one of: tcp, tcp4, tcp6")
	}

	return ch, nil
}

// Listen waits for incoming callhome connections
// NOTE: only handles one connection at a time
func (ch *CallHome) Listen() error {
	ln, err := net.Listen(ch.network, ch.addr)
	if err != nil {
		return err
	}
	ch.listener = ln
	defer func() {
		_ = ch.Close()
	}()

	conn, err := ch.listener.Accept()
	if err != nil {
		return err
	}
	err = ch.handleConnection(conn)
	if err != nil {
		return err
	}
	return nil
}

// handleConnection upgrade input net.Conn to establish a netconf session
func (ch *CallHome) handleConnection(conn net.Conn) error {
	var t transport.Transport
	var err error
	// TODO: Missing certificates validation as specified in rfc8071 3.1
	switch ch.transport {
	case TransportTypeSSH:
		t, err = ncssh.DialWithConn(conn.RemoteAddr().String(), ch.configSSH, conn)
		if err != nil {
			return err
		}
	case TransportTypeTLS:
		t, err = nctls.DialWithConn(ch.configTLS, conn)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid transport type")
	}
	s, err := Open(t)
	if err != nil {
		return err
	}
	ch.session = s
	return nil
}

// Session gets the callhome netconf session
func (ch *CallHome) Session() *Session {
	return ch.session
}

// Close terminates the callhome server connection
func (ch *CallHome) Close() error {
	return ch.listener.Close()
}
