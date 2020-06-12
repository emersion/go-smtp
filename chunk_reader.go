package smtp

import (
	"errors"
	"io"
	"io/ioutil"
)

// ErrDataReset is returned by Reader pased to Data function if client does not
// send another BDAT command and instead closes connection or issues RSET command.
var ErrDataReset = errors.New("smtp: message transmission aborted")

// chunkReader implements io.Reader by consuming a sequence of size-limited
// "chunks" from underlying Reader allowing them to be interleaved with other
// protocol data.
//
// It is caller responsibility to not use Reader while chunk is being processed.
// This can be enforced by blocking on chunkEnd channel that is used to signal the
// end of another chunk being reached.
type chunkReader struct {
	remainingBytes int
	r              io.Reader
	chunks         chan int
	// Sent to by abort() to unlock running Read.
	rset         chan struct{}
	currentChunk *io.LimitedReader

	chunkEnd chan struct{}
}

func (cr *chunkReader) addChunk(size int) {
	cr.chunks <- size
}

func (cr *chunkReader) end() {
	close(cr.chunks)
}

func (cr *chunkReader) abort() {
	close(cr.rset)
	close(cr.chunkEnd)
}

func (cr *chunkReader) discardCurrentChunk() error {
	if cr.currentChunk == nil {
		return nil
	}
	_, err := io.Copy(ioutil.Discard, cr.currentChunk)
	return err
}

func (cr *chunkReader) waitNextChunk() error {
	select {
	case <-cr.rset:
		return ErrDataReset
	case r, ok := <-cr.chunks:
		if !ok {
			// Okay, that's the end.
			return io.EOF
		}
		cr.currentChunk = &io.LimitedReader{R: cr.r, N: int64(r)}
		return nil
	}
}

func (cr *chunkReader) Read(b []byte) (int, error) {
	/*
		Possible states:

		1. We are at the start of next chunk.
		cr.currentChunk == nil, cr.chunks is not closed.

		2. We are in the middle of chunk.
		cr.currentchunk != nil

		3. Chunk ended, cr.currentChunk returns io.EOF.
		Signal connection handling code to send 250 (using chunkEnd)
		and wait for the next chunk to arrive.
	*/

	if cr.currentChunk == nil {
		if err := cr.waitNextChunk(); err != nil {
			return 0, err
		}
	}

	n, err := cr.currentChunk.Read(b)
	if err == io.EOF {
		cr.chunkEnd <- struct{}{}
		cr.currentChunk = nil
		err = nil
	}

	if cr.remainingBytes != 0 /* no limit */ {
		cr.remainingBytes -= n
		if cr.remainingBytes <= 0 {
			return 0, ErrDataTooLarge
		}
	}

	// Strip CR from slice contents.
	offset := 0
	for i, chr := range b {
		if chr == '\r' {
			offset += 1
		}
		if i+offset >= len(b) {
			break
		}
		b[i] = b[i+offset]
	}

	// We also likely left garbage in remaining bytes but lets hope backend
	// code does not assume they are intact.
	return n - offset, err

}

func newChunkReader(conn io.Reader, maxBytes int) *chunkReader {
	return &chunkReader{
		remainingBytes: maxBytes,
		r:              conn,
		chunks:         make(chan int, 1),
		// buffer to make sure abort() will not block if Read is not running.
		rset:     make(chan struct{}, 1),
		chunkEnd: make(chan struct{}, 1),
	}
}
