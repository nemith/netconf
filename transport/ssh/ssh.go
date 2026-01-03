package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"golang.org/x/crypto/ssh"
	"nemith.io/netconf/transport"
)

// alias it to a private type so we can make it private when embedding
type framer = transport.Framer

// Transport implements RFC6242 for implementing NETCONF protocol over SSH.
type Transport struct {
	c     *ssh.Client
	sess  *ssh.Session
	stdin io.WriteCloser

	// set to true if the transport is managing the underlying ssh connection
	// and should close it when the transport is closed.  This is is set to true
	// when used with `Dial`.
	managedConn bool

	*framer
}

// Dial will connect to a ssh server and issues a transport, it's used as a
// convenience function as essentially is the same as
//
//		c, err := ssh.Dial(network, addr, config)
//	 	if err != nil { /* ... handle error ... */ }
//	 	t, err := NewTransport(c)
//
// When the transport is closed the underlying connection is also closed.
func Dial(ctx context.Context, network, addr string, config *ssh.ClientConfig) (*Transport, error) {
	d := net.Dialer{Timeout: config.Timeout}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	// Setup a go routine to monitor the context and close the connection.  This
	// is needed as the ssh library doesn't support contexts for dialing so this
	// approximates a context based cancelation/timeout for the ssh handshake.
	//
	// Since writing this code, Go 1.20 added DialContext to ssh.ClientConn but it doesn't support a custom configuration
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		// ssh.NewClientConn closes the underlying connection so no need to call conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	t, err := newTransport(client, true)
	if err != nil {
		// Close the client to not leak it on transport failure.
		_ = client.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	return t, nil
}

// NewTransport will create a new ssh transport as defined in RFC6242 for use
// with netconf.  Unlike Dial, the underlying client will not be automatically
// closed when the transport is closed (however any sessions and subsystems
// are still closed).
func NewTransport(client *ssh.Client) (*Transport, error) {
	return newTransport(client, false)
}

func newTransport(client *ssh.Client, managed bool) (*Transport, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh session: %w", err)
	}

	w, err := sess.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	r, err := sess.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	const subsystem = "netconf"
	if err := sess.RequestSubsystem(subsystem); err != nil {
		return nil, fmt.Errorf("failed to start netconf ssh subsytem: %w", err)
	}

	return &Transport{
		c:           client,
		managedConn: managed,
		sess:        sess,
		stdin:       w,

		framer: transport.NewFramer(r, w),
	}, nil
}

// Close will close the underlying transport. If the connection was created
// with Dial then underlying ssh.Client is closed as well.  If not only
// the sessions is closed.
func (t *Transport) Close() error {
	// will save previous errors but try to close everything returning just the
	// "lowest" abstraction layer error
	var retErr error

	if err := t.stdin.Close(); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("failed to close ssh stdin: %w", err))
	}

	if err := t.sess.Close(); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("failed to close ssh channel: %w", err))
	}

	// If this is a "managed connection" (i.e one created with Dial) then we are
	// responsible to close the connection.
	if t.managedConn {
		if err := t.c.Close(); err != nil {
			return errors.Join(retErr, fmt.Errorf("failed to close ssh connection: %w", err))
		}
	}

	return retErr
}
