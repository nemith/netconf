package transport

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

var ErrStreamBusy = errors.New("transport: stream is already active")

// Framer is a wrapper used for transports that implement the framing defined in
// RFC6242.  This supports End-of-Message and Chunked framing methods and
// will move from End-of-Message to Chunked framing after the `Upgrade` method
// has been called.
//
// This is not a transport on it's own (missing the `Close` method) and is
// intended to be embedded into other transports.
type Framer struct {
	r io.Reader
	w io.Writer

	br *bufio.Reader
	bw *bufio.Writer

	mu           sync.Mutex
	chunkFraming bool
	activeReader bool
	activeWriter bool
}

// NewFramer return a new Framer to be used against the given io.Reader and io.Writer.
func NewFramer(r io.Reader, w io.Writer) *Framer {
	return &Framer{
		r:  r,
		w:  w,
		br: bufio.NewReader(r),
		bw: bufio.NewWriter(w),
	}
}

// DebugCapture will copy all *framed* input/output to the the given
// `io.Writers` for sent or recv data.  Either sent of recv can be nil to not
// capture any data.  Useful for displaying to a screen or capturing to a file
// for debugging.
//
// This needs to be called before `MsgReader` or `MsgWriter`.
func (f *Framer) DebugCapture(input, output io.Writer) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.activeReader ||
		f.activeWriter ||
		f.bw.Buffered() > 0 ||
		f.br.Buffered() > 0 {
		panic("debug capture added with active reader or writer")
	}

	if input != nil {
		f.br = bufio.NewReader(io.TeeReader(f.r, input))
	}

	if output != nil {
		f.bw = bufio.NewWriter(io.MultiWriter(f.w, output))
	}
}

// Upgrade will cause the Framer to switch from End-of-Message framing to
// Chunked framing.  This is usually called after netconf exchanged the hello
// messages.
func (f *Framer) Upgrade() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunkFraming = true
}

func (f *Framer) closeReader() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeReader = false
}

func (f *Framer) closeWriter() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeWriter = false
}

func (f *Framer) MsgReader() (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.activeReader {
		return nil, ErrStreamBusy
	}
	f.activeReader = true

	if f.chunkFraming {
		return &chunkedReader{
			r: f.br,
			f: f,
		}, nil
	}
	return &markedReader{
		r: f.br,
		f: f,
	}, nil
}

func (f *Framer) MsgWriter() (io.WriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.activeWriter {
		return nil, ErrStreamBusy
	}
	f.activeWriter = true

	if f.chunkFraming {
		return &chunkedWriter{
			w: f.bw,
			f: f,
		}, nil
	}
	return &markedWriter{
		w: f.bw,
		f: f,
	}, nil
}

var endOfChunks = []byte("\n##\n")

// ErrMalformedChunk represents a message that invalid as defined in the chunk
// framing in RFC6242
var ErrMalformedChunk = errors.New("netconf: invalid chunk")

type chunkedReader struct {
	f         *Framer
	r         *bufio.Reader
	chunkLeft uint32
	eof       bool
}

