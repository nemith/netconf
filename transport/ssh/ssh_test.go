package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/carlmjohnson/be"
	"golang.org/x/crypto/ssh"
)

type testServer struct {
	t               *testing.T // Add this field
	listener        net.Listener
	config          *ssh.ServerConfig
	errCh           chan error
	RejectSubsystem bool
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	be.NilErr(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	be.NilErr(t, err)

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "localhost:0")
	be.NilErr(t, err)

	return &testServer{
		t:        t,
		listener: ln,
		config:   config,
		errCh:    make(chan error, 1),
	}
}

func (s *testServer) Addr() string { return s.listener.Addr().String() }

func (s *testServer) Serve(handler func(ssh.Channel) error) {
	go func() {
		defer close(s.errCh)
		defer func() {
			if err := s.listener.Close(); err != nil {
				// We often ignore "use of closed network connection"
				// but logging it hurts nothing in verbose mode.
				s.t.Logf("testServer listener close: %v", err)
			}
		}()

		conn, err := s.listener.Accept()
		if err != nil {
			s.errCh <- fmt.Errorf("accept: %w", err)
			return
		}

		_, chans, reqs, err := ssh.NewServerConn(conn, s.config)
		if err != nil {
			s.errCh <- fmt.Errorf("handshake: %w", err)
			return
		}
		go ssh.DiscardRequests(reqs)

		for newChannel := range chans {
			if newChannel.ChannelType() != "session" {
				err := newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
				if err != nil {
					s.t.Logf("failed to reject channel: %v", err)
				}
				continue
			}
			ch, reqs, err := newChannel.Accept()
			if err != nil {
				s.errCh <- fmt.Errorf("channel accept: %w", err)
				return
			}

			go func(in <-chan *ssh.Request) {
				for req := range in {
					// In a real server we'd check payload == "netconf",
					// but for tests accepting any subsystem is fine.
					if req.Type == "subsystem" {
						if err := req.Reply(!s.RejectSubsystem, nil); err != nil {
							s.t.Logf("failed to reply to subsystem req: %v", err)
						}
					}
				}
			}(reqs)

			if err := handler(ch); err != nil {
				s.errCh <- err
			}
			return
		}
	}()
}

func (s *testServer) Wait(t *testing.T) error {
	t.Helper()
	err := <-s.errCh
	return err
}

func TestTransport_Dial(t *testing.T) {
	srv := newTestServer(t)
	var serverSeen []byte

	srv.Serve(func(ch ssh.Channel) error {
		if _, err := io.WriteString(ch, "muffins]]>]]>"); err != nil {
			return err
		}

		var err error
		serverSeen, err = io.ReadAll(ch)
		return err
	})

	config := &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)
	be.NilErr(t, err)

	r, err := tr.MsgReader()
	be.NilErr(t, err)
	greeting, _ := io.ReadAll(r)
	be.Equal(t, "muffins", string(greeting))

	w, err := tr.MsgWriter()
	be.NilErr(t, err)

	out := "a man a plan a canal panama"
	_, err = io.WriteString(w, out)
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	err = tr.Close()
	be.NilErr(t, err)

	be.NilErr(t, srv.Wait(t))
	be.Equal(t, "a man a plan a canal panama]]>]]>", string(serverSeen))
}

func TestTransport_Dial_NetworkFailure(t *testing.T) {
	// Try to dial a port that is definitely closed (port 1 on localhost)
	config := &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}

	// Use a short timeout so the test is fast
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tr, err := Dial(ctx, "tcp", "127.0.0.1:1", config)

	be.Nonzero(t, err)
	be.Zero(t, tr)
	// Assert it's a network error
	be.In(t, "connection refused", err.Error())
}

func TestTransport_Dial_AuthFailure(t *testing.T) {
	srv := newTestServer(t)
	// Force the server to require a password, but provide none
	srv.config.NoClientAuth = false
	srv.config.PasswordCallback = func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		return nil, fmt.Errorf("password rejected")
	}

	// We don't need srv.Serve here because the handshake will fail
	// before the handler is ever reached.
	// But we must start the listener loop:
	srv.Serve(func(ch ssh.Channel) error { return nil })

	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		// No Auth methods provided = Auth Failure
	}

	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)

	be.Nonzero(t, err)
	be.Zero(t, tr)
	be.True(t, err != nil && strings.Contains(err.Error(), "unable to authenticate"))

	// For some reason ErrorsIs doesn't work here despite ssh.ErrNoAuth existing.
	err = srv.Wait(t)
	be.True(t, err != nil && strings.Contains(err.Error(), "no auth passed yet"))
}

func TestTransport_DialContextCancel(t *testing.T) {
	// Standard hanging listener pattern (no changes needed here)
	ln, err := net.Listen("tcp", "localhost:0")
	be.NilErr(t, err)
	defer func() {
		if err := ln.Close(); err != nil {
			t.Logf("failed to close listener: %v", err)
		}
	}()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			if _, err := io.Copy(io.Discard, conn); err != nil {
				t.Logf("failed to copy from conn: %v", err)
			}
		}
	}()

	config := &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = Dial(ctx, "tcp", ln.Addr().String(), config)

	be.True(t, errors.Is(err, context.DeadlineExceeded))
	be.True(t, time.Since(start) <= 200*time.Millisecond)
}

func TestTransport_Dial_SubsystemFails(t *testing.T) {
	srv := newTestServer(t)
	srv.RejectSubsystem = true

	srv.Serve(func(ch ssh.Channel) error {
		_, err := io.ReadAll(ch)
		return err
	})

	config := &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}

	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)

	// Dial should fail because the subsystem request was rejected
	be.Nonzero(t, err)
	be.Zero(t, tr)

	// Ensure the server finishes cleanly (client should close connection on error)
	be.NilErr(t, srv.Wait(t))
}

func TestTransport_MultipleMessages(t *testing.T) {
	srv := newTestServer(t)
	var serverSeen []byte

	srv.Serve(func(ch ssh.Channel) error {
		_, err := io.WriteString(ch, "muffins]]>]]>")
		if err != nil {
			return err
		}

		serverSeen, err = io.ReadAll(ch)
		return err
	})

	config := &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)
	be.NilErr(t, err)

	r, _ := tr.MsgReader()
	_, err = io.ReadAll(r) // Clear greeting
	be.NilErr(t, err)

	w, _ := tr.MsgWriter()
	_, err = io.WriteString(w, "msg1")
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	w, _ = tr.MsgWriter()
	_, err = io.WriteString(w, "msg2")
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	err = tr.Close()
	be.NilErr(t, err)

	be.NilErr(t, srv.Wait(t))

	be.Equal(t, "msg1]]>]]>msg2]]>]]>", string(serverSeen))
}
