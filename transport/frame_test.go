package transport

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	rfcChunkedRPC = []byte(`
#4
<rpc
#18
 message-id="102"

#79
     xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <close-session/>
</rpc>
##
`)

	rfcUnchunkedRPC = []byte(`<rpc message-id="102"
     xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <close-session/>
</rpc>`)
)

var chunkedTests = []struct {
	name        string
	input, want []byte
	err         error
}{
	{"normal",
		[]byte("\n#3\nfoo\n##\n"),
		[]byte("foo"),
		nil},
	{"empty frame",
		[]byte("\n##\n"),
		[]byte(""),
		nil},
	{"multichunk",
		[]byte("\n#3\nfoo\n#3\nbar\n##\n"),
		[]byte("foobar"),
		nil},
	{"missing header",
		[]byte("uhoh"),
		[]byte(""),
		ErrMalformedChunk},
	{"eof in header",
		[]byte("\n#\n"),
		[]byte(""),
		io.ErrUnexpectedEOF},
	{"no headler",
		[]byte("\n00\n"),
		[]byte(""),
		ErrMalformedChunk},
	{"malformed header",
		[]byte("\n#big\n"),
		[]byte(""),
		ErrMalformedChunk},
	{"zero len chunk",
		[]byte("\n#0\n"),
		[]byte(""),
		ErrMalformedChunk},
	{"too big chunk",
		[]byte("\n#4294967296\n"),
		[]byte(""),
		ErrMalformedChunk},
	{"rfc example rpc", rfcChunkedRPC, rfcUnchunkedRPC, nil},
}

func TestChunkReaderReadByte(t *testing.T) {
	for _, tc := range chunkedTests {
		t.Run(tc.name, func(t *testing.T) {
			r := &chunkedReader{
				r: bufio.NewReader(bytes.NewReader(tc.input)),
			}

			buf := make([]byte, 8192)

			var (
				b   byte
				n   int
				err error
			)
			for {
				b, err = r.ReadByte()
				if err != nil {
					break
				}
				buf[n] = b
				n++
			}
			buf = buf[:n]

			if !errors.Is(err, io.EOF) {
				assert.Equal(t, tc.err, err)
			}
			assert.Equal(t, tc.want, buf)

			closeErr := r.Close()
			if tc.err == nil {
				assert.NoError(t, closeErr)
			}
		})
	}
}

func TestChunkReaderRead(t *testing.T) {
	for _, tc := range chunkedTests {
		t.Run(tc.name, func(t *testing.T) {
			r := &chunkedReader{
				r: bufio.NewReader(bytes.NewReader(tc.input)),
			}

			got, err := io.ReadAll(r)
			assert.Equal(t, tc.err, err)
			assert.Equal(t, tc.want, got)

			closeErr := r.Close()
			if tc.err == nil {
				assert.NoError(t, closeErr)
			}
		})
	}
}

