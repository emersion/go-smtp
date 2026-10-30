package smtp

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"math/rand"
	"strings"
	"testing"
)

// Frozen pre-optimization implementation, used as a differential oracle.
type referenceDataReader struct{ dataReader }

func (r *referenceDataReader) Read(b []byte) (n int, err error) {
	if r.limited {
		if r.n <= 0 {
			return 0, ErrDataTooLarge
		}
		if int64(len(b)) > r.n {
			b = b[0:r.n]
		}
	}

	// Code below is taken from net/textproto with only one modification to
	// not rewrite CRLF -> LF.

	// Run data through a simple state machine to
	// elide leading dots and detect End-of-Data (<CR><LF>.<CR><LF>) line.
	const (
		stateBeginLine = iota // beginning of line; initial state; must be zero
		stateDot              // read . at beginning of line
		stateDotCR            // read .\r at beginning of line
		stateCR               // read \r (possibly at end of line)
		stateData             // reading data in middle of line
		stateEOF              // reached .\r\n end marker line
	)
	for n < len(b) && r.state != stateEOF {
		var c byte
		c, err = r.r.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			break
		}
		switch r.state {
		case stateBeginLine:
			if c == '.' {
				r.state = stateDot
				continue
			}
			if c == '\r' {
				r.state = stateCR
				break
			}
			r.state = stateData
		case stateDot:
			if c == '\r' {
				r.state = stateDotCR
				continue
			}
			r.state = stateData
		case stateDotCR:
			if c == '\n' {
				r.state = stateEOF
				continue
			}
			r.state = stateData
		case stateCR:
			if c == '\n' {
				r.state = stateBeginLine
				break
			}
			r.state = stateData
		case stateData:
			if c == '\r' {
				r.state = stateCR
			}
		}
		b[n] = c
		n++
	}
	if err == nil && r.state == stateEOF {
		err = io.EOF
	}

	if r.limited {
		r.n -= int64(n)
	}
	return
}

func FuzzDataReader(f *testing.F) {
	for _, seed := range []string{"", ".\r\n", "..first\r\n.\r\n", "abc\r\r\n.\r\n", "x\r\n..dot\r\n.\r\n", "x\r\r\r\n.\r\n", strings.Repeat("x", 8193)} {
		f.Add([]byte(seed), uint16(17), uint16(64), uint16(0))
	}
	f.Fuzz(checkDataReaderEquivalent)
}

func checkDataReaderEquivalent(t *testing.T, body []byte, input, output, limit uint16) {
	if len(body) > 32768 {
		t.Skip()
	}
	wire := string(body) + "\r\n.\r\nNEXT\r\n"
	makeReader := func() dataReader {
		return dataReader{r: bufio.NewReader(fragmentReader{strings.NewReader(wire), 1 + int(input)%8192}), limited: limit > 0, n: int64(limit)}
	}
	actual, old := makeReader(), referenceDataReader{makeReader()}
	var got, want bytes.Buffer
	buffer := make([]byte, 1+int(output)%8192)
	_, gotErr := io.CopyBuffer(&writeOnlyBuffer{&got}, &actual, buffer)
	_, wantErr := io.CopyBuffer(&writeOnlyBuffer{&want}, &old, buffer)
	if gotErr != wantErr || !bytes.Equal(got.Bytes(), want.Bytes()) || actual.n != old.n {
		t.Fatalf("input %q: got %q, %v, remaining %d; want %q, %v, remaining %d", body, got.Bytes(), gotErr, actual.n, want.Bytes(), wantErr, old.n)
	}
	a, ae := ioutil.ReadAll(actual.r)
	w, we := ioutil.ReadAll(old.r)
	if ae != we || !bytes.Equal(a, w) {
		t.Fatalf("remaining input: got %q, %v; want %q, %v", a, ae, w, we)
	}
}

func TestDataReaderCompatibility(t *testing.T) {
	rng := rand.New(rand.NewSource(312))
	alphabet := []byte("x.\r\n")
	for i := 0; i < 5000; i++ {
		body := make([]byte, rng.Intn(256))
		for j := range body {
			body[j] = alphabet[rng.Intn(len(alphabet))]
		}
		limit := uint16(0)
		if i%2 != 0 {
			limit = uint16(1 + rng.Intn(256))
		}
		checkDataReaderEquivalent(t, body, uint16(rng.Intn(32)), uint16(rng.Intn(32)), limit)
	}
}
