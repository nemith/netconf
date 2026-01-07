package tls

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/carlmjohnson/be"
)

// testServer handles the boilerplate of a one-shot TLS server
type testServer struct {
	t        *testing.T
	listener net.Listener
	config   *tls.Config
	errCh    chan error
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	// 1. Generate ephemeral self-signed cert
	cert, err := generateSelfSignedCert()
	be.NilErr(t, err)

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	ln, err := net.Listen("tcp", "localhost:0")
	be.NilErr(t, err)

	return &testServer{
		t:        t,
		listener: ln,
		config:   config,
		errCh:    make(chan error, 1),
	}
}

func (s *testServer) Addr() string {
	return s.listener.Addr().String()
}

// Serve accepts ONE connection, performs the TLS handshake,
// and hands the connection to the handler.
func (s *testServer) Serve(handler func(net.Conn) error) {
	go func() {
		defer close(s.errCh)
		defer func() {
			if err := s.listener.Close(); err != nil {
				s.t.Logf("testServer listener close: %v", err)
			}
		}()

		conn, err := s.listener.Accept()
		if err != nil {
			s.errCh <- fmt.Errorf("accept: %w", err)
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				s.t.Logf("testServer conn close: %v", err)
			}
		}()

		tlsConn := tls.Server(conn, s.config)

		// Force handshake to ensure the connection is actually established
		// before handing it off.
		if err := tlsConn.Handshake(); err != nil {
			s.errCh <- fmt.Errorf("handshake: %w", err)
			return
		}

		if err := handler(tlsConn); err != nil {
			s.errCh <- err
		}
	}()
}

func (s *testServer) Wait(t *testing.T) {
	t.Helper()
	err := <-s.errCh
	be.NilErr(t, err)
}

func TestTransport_Dial(t *testing.T) {
	srv := newTestServer(t)
	var serverSeen []byte

	// Define Server Logic
	srv.Serve(func(c net.Conn) error {
		// TLS doesn't have subsystem requests; we start immediately.
		if _, err := io.WriteString(c, "muffins]]>]]>"); err != nil {
			return err
		}

		var err error
		serverSeen, err = io.ReadAll(c)
		return err
	})

	config := &tls.Config{InsecureSkipVerify: true}
	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)
	be.NilErr(t, err)

	// Read Greeting
	r, err := tr.MsgReader()
	be.NilErr(t, err)
	greeting, _ := io.ReadAll(r)
	be.Equal(t, "muffins", string(greeting))

	// Write Data
	w, err := tr.MsgWriter()
	be.NilErr(t, err)

	_, err = io.WriteString(w, "a man a plan a canal panama")
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	err = tr.Close() // Send EOF to server
	be.NilErr(t, err)

	srv.Wait(t)

	be.Equal(t, "a man a plan a canal panama]]>]]>", string(serverSeen))
}

func TestTransport_DialContextCancel(t *testing.T) {
	// Raw TCP listener that accepts but never handshakes
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
			_, _ = io.Copy(io.Discard, conn)
		}
	}()

	config := &tls.Config{InsecureSkipVerify: true}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = Dial(ctx, "tcp", ln.Addr().String(), config)

	be.True(t, errors.Is(err, context.DeadlineExceeded))
	be.True(t, time.Since(start) <= 200*time.Millisecond)
}

func TestTransport_MultipleMessages(t *testing.T) {
	srv := newTestServer(t)
	var serverSeen []byte

	srv.Serve(func(c net.Conn) error {
		_, err := io.WriteString(c, "muffins]]>]]>")
		if err != nil {
			return err
		}

		serverSeen, err = io.ReadAll(c)
		return err
	})

	config := &tls.Config{InsecureSkipVerify: true}
	tr, err := Dial(context.Background(), "tcp", srv.Addr(), config)
	be.NilErr(t, err)

	r, _ := tr.MsgReader()

	_, err = io.ReadAll(r) // Consume greeting
	be.NilErr(t, err)

	// Msg 1
	w, _ := tr.MsgWriter()
	_, err = io.WriteString(w, "msg1")
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	// Msg 2
	w, _ = tr.MsgWriter()
	_, err = io.WriteString(w, "msg2")
	be.NilErr(t, err)

	err = w.Close()
	be.NilErr(t, err)

	err = tr.Close()
	be.NilErr(t, err)

	srv.Wait(t)

	be.Equal(t, "msg1]]>]]>msg2]]>]]>", string(serverSeen))
}

// generateSelfSignedCert creates an in-memory generic cert for testing
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  key,
	}, nil
}
