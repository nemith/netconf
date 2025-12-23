package transport

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
)

// Framer is a wrapper used for transports that implement the framing defined in
// RFC6242.  This supports End-of-Message and Chucked framing methods and
// will move from End-of-Message to Chunked framing after the `Upgrade` method
// has been called.
//
// This is not a transport on it's own (missing the `Close` method) and is
// intended to be embedded into other transports.
type Framer struct {
	r            *bufio.Reader
	w            *bufio.Writer
	chunkFraming bool
}

// NewFramer return a new Framer to be used against the given io.Reader and io.Writer.
func NewFramer(r io.Reader, w io.Writer) *Framer {
	return &Framer{
		r: bufio.NewReader(r),
		w: bufio.NewWriter(w),
	}
}

// Upgrade will cause the Framer to switch from End-of-Message framing to
// Chunked framing.  This is usually called after netconf exchanged the hello
// messages.
func (f *Framer) Upgrade() {
	f.chunkFraming = true
}

func (f *Framer) MsgReader() (io.ReadCloser, error) {
	if f.chunkFraming {
		return &chunkedReader{r: f.r}, nil
	}
	return &markedReader{r: f.r}, nil
}

func (f *Framer) MsgWriter() (io.WriteCloser, error) {
	if f.chunkFraming {
		return &chunkedWriter{w: f.w}, nil
	}
	return &markedWriter{w: f.w}, nil
}

var endOfChunks = []byte("\n##\n")

// Defined in https://www.rfc-editor.org/rfc/rfc6242#section-4.2
const maxChunk = uint32(math.MaxUint32)

// ErrMalformedChunk represents a message that invalid as defined in the chunk
// framing in RFC6242
var ErrMalformedChunk = errors.New("netconf: invalid chunk")

type chunkedReader struct {
	r         *bufio.Reader
	chunkLeft uint32
}

func (r *chunkedReader) readHeader() (uint32, error) {
	// Peek at preamble to check for "\n#"
	preamble, err := r.r.Peek(2)
	if err != nil {
		return 0, err
	}

	if preamble[0] != '\n' || preamble[1] != '#' {
		return 0, ErrMalformedChunk
	}

	// Check if this is end-of-chunks marker "\n##\n"
	marker, err := r.r.Peek(4)
	if err != nil {
		return 0, err
	}
	if marker[2] == '#' && marker[3] == '\n' {
		// Discard the end-of-chunks marker
		r.r.Discard(4)
		return 0, nil // Signal end of message with 0 chunk size
	}

	// Discard the "\n#" preamble
	r.r.Discard(2)

	// Parse chunk size by reading digits until '\n'
	var chunkSize uint32
	for {
		c, err := r.r.ReadByte()
		if err != nil {
			return 0, err
		}

		if c == '\n' {
			break
		}

		if c < '0' || c > '9' {
			return 0, ErrMalformedChunk
		}

		n := chunkSize*10 + uint32(c-'0')
		if n < chunkSize || n > uint32(maxChunk) {
			return 0, ErrMalformedChunk
		}
		chunkSize = n
	}

	if chunkSize == 0 {
		return 0, ErrMalformedChunk
	}

	return chunkSize, nil
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.r == nil {
		return 0, ErrInvalidIO
	}

	if uint32(len(p)) > maxChunk {
		p = p[:maxChunk]
	}

	// If we have no bytes left in the current chunk, we must read a new header
	if r.chunkLeft <= 0 {
		chunkSize, err := r.readHeader() // (same helper as before)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}

		// Zero chunk size indicates "End of Chunks" (\n##\n)
		if chunkSize == 0 {
			return 0, io.EOF
		}
		r.chunkLeft = chunkSize
	}

	toRead := min(uint32(len(p)), r.chunkLeft)
	n, err := r.r.Read(p[:toRead])
	r.chunkLeft -= uint32(n)

	return n, err
}

