package transport

import (
	"errors"
	"io"
)

var (
	// ErrInvalidIO is returned when a write or read operation is called on
	// message io.Reader or a message io.Writer when they are no longer valid.
	// (i.e a new reader or writer has been obtained)
	ErrInvalidIO = errors.New("netconf: read/write on invalid io")
)

// Transport is used for a netconf.Session to talk to the device. It is message
// oriented to allow for framing and other details to happen on a per message
// basis.
type Transport interface {
	// MsgReader returns a reader for the next message.
	// The caller must close the reader when done.
	MsgReader() (io.ReadCloser, error)

	// MsgWriter returns a writer for a new message. Closing it will finalize
	// the message framing and flush to the underlying transport.
	MsgWriter() (io.WriteCloser, error)

	Close() error
}
