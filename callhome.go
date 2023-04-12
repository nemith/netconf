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

// CallHomeTransport interface allows for upgrading an incoming callhome TCP connection into a transport
type CallHomeTransport interface {
	DialWithConn(conn net.Conn) (transport.Transport, error)
}

// SSHCallHomeTransport implements the CallHomeTransport for SSH
type SSHCallHomeTransport struct {
	Config *ssh.ClientConfig
}

// DialWithConn is same as Dial but creates the transport on top of input net.Conn
func (t *SSHCallHomeTransport) DialWithConn(conn net.Conn) (transport.Transport, error) {
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, conn.RemoteAddr().String(), t.Config)
	if err != nil {
		return nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	return ncssh.NewTransport(client)
}

// TLSCallHomeTransport implements the CallHomeTransport on TLS
type TLSCallHomeTransport struct {
	Config *tls.Config
}

// DialWithConn is same as Dial but creates the transport on top of input net.Conn
func (t *TLSCallHomeTransport) DialWithConn(conn net.Conn) (transport.Transport, error) {
	tlsConn := tls.Client(conn, t.Config)
	return nctls.NewTransport(tlsConn), nil
}

/*
CallHomeClient holds connecting callhome device information
*/
type CallHomeClient struct {
	session   *Session
	Transport CallHomeTransport
	Address   string
}

/*
CallHomeServer implements netconf callhome procedure as specified in RFC 8071
*/
type CallHomeServer struct {
	listener net.Listener
	network  string
	addr     string
	clients  map[string]*CallHomeClient
}

type CallHomeOption func(*CallHomeServer)

// WithAddress sets the address (as required by net.Listen) the CallHomeServer server listen to
func WithAddress(addr string) CallHomeOption {
	return func(ch *CallHomeServer) {
		ch.addr = addr
	}
}

// WithNetwork set the network (as required by net.Listen) the CallHomeServer server listen to
func WithNetwork(network string) CallHomeOption {
	return func(ch *CallHomeServer) {
		ch.network = network
	}
}

// WithCallHomeClient set the netconf callhome clients
func WithCallHomeClient(chc ...*CallHomeClient) CallHomeOption {
	return func(chs *CallHomeServer) {
		for _, c := range chc {
			chs.clients[c.Address] = c
		}
	}
}

// NewCallHomeServer creates a CallHomeServer client
func NewCallHomeServer(opts ...CallHomeOption) (*CallHomeServer, error) {
	const (
		defaultAddress = "0.0.0.0:4334"
		defaultNetwork = "tcp"
	)

	ch := &CallHomeServer{
		addr:    defaultAddress,
		network: defaultNetwork,
		clients: map[string]*CallHomeClient{},
	}

	for _, opt := range opts {
		opt(ch)
	}

	if ch.network != "tcp" && ch.network != "tcp4" && ch.network != "tcp6" {
		return nil, fmt.Errorf("invalid network, must be one of: tcp, tcp4, tcp6")
	}

	return ch, nil
}

// Listen waits for incoming callhome connections
func (chs *CallHomeServer) Listen() error {
	ln, err := net.Listen(chs.network, chs.addr)
	if err != nil {
		return err
	}
	chs.listener = ln
	defer func() {
		_ = chs.Close()
	}()
	for {
		conn, err := chs.listener.Accept()
		if err != nil {
			return err
		}
		go func() {
			err := chs.handleConnection(conn)
			if err != nil {
				fmt.Printf("error handling callhome connection from address: %s, error: %s \n", conn.RemoteAddr(), err)
			}
		}()
	}
}

// handleConnection upgrade input net.Conn to establish a netconf session
func (chs *CallHomeServer) handleConnection(conn net.Conn) error {
	fmt.Printf("handling connection from: %s\n", conn.RemoteAddr().String())
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("callhome only supports tcp")
	}
	chc, ok := chs.clients[addr.IP.String()]
	if !ok {
		return fmt.Errorf("no client configured for address %s", addr)
	}

	t, err := chc.Transport.DialWithConn(conn)

	s, err := Open(t)
	if err != nil {
		return err
	}
	chs.clients[addr.IP.String()].session = s
	return nil
}

// Close terminates the callhome server connection
func (chs *CallHomeServer) Close() error {
	return chs.listener.Close()
}

// ClientSession returns the netconf session given callhome client IP address
func (chs *CallHomeServer) ClientSession(clientIP string) (*Session, error) {
	c, ok := chs.clients[clientIP]
	if !ok {
		return nil, fmt.Errorf("no callhome client with IP %s", clientIP)
	}
	if c.session == nil {
		return nil, fmt.Errorf("no active callhome session for client %s", clientIP)
	}
	return chs.clients[clientIP].session, nil
}