func (r *chunkedReader) ReadByte() (byte, error) {
	if r.r == nil {
		return 0, ErrInvalidIO
	}

	// done with existing chunck so grab the next one
	if r.chunkLeft <= 0 {
		n, err := r.readHeader()
		if n == 0 && err == nil {
			return 0, io.EOF
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		r.chunkLeft = n
	}

	b, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	r.chunkLeft--
	return b, nil
}

func (r *chunkedReader) Close() error {
	// poison the reader so that it can no longer be used
	defer func() { r.r = nil }()

	// read all remaining chunks until we get to the end of the frame.
	for {
		if r.chunkLeft <= 0 {
			// readHeader return io.EOF when it encounter the end-of-frame
			// marker ("\n##\n")
			n, err := r.readHeader()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			r.chunkLeft = n
		}

		for r.chunkLeft > 0 {
			// Protect agaisnt int overflow on 32-bit systems
			toDiscard := int(r.chunkLeft)
			if uint(r.chunkLeft) > uint(math.MaxInt) {
				toDiscard = math.MaxInt
			}

			n, err := r.r.Discard(toDiscard)
			r.chunkLeft -= uint32(n)
			if err != nil {
				return err
			}
		}
	}
}

type chunkedWriter struct {
	w *bufio.Writer
}

func (w *chunkedWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return 0, ErrInvalidIO
	}

	totalWritten := 0
	for len(p) > 0 {
		// Cap chunk size at MaxInt32 (~2GB) to avoid overflow issues on all architectures
		chunkSize := len(p)
		if chunkSize > math.MaxInt32 {
			chunkSize = math.MaxInt32
		}

		// Write header
		if _, err := fmt.Fprintf(w.w, "\n#%d\n", chunkSize); err != nil {
			return totalWritten, err
		}

		// Write payload
		n, err := w.w.Write(p[:chunkSize])
		totalWritten += n
		if err != nil {
			return totalWritten, err
		}

		// Advance
		p = p[n:]
	}

	return totalWritten, nil
}

func (w *chunkedWriter) Close() error {
	defer func() { w.w = nil }()

	// write the end-of-chunks marker
	if _, err := w.w.Write(endOfChunks); err != nil {
		return err
	}
	return w.w.Flush()
}

var endOfMsg = []byte("]]>]]>")

type markedReader struct {
	r         *bufio.Reader
	fullyRead bool
}

func (r *markedReader) Read(p []byte) (int, error) {
	for i := 0; i < len(p); i++ {
		b, err := r.ReadByte()
		if err != nil {
			// Return bytes read so far if we hit EOF/Delimiter
			return i, err
		}
		p[i] = b
	}
	return len(p), nil
}

func (r *markedReader) ReadByte() (byte, error) {
	if r.r == nil {
		return 0, ErrInvalidIO
	}

	b, err := r.r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return b, io.ErrUnexpectedEOF
		}
		return b, err
	}

	// look for the end of the message marker
	if b == endOfMsg[0] {
		peeked, err := r.r.Peek(len(endOfMsg) - 1)
		if err != nil {
			if err == io.EOF {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}

		// check if we are at the end of the message
		if bytes.Equal(peeked, endOfMsg[1:]) {
			if _, err := r.r.Discard(len(endOfMsg) - 1); err != nil {
				return 0, err
			}

			r.fullyRead = true
			return 0, io.EOF
		}
	}

	return b, nil
}

func (r *markedReader) Close() error {
	// poison the reader so that it can no longer be used
	defer func() { r.r = nil }()

	// If we have alread read read to the end, nothing to do
	if r.fullyRead {
		return nil
	}

	var err error
	for err == nil {
		_, err = r.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
	return err
}

type markedWriter struct {
	w *bufio.Writer
}

func (w *markedWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return 0, ErrInvalidIO
	}

	return w.w.Write(p)
}

func (w *markedWriter) Close() error {
	defer func() { w.w = nil }()

	if _, err := w.w.Write(endOfMsg); err != nil {
		return err
	}

	return w.w.Flush()
}
