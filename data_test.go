package smtp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

type fragmentReader struct {
	io.Reader
	size int
}

func (r fragmentReader) Read(p []byte) (int, error) {
	if len(p) > r.size {
		p = p[:r.size]
	}
	return r.Reader.Read(p)
}

func TestDataReaderFragmented(t *testing.T) {
	tests := []struct {
		name, body, want string
		err              error
	}{
		{"empty", ".\r\n", "", nil},
		{"lines", "hello\r\nworld\r\n.\r\n", "hello\r\nworld\r\n", nil},
		{"dots", "..one\r\n...two\r\n.\r\n", ".one\r\n..two\r\n", nil},
		{"bare-lf", "first\n.second\r\n.\r\n", "first\n.second\r\n", nil},
		{"bare-cr", "first\rsecond\r\n.\r\n", "first\rsecond\r\n", nil},
		{"dot-cr", ".\rx\r\n.\r\n", "x\r\n", nil},
		{"unterminated", "hello\r\n", "hello\r\n", io.ErrUnexpectedEOF},
		{"split-dot", "hello\r\n.", "hello\r\n", io.ErrUnexpectedEOF},
		{"long-run", strings.Repeat("a", 8193) + "\r\n.\r\n", strings.Repeat("a", 8193) + "\r\n", nil},
	}
	for _, tc := range tests {
		for _, input := range []int{1, 2, 3, 17, 4096} {
			for _, output := range []int{1, 2, 3, 64, 4096} {
				t.Run(fmt.Sprintf("%s/in%d/out%d", tc.name, input, output), func(t *testing.T) {
					wire := tc.body
					if tc.err == nil {
						wire += "NEXT\r\n"
					}
					buffered := bufio.NewReader(fragmentReader{strings.NewReader(wire), input})
					reader := &dataReader{r: buffered}
					var got bytes.Buffer
					buffer := make([]byte, output)
					_, err := io.CopyBuffer(&writeOnlyBuffer{&got}, reader, buffer)
					if err != tc.err || got.String() != tc.want {
						t.Fatalf("got %q, %v; want %q, %v", got.String(), err, tc.want, tc.err)
					}
					if tc.err == nil {
						remaining, err := ioutil.ReadAll(buffered)
						if err != nil || string(remaining) != "NEXT\r\n" {
							t.Fatalf("read beyond DATA: %q, %v", remaining, err)
						}
					}
				})
			}
		}
	}
}

type writeOnlyBuffer struct{ b *bytes.Buffer }

func (w *writeOnlyBuffer) Write(p []byte) (int, error) { return w.b.Write(p) }

func TestDataReaderBulkLimit(t *testing.T) {
	for _, limit := range []int64{1, 63, 4095, 4096, 4097} {
		reader := &dataReader{r: bufio.NewReader(strings.NewReader(strings.Repeat("a", 8192) + "\r\n.\r\n")), limited: true, n: limit}
		data, err := ioutil.ReadAll(reader)
		if err != ErrDataTooLarge || int64(len(data)) != limit {
			t.Fatalf("limit %d: read %d, %v", limit, len(data), err)
		}
	}
}

type repeatedDataReader struct {
	block     []byte
	offset    int
	remaining int64
}

func (r *repeatedDataReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n := 0
	for n < len(p) {
		k := copy(p[n:], r.block[r.offset:])
		n += k
		r.offset = (r.offset + k) % len(r.block)
	}
	r.remaining -= int64(n)
	return n, nil
}

type discardDataWriter struct{}

func (discardDataWriter) Write(p []byte) (int, error) { return len(p), nil }

func BenchmarkDataReader(b *testing.B) {
	for _, tc := range []struct{ name, block string }{
		{"MIME", strings.Repeat(strings.Repeat("x", 76)+"\r\n", 13443)},
		{"LongLine", strings.Repeat("x", 1<<20)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			block := []byte(tc.block)
			buffer := make([]byte, 128<<10)
			b.SetBytes(2 << 30)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				wire := io.MultiReader(&repeatedDataReader{block: block, remaining: 2 << 30}, strings.NewReader("\r\n.\r\n"))
				reader := &dataReader{r: bufio.NewReader(wire)}
				n, err := io.CopyBuffer(discardDataWriter{}, reader, buffer)
				if err != nil || n != (2<<30)+2 {
					b.Fatalf("read %d, %v", n, err)
				}
			}
		})
	}
}
