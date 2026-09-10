package smtp

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// Compare against the original byte-wise accounting across all chunk boundaries.
func TestLineLimitReaderSpans(t *testing.T) {
	for _, input := range []string{"", "a", "\n", "\r\n", "abc\r\ndef\r\n", "\n\n\n", strings.Repeat("x", 4097) + "\nnext"} {
		for _, limit := range []int{0, 1, 2, 4, 4096, 4097} {
			for _, chunk := range []int{1, 2, 3, 17, 4096, 8192} {
				r := &lineLimitReader{R: strings.NewReader(input), LineLimit: limit}
				buf := make([]byte, chunk)
				offset, length := 0, 0
				for {
					n, err := r.Read(buf)
					if err == io.EOF {
						break
					}
					end := offset + chunk
					if end > len(input) {
						end = len(input)
					}
					wantErr := error(nil)
					if limit > 0 {
						for _, ch := range []byte(input[offset:end]) {
							if ch == '\n' {
								length = 0
							}
							length++
							if length > limit {
								wantErr = ErrTooLongLine
								break
							}
						}
					}
					if err != wantErr || (err == nil && (n != end-offset || !bytes.Equal(buf[:n], []byte(input[offset:end])))) {
						t.Fatalf("input %q limit %d chunk %d: n=%d err=%v want=%v", input, limit, chunk, n, err, wantErr)
					}
					if err != nil {
						if n != 0 {
							t.Fatal("returned rejected bytes")
						}
						if n, err := r.Read(buf); n != 0 || err != ErrTooLongLine {
							t.Fatal("lost sticky line error", n, err)
						}
						break
					}
					offset = end
				}
			}
		}
	}
}
