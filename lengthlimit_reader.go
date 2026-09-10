package smtp

import (
	"bytes"
	"errors"
	"io"
)

var ErrTooLongLine = errors.New("smtp: too long a line in input stream")

// lineLimitReader reads from the underlying Reader but restricts
// line length of lines in input stream to a certain length.
//
// If line length exceeds the limit - Read returns ErrTooLongLine
type lineLimitReader struct {
	R         io.Reader
	LineLimit int

	curLineLength int
}

func (r *lineLimitReader) Read(b []byte) (int, error) {
	if r.curLineLength > r.LineLimit && r.LineLimit > 0 {
		return 0, ErrTooLongLine
	}

	n, err := r.R.Read(b)
	if err != nil {
		return n, err
	}

	if r.LineLimit == 0 {
		return n, nil
	}

	for rest := b[:n]; len(rest) > 0; {
		i := bytes.IndexByte(rest, '\n')
		span := len(rest)
		if i >= 0 {
			span = i
		}
		r.curLineLength += span
		if r.curLineLength > r.LineLimit {
			return 0, ErrTooLongLine
		}
		if i < 0 {
			break
		}
		// Preserve the existing accounting: LF starts the next line.
		r.curLineLength = 1
		rest = rest[i+1:]
	}

	return n, nil
}