func TestChunkWriter(t *testing.T) {
	buf := bytes.Buffer{}
	w := &chunkedWriter{bufio.NewWriter(&buf)}

	n, err := w.Write([]byte("foo"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)

	n, err = w.Write([]byte("quux"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)

	err = w.Close()
	assert.NoError(t, err)

	want := []byte("\n#3\nfoo\n#4\nquux\n##\n")
	assert.Equal(t, want, buf.Bytes())
}

var (
	rfcMarkedRPC = []byte(`
<?xml version="1.0" encoding="UTF-8"?>
<rpc message-id="105"
xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <get-config>
    <source><running/></source>
    <config xmlns="http://example.com/schema/1.2/config">
     <users/>
    </config>
  </get-config>
</rpc>
]]>]]>`)
	rfcUnframedRPC = rfcMarkedRPC[:len(rfcMarkedRPC)-6]
)

var framedTests = []struct {
	name        string
	input, want []byte
	err         error
}{
	{"normal",
		[]byte("foo]]>]]>"),
		[]byte("foo"),
		nil},
	{"empty frame",
		[]byte("]]>]]>"),
		[]byte(""),
		nil},
	{"next message",
		[]byte("foo]]>]]>bar]]>]]>"),
		[]byte("foo"), nil},
	{"no delim",
		[]byte("uhohwhathappened"),
		[]byte("uhohwhathappened"),
		io.ErrUnexpectedEOF},
	{"truncated delim",
		[]byte("foo]]>"),
		[]byte("foo"),
		io.ErrUnexpectedEOF},
	{"partial delim",
		[]byte("foo]]>]]bar]]>]]>"),
		[]byte("foo]]>]]bar"),
		nil},
	{"rfc example rpc", rfcMarkedRPC, rfcUnframedRPC, nil},
}

func TestMarkedReadByte(t *testing.T) {
	for _, tc := range framedTests {
		t.Run(tc.name, func(t *testing.T) {
			r := &markedReader{
				r: bufio.NewReader(bytes.NewReader(tc.input)),
			}

			buf := make([]byte, 8192)
			var (
				b   byte
				n   int
				err error
			)
			for {
				b, err = r.ReadByte()
				if err != nil {
					break
				}
				buf[n] = b
				n++
			}
			buf = buf[:n]

			if !errors.Is(err, io.EOF) {
				assert.Equal(t, err, tc.err)
			}

			assert.Equal(t, tc.want, buf)

			closeErr := r.Close()
			if tc.err == nil {
				assert.NoError(t, closeErr)
			}
		})
	}
}

func TestMarkedRead(t *testing.T) {
	for _, tc := range framedTests {
		t.Run(tc.name, func(t *testing.T) {
			r := &markedReader{
				r: bufio.NewReader(bytes.NewReader(tc.input)),
			}
			got, err := io.ReadAll(r)
			assert.Equal(t, err, tc.err)
			assert.Equal(t, tc.want, got)

			closeErr := r.Close()
			if tc.err == nil {
				assert.NoError(t, closeErr)
			}
		})
	}
}

func TestMarkedWriter(t *testing.T) {
	buf := bytes.Buffer{}
	w := &markedWriter{w: bufio.NewWriter(&buf)}

	n, err := w.Write([]byte("foo"))
	assert.NoError(t, err)
	assert.Equal(t, 3, n)

	err = w.Close()
	assert.NoError(t, err)

	want := []byte("foo]]>]]>")
	assert.Equal(t, want, buf.Bytes())
}

const (
	// Chunk size used for generating synthetic data.
	// 4KB is a reasonable default for many network implementations.
	benchChunkSize = 4096
)

// generateChunkedData creates a valid chunked message of approx totalSize bytes.
func generateChunkedData(totalSize int) []byte {
	buf := bytes.Buffer{}
	// Use a recognizable pattern
	payload := []byte("0123456789abcdef")

	current := 0
	for current < totalSize {
		// Determine size for this specific chunk
		size := benchChunkSize
		if totalSize-current < size {
			size = totalSize - current
		}

		// Write header: \n#<len>\n
		fmt.Fprintf(&buf, "\n#%d\n", size)

		// Write payload repeating the pattern
		for i := 0; i < size; i++ {
			buf.WriteByte(payload[i%len(payload)])
		}
		current += size
	}

	// Write End-of-Chunks
	buf.Write([]byte("\n##\n"))
	return buf.Bytes()
}

// generateMarkedData creates a valid end-of-message delimited message of approx
// totalSize bytes.
func generateMarkedData(totalSize int) []byte {
	buf := bytes.Buffer{}
	payload := []byte("0123456789abcdef")

	for i := 0; i < totalSize; i++ {
		buf.WriteByte(payload[i%len(payload)])
	}
	buf.Write(endOfMsg)
	return buf.Bytes()
}

func BenchmarkChunkedRead(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"Small_200B", 200},
		{"Medium_128KB", 128 * 1024},
		{"Large_10MB", 10 * 1024 * 1024},
	}

	// Pre-allocate a buffer for io.CopyBuffer to avoid measuring allocation overhead
	copyBuf := make([]byte, 32*1024)
	dst := io.Discard

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			data := generateChunkedData(bm.size)
			src := bytes.NewReader(data)

			br := bufio.NewReader(src)
			r := &chunkedReader{r: br}

			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				src.Reset(data)
				br.Reset(src)

				_, err := io.CopyBuffer(dst, r, copyBuf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkChunkedReadByte(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"Small_200B", 200},
		{"Medium_128KB", 128 * 1024},
		// ReadByte on 10MB is extremely slow and mostly tests CPU patience,
		// but included for completeness if desired.
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			data := generateChunkedData(bm.size)
			src := bytes.NewReader(data)

			br := bufio.NewReader(src)
			r := &chunkedReader{r: br}

			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				src.Reset(data)
				br.Reset(src)

				for {
					_, err := r.ReadByte()
					if err != nil {
						if err == io.EOF {
							break
						}
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkMarkedRead(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"Small_200B", 200},
		{"Medium_128KB", 128 * 1024},
		{"Large_10MB", 10 * 1024 * 1024},
	}

	copyBuf := make([]byte, 32*1024)
	dst := io.Discard

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			data := generateMarkedData(bm.size)
			src := bytes.NewReader(data)

			br := bufio.NewReader(src)
			r := &markedReader{r: br}

			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				src.Reset(data)
				br.Reset(src)

				_, err := io.CopyBuffer(dst, r, copyBuf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarkedReadByte(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"Small_200B", 200},
		{"Medium_128KB", 128 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			data := generateMarkedData(bm.size)
			src := bytes.NewReader(data)

			br := bufio.NewReader(src)
			r := &markedReader{r: br}

			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				src.Reset(data)
				br.Reset(src)

				for {
					_, err := r.ReadByte()
					if err != nil {
						if err == io.EOF {
							break
						}
						b.Fatal(err)
					}
				}
			}
		})
	}
}
