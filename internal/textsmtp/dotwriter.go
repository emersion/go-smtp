// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Based on the modifications from
// https://github.com/go-textproto/textproto/blob/v0/writer.go

package textsmtp

import (
	"bufio"
	"bytes"
	"io"

	"github.com/uponusolutions/go-smtp"
)

// NewDotWriter returns a writer that can be used to write a dot-encoding to w.
// It takes care of inserting leading dots when necessary,
// translating line-ending \n into \r\n, and adding the final .\r\n line
// when the DotWriter is closed. The caller should close the
// DotWriter before the next call to a method on w.
//
// See the documentation for Reader's DotReader method for details about dot-encoding.
func NewDotWriter(writer *bufio.Writer) io.WriteCloser {
	return &dotWriter{
		W: writer,
	}
}

type dotWriter struct {
	W     *bufio.Writer
	state int
}

const (
	wstateBegin     = iota // starting state
	wstateBeginLine        // beginning of line
	wstateCR               // wrote \r (possibly at end of line)
	wstateData             // writing data in middle of line
)

func (d *dotWriter) Write(b []byte) (n int, err error) {
	var (
		i    int
		p    []byte
		pLen int
		bw   = d.W
	)
	for len(b) > 0 {
		i = bytes.IndexByte(b, '\n')
		if i >= 0 {
			p, b = b[:i+1], b[i+1:]
		} else {
			p, b = b, nil
		}
		pLen = len(p)
		// A leading dot must be stuffed at the beginning of every line,
		// including the very first one (wstateBegin).
		if (d.state == wstateBeginLine || d.state == wstateBegin) && p[0] == '.' {
			err = bw.WriteByte('.')
			if err != nil {
				return n, err
			}
		}

		if b == nil {
			// no end of line found in p
			if p[pLen-1] == '\r' {
				// p ends with \r
				d.state = wstateCR
			} else {
				// just write it down
				d.state = wstateData
			}

			if _, err = bw.Write(p); err != nil {
				return n, err
			}
		} else if d.state == wstateCR && pLen == 1 {
			// if b isn't nil and pLen is 1, then it must be a \n
			// as \r was send before, just write crnl
			d.state = wstateBeginLine
			if err = bw.WriteByte('\n'); err != nil {
				return n, err
			}
		} else {
			// line is ending
			d.state = wstateBeginLine
			if pLen >= 2 && p[pLen-2] == '\r' {
				// fastpath if line ending is correct \r\n
				if _, err = bw.Write(p); err != nil {
					return n, err
				}
			} else {
				// data + crnl
				if _, err = bw.Write(p[:pLen-1]); err != nil {
					return n, err
				}
				if _, err = bw.Write(smtp.Crnl); err != nil {
					return n, err
				}
			}
		}

		n += pLen
	}
	return n, err
}

func (d *dotWriter) Close() error {
	bw := d.W
	// nolint revive
	switch d.state {
	default:
		if err := bw.WriteByte('\r'); err != nil {
			return err
		}
		fallthrough
	case wstateCR:
		// normally \r gets ignored if no \n follows, but at closing we just take it as a line break
		// same behavior as original textproto
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		fallthrough
	case wstateBeginLine:
		if _, err := bw.Write(smtp.Dotcrnl); err != nil {
			return err
		}
	}
	return bw.Flush()
}
