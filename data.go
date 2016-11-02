package smtp

import (
	"io"
)

type smtpError struct {
	Code    int
	Message string
}

func (err *smtpError) Error() string {
	return err.Message
}

var ErrDataTooLarge = &smtpError{
	Code:    552,
	Message: "Maximum message size exceeded",
}

type dataReader struct {
	r io.Reader

	limited bool
	n       int64 // Maximum bytes remaining
}

func newDataReader(c *Conn) io.Reader {
	dr := &dataReader{
		r: c.text.DotReader(),
	}

	if c.server.MaxMessageBytes > 0 {
		dr.limited = true
		dr.n = int64(c.server.MaxMessageBytes)
	}

	return dr
}

func (r *dataReader) Read(b []byte) (n int, err error) {
	if r.limited {
		if r.n <= 0 {
			return 0, ErrDataTooLarge
		}
		if int64(len(b)) > r.n {
			b = b[0:r.n]
		}
	}

	n, err = r.r.Read(b)

	if r.limited {
		r.n -= int64(n)
	}
	return
}
