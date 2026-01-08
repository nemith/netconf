package testutil

import (
	"io"
	"testing"

	"github.com/carlmjohnson/be"
)

func TestTransport_SingleActiveWriter(t *testing.T) {
	tr := NewTransport(func(req string) []string {
		return []string{"response"}
	})

	// Get first writer - should succeed
	w1, err := tr.MsgWriter()
	be.NilErr(t, err)
	be.Nonzero(t, w1)

	// Try to get second writer while first is active - should fail
	w2, err := tr.MsgWriter()
	be.Nonzero(t, err)
	be.Zero(t, w2)

	// Close first writer
	be.NilErr(t, w1.Close())

	// Now should be able to get another writer
	w3, err := tr.MsgWriter()
	be.NilErr(t, err)
	be.Nonzero(t, w3)

	be.NilErr(t, w3.Close())
}

func TestTransport_SingleActiveReader(t *testing.T) {
	tr := NewTransport(nil)

	// Queue two messages
	tr.Push("message1", "message2")

	// Get first reader - should succeed
	r1, err := tr.MsgReader()
	be.NilErr(t, err)
	be.Nonzero(t, r1)

	// Try to get second reader while first is active - should fail
	r2, err := tr.MsgReader()
	be.Nonzero(t, err)
	be.Zero(t, r2)

	// Close first reader
	be.NilErr(t, r1.Close())

	// Now should be able to get another reader
	r3, err := tr.MsgReader()
	be.NilErr(t, err)
	be.Nonzero(t, r3)

	be.NilErr(t, r3.Close())
}

func TestTransport_WriteAndRead(t *testing.T) {
	tr := NewTransport(func(req string) []string {
		return []string{"reply to: " + req}
	})

	// Write a message
	w, err := tr.MsgWriter()
	be.NilErr(t, err)

	_, err = io.WriteString(w, "hello")
	be.NilErr(t, err)

	be.NilErr(t, w.Close())

	// Read the response
	r, err := tr.MsgReader()
	be.NilErr(t, err)

	data, err := io.ReadAll(r)
	be.NilErr(t, err)
	be.Equal(t, "reply to: hello", string(data))

	be.NilErr(t, r.Close())

	// Verify the output was recorded
	outputs := tr.Outputs()
	be.Equal(t, 1, len(outputs))
	be.Equal(t, "hello", outputs[0])
}

func TestTransport_ConcurrentWritesFail(t *testing.T) {
	tr := NewTransport(nil)

	w1, err := tr.MsgWriter()
	be.NilErr(t, err)

	// Try to get second writer concurrently
	done := make(chan error, 1)
	go func() {
		_, err := tr.MsgWriter()
		done <- err
	}()

	// Should get an error
	err = <-done
	be.Nonzero(t, err)

	be.NilErr(t, w1.Close())
}

func TestTransport_ConcurrentReadsFail(t *testing.T) {
	tr := NewTransport(nil)
	tr.Push("msg1", "msg2")

	r1, err := tr.MsgReader()
	be.NilErr(t, err)

	// Try to get second reader concurrently
	done := make(chan error, 1)
	go func() {
		_, err := tr.MsgReader()
		done <- err
	}()

	// Should get an error
	err = <-done
	be.Nonzero(t, err)

	be.NilErr(t, r1.Close())
}
