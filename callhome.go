package netconf

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/nemith/netconf/transport"
	ncssh "github.com/nemith/netconf/transport/ssh"
	nctls "github.com/nemith/netconf/transport/tls"
	"golang.org/x/crypto/ssh"
	"net"
)

var ErrNoClientConfig = errors.New("missing transport configuration")

// CallHomeTransport interface allows for upgrading an incoming callhome TCP connection into a transport
type CallHomeTransport interface {
	DialWithConn(conn net.Conn) (transport.Transport, error)
}

// SSHCallHomeTransport implements the CallHomeTransport on SSH
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
CallHomeClientConfig holds connecting callhome device information
*/
type CallHomeClientConfig struct {
	Transport CallHomeTransport
	Address   string
}

type CallHomeClient struct {
	session *Session
	*CallHomeClientConfig
}

func (chc *CallHomeClient) Session() *Session {
	return chc.session
}

type ClientError struct {
	Address string
	Err     error
}

func (ce *ClientError) Error() string {
	return fmt.Sprintf("client %s: %s", ce.Address, ce.Err.Error())
}

/*
CallHomeServer implements netconf callhome procedure as specified in RFC 8071
*/
type CallHomeServer struct {
	listener       net.Listener
	network        string
	addr           string
	clientsConfig  map[string]*CallHomeClientConfig
	clientsChannel chan *CallHomeClient
	errorChannel   chan *ClientError
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

// WithCallHomeClientConfig set the netconf callhome clientsConfig
func WithCallHomeClientConfig(chc ...*CallHomeClientConfig) CallHomeOption {
	return func(chs *CallHomeServer) {
		for _, c := range chc {
			chs.clientsConfig[c.Address] = c
		}
	}
}

// NewCallHomeServer creates a CallHomeServer
func NewCallHomeServer(opts ...CallHomeOption) (*CallHomeServer, error) {
	const (
		defaultAddress = "0.0.0.0:4334"
		defaultNetwork = "tcp"
	)

	ch := &CallHomeServer{
		addr:           defaultAddress,
		network:        defaultNetwork,
		clientsConfig:  map[string]*CallHomeClientConfig{},
		clientsChannel: make(chan *CallHomeClient),
		errorChannel:   make(chan *ClientError),
	}

	for _, opt := range opts {
		opt(ch)
	}

	if ch.network != "tcp" && ch.network != "tcp4" && ch.network != "tcp6" {
		return nil, fmt.Errorf("invalid network, must be one of: tcp, tcp4, tcp6")
	}

	return ch, nil
}

// Listen waits for incoming callhome connections and handles them.
// Send ClientError messages to the ErrChan whenever a callhome connection to a host fails and
// send a new CallHomeClient every time a callhome connection is successful
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
			chc, err := chs.handleConnection(conn)
			if err != nil {
				chs.errorChannel <- &ClientError{
					Address: conn.RemoteAddr().String(),
					Err:     err,
				}
			} else {
				chs.clientsChannel <- chc
			}
		}()
	}
}

// handleConnection upgrade input net.Conn to establish a netconf session
func (chs *CallHomeServer) handleConnection(conn net.Conn) (*CallHomeClient, error) {
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return nil, errors.New("invalid network connection, callhome support tcp only")
	}
	chcc, ok := chs.clientsConfig[addr.IP.String()]
	if !ok {
		return nil, ErrNoClientConfig
	}

	t, err := chcc.Transport.DialWithConn(conn)
	if err != nil {
		return nil, err
	}

	s, err := Open(t)
	if err != nil {
		return nil, err
	}

	return &CallHomeClient{
		session:              s,
		CallHomeClientConfig: chcc,
	}, nil
}

// Close terminates the callhome server connection
func (chs *CallHomeServer) Close() error {
	return chs.listener.Close()
}

func (chs *CallHomeServer) ErrorChannel() chan *ClientError {
	return chs.errorChannel
}

func (chs *CallHomeServer) CallHomeClientChannel() chan *CallHomeClient {
	return chs.clientsChannel
}

// SetCallHomeClientConfig adds a new callhome client configuration to the callhome server
func (chs *CallHomeServer) SetCallHomeClientConfig(chcc *CallHomeClientConfig) {
	chs.clientsConfig[chcc.Address] = chcc
}
