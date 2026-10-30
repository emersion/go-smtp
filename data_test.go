package smtp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strings"
	"testing"
	"time"
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

func BenchmarkDataReaderWithLineLimit(b *testing.B) {
	block := []byte(strings.Repeat(strings.Repeat("x", 76)+"\r\n", 13443))
	buffer := make([]byte, 128<<10)
	b.SetBytes(2 << 30)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wire := io.MultiReader(&repeatedDataReader{block: block, remaining: 2 << 30}, strings.NewReader("\r\n.\r\n"))
		reader := &dataReader{r: bufio.NewReader(&lineLimitReader{R: wire, LineLimit: 2000})}
		n, err := io.CopyBuffer(discardDataWriter{}, reader, buffer)
		if err != nil || n != (2<<30)+2 {
			b.Fatalf("read %d, %v", n, err)
		}
	}
}

func TestConnDataPipeline(t *testing.T) {
	server, client := net.Pipe()
	s := NewServer(nil)
	c := newConn(server, s)
	wantSize := 4096
	if c.text.R.Size() != wantSize {
		t.Fatalf("buffer size %d", c.text.R.Size())
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.WriteString(client, "DATA\r\nhello\r\n..dot\r\n.\r\nNOOP\r\n")
		done <- err
	}()
	line, err := c.text.ReadLine()
	if err != nil || line != "DATA" {
		t.Fatal(line, err)
	}
	body, err := ioutil.ReadAll(newDataReader(c))
	if err != nil || string(body) != "hello\r\n.dot\r\n" {
		t.Fatal(string(body), err)
	}
	line, err = c.text.ReadLine()
	if err != nil || line != "NOOP" {
		t.Fatal(line, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.Close()
}

// A live SMTP peer waits for the reply without closing the write side.
func TestDataReaderEmptyLiveConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	server.SetReadDeadline(time.Now().Add(2 * time.Second))
	done := make(chan error, 1)
	go func() { _, err := io.WriteString(client, ".\r\n"); done <- err }()
	r := &dataReader{r: bufio.NewReader(server)}
	got, err := ioutil.ReadAll(r)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty DATA: %q, %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDataReaderZeroRead(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("..first\r\n.\r\nNEXT\r\n"))
	r := &dataReader{r: br}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("zero read: %d, %v", n, err)
	}
	got, err := ioutil.ReadAll(r)
	if err != nil || string(got) != ".first\r\n" {
		t.Fatalf("body: %q, %v", got, err)
	}
	got, err = ioutil.ReadAll(br)
	if err != nil || string(got) != "NEXT\r\n" {
		t.Fatalf("next command: %q, %v", got, err)
	}
}