func (r *chunkedReader) readHeader() (uint32, error) {
	// Peek at marker to check for "\n#"
	marker, err := r.r.Peek(4)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}

	if marker[0] != '\n' || marker[1] != '#' {
		return 0, ErrMalformedChunk
	}

	if marker[2] == '#' && marker[3] == '\n' {
		// Discard the end-of-chunks marker
		if _, err := r.r.Discard(4); err != nil {
			return 0, err
		}

		return 0, nil // Signal end of message with 0 chunk size
	}

	// Discard the "\n#" preamble
	if _, err := r.r.Discard(2); err != nil {
		return 0, err
	}

	line, err := r.r.ReadSlice('\n')
	if err != nil {
		if err == io.EOF {
			return 0, io.ErrUnexpectedEOF
		}
		// ReadSlice returns err if '\n' is missing (EOF or buffer full)
		return 0, err
	}

	// Cut off the '\n' from the end for parsing
	digits := line[:len(line)-1]

	// If the line was just "\n", digits is empty (chunks must have size)
	if len(digits) == 0 {
		return 0, ErrMalformedChunk
	}

	var chunkSize uint32
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, ErrMalformedChunk
		}

		if chunkSize > math.MaxUint32/10 {
			return 0, ErrMalformedChunk
		}
		chunkSize = chunkSize * 10

		digit := uint32(c - '0')
		if chunkSize > math.MaxUint32-digit {
			return 0, ErrMalformedChunk
		}
		chunkSize += digit
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

	if len(p) > math.MaxUint32 {
		p = p[:math.MaxUint32]
	}

	// If we have no bytes left in the current chunk, we must read a new header
	if r.chunkLeft <= 0 {
		chunkSize, err := r.readHeader()
		if err != nil {
			return 0, err
		}

		// Zero chunk size indicates "End of Chunks" (\n##\n)
		if chunkSize == 0 {
			r.eof = true
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

	// done with existing chunk so grab the next one
	if r.chunkLeft <= 0 {
		n, err := r.readHeader()
		if err != nil {
			return 0, err
		}
		if n == 0 {
			r.eof = true
			return 0, io.EOF
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
	if r.r == nil {
		return nil
	}
	defer func() {
		r.r = nil
		r.f.closeReader()
	}()

	// If we have already read to the end, nothing to do
	if r.eof {
		return nil
	}

	// Drain the rest of the chunks until we hit the end-of-chunks marker
	for {
		if r.chunkLeft <= 0 {
			// readHeader return 0, nil when it hits the end-of-chunks
			n, err := r.readHeader()
			if err != nil {
				return err
			}

			if n == 0 {
				return nil
			}

			r.chunkLeft = n
		}

		for r.chunkLeft > 0 {
			// Protect against int overflow on 32-bit systems
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
	f *Framer
	w *bufio.Writer
}

func (w *chunkedWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return 0, ErrInvalidIO
	}

	totalWritten := 0
	for len(p) > 0 {
		// Cap chunk size at MaxInt32 (~2GB) to avoid overflow issues on all
		// architectures.
		//
		// XXX: Should we default to smaller chunk sizes.  Default
		// buffer in a bufio writer is 4k and seems resonable?  Check what other
		// chunked implementations do?
		chunkSize := len(p)
		if chunkSize > math.MaxInt32 {
			chunkSize = math.MaxInt32
		}

		// Write chunk header
		if _, err := fmt.Fprintf(w.w, "\n#%d\n", chunkSize); err != nil {
			return totalWritten, err
		}

		// Note: we are not checking for a partial writes as bufio.Writer
		// will only return a short write if the underlying writer returns an
		// error.
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
	if w.w == nil {
		return nil
	}
	defer func() {
		w.w = nil
		w.f.closeWriter()
	}()

	// write the end-of-chunks marker
	if _, err := w.w.Write(endOfChunks); err != nil {
		return err
	}
	return w.w.Flush()
}

var endOfMsg = []byte("]]>]]>")

type markedReader struct {
	f   *Framer
	r   *bufio.Reader
	eof bool
}

func (r *markedReader) Read(p []byte) (int, error) {
	for i := 0; i < len(p); i++ {
		b, err := r.ReadByte()
		if err != nil {
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

	if r.eof {
		return 0, io.EOF
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

			r.eof = true
			return 0, io.EOF
		}
	}

	return b, nil
}

func (r *markedReader) Close() error {
	if r.r == nil {
		return nil
	}
	defer func() {
		r.r = nil
		r.f.closeReader()
	}()

	// If we have already read to the end, nothing to do
	if r.eof {
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
	f *Framer
	w *bufio.Writer
}

func (w *markedWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return 0, ErrInvalidIO
	}

	return w.w.Write(p)
}

func (w *markedWriter) Close() error {
	if w.w == nil {
		return nil
	}
	defer func() {
		w.w = nil
		w.f.closeWriter()
	}()

	if _, err := w.w.Write(endOfMsg); err != nil {
		return err
	}

	return w.w.Flush()
}
