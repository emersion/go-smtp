// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package textsmtp_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"embed"
	"io"
	legacy "net/textproto"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uponusolutions/go-smtp/internal/textsmtp"
	"github.com/uponusolutions/go-smtp/tester"
)

//go:embed testdata/writer/*
var embedFSWriter embed.FS

func TestDotWriter(t *testing.T) {
	t.Run("CompareTest", func(t *testing.T) {
		tester.WriterCompareTest(t, &embedFSWriter, "testdata/writer", func(b io.Writer) io.WriteCloser {
			return legacy.NewWriter(bufio.NewWriter(b)).DotWriter()
		}, func(b io.Writer) io.WriteCloser {
			return textsmtp.NewDotWriter(bufio.NewWriter(b))
		})
	})

	t.Run("Encode", func(t *testing.T) {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		n, err := d.Write([]byte("abc\n.def\n..ghi\n.jkl\n."))
		if n != 21 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}
		require.NoError(t, d.Close())
		want := "abc\r\n..def\r\n...ghi\r\n..jkl\r\n..\r\n.\r\n"
		if s := buf.String(); s != want {
			t.Fatalf("wrote %q", s)
		}
	})

	t.Run("EncodeNoError", func(t *testing.T) {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		n, err := d.Write([]byte("abc\r\n.def\r\n..ghi\r\n.jkl\r\n."))
		if n != 25 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}
		_ = d.Close()
		want := "abc\r\n..def\r\n...ghi\r\n..jkl\r\n..\r\n.\r\n"
		if s := buf.String(); s != want {
			t.Fatalf("wrote %q", s)
		}
	})

	t.Run("EncodeSeparateWrites", func(t *testing.T) {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		n, err := d.Write([]byte("abc\r"))
		if n != 4 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		n, err = d.Write([]byte("\n.def\r\n..ghi\r\n.jkl\r\n."))
		if n != 21 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		_ = d.Close()
		want := "abc\r\n..def\r\n...ghi\r\n..jkl\r\n..\r\n.\r\n"
		if s := buf.String(); s != want {
			t.Fatalf("wrote %q", s)
		}
	})

	t.Run("EncodeSeparateWritesEndsWithR", func(t *testing.T) {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		n, err := d.Write([]byte("abc\r"))
		if n != 4 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		n, err = d.Write([]byte("\n.def\r\n..ghi\r\n.jkl\r"))
		if n != 19 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		require.NoError(t, d.Close())
		// \r gets interpreted as line break
		want := "abc\r\n..def\r\n...ghi\r\n..jkl\r\n.\r\n"
		if s := buf.String(); s != want {
			t.Fatalf("wrote %q", s)
		}
	})

	t.Run("EncodeSeparateWritesContainsR", func(t *testing.T) {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		n, err := d.Write([]byte("abc\r"))
		if n != 4 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		n, err = d.Write([]byte("\n.def\r..ghi\r\n.jkl\r"))
		if n != 18 || err != nil {
			t.Fatalf("Write: %d, %s", n, err)
		}

		require.NoError(t, d.Close())
		// \r gets interpreted as normal character
		want := "abc\r\n..def\r..ghi\r\n..jkl\r\n.\r\n"
		if s := buf.String(); s != want {
			t.Fatalf("wrote %q", s)
		}
	})
}

// TestDotWriterLeadingDot checks that a dot at the very beginning of the
// first line is stuffed, as required by RFC 5321 4.5.2 (transparency).
// Without stuffing the first line, a body starting with a dot is silently
// corrupted (or misread as end-of-data) by a conforming receiver. See
// MS-1560.
func TestDotWriterLeadingDot(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{".hello\r\n", "..hello\r\n.\r\n"},
		{".\r\nfoo\r\n", "..\r\nfoo\r\n.\r\n"},
		{"..x\r\n", "...x\r\n.\r\n"},
	} {
		var buf bytes.Buffer
		d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
		_, err := d.Write([]byte(tc.in))
		require.NoError(t, err)
		require.NoError(t, d.Close())
		require.Equal(t, tc.want, buf.String(), "input %q", tc.in)
	}
}

func TestDotWriterCloseEmptyWrite(t *testing.T) {
	var buf bytes.Buffer
	d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
	n, err := d.Write([]byte{})
	if n != 0 || err != nil {
		t.Fatalf("Write: %d, %s", n, err)
	}
	require.NoError(t, d.Close())
	want := "\r\n.\r\n"
	if s := buf.String(); s != want {
		t.Fatalf("wrote %q; want %q", s, want)
	}
}

func TestDotWriterCloseNoWrite(t *testing.T) {
	var buf bytes.Buffer
	d := textsmtp.NewDotWriter(bufio.NewWriter(&buf))
	require.NoError(t, d.Close())
	want := "\r\n.\r\n"
	if s := buf.String(); s != want {
		t.Fatalf("wrote %q; want %q", s, want)
	}
}

func BenchmarkDotWriter(b *testing.B) {
	const size = 256 * 1024 * 1024
	data, _ := io.ReadAll(io.LimitReader(rand.Reader, size))

	b.Run("Legacy", func(b *testing.B) {
		if os.Getenv("SETBYTES") == "" {
			b.SetBytes(size)
		}
		for b.Loop() {
			r := bytes.NewReader(data)
			w := legacy.NewWriter(bufio.NewWriter(io.Discard)).DotWriter()
			_, _ = io.Copy(w, r)
		}
	})

	b.Run("Optimized", func(b *testing.B) {
		if os.Getenv("SETBYTES") == "" {
			b.SetBytes(size)
		}
		for b.Loop() {
			r := bytes.NewReader(data)
			w := textsmtp.NewDotWriter(bufio.NewWriter(io.Discard))
			_, _ = io.Copy(w, r)
		}
	})

	b.Run("LegacySimpleReader", func(b *testing.B) {
		if os.Getenv("SETBYTES") == "" {
			b.SetBytes(size)
		}
		for b.Loop() {
			r := tester.NewBuffer(data)
			w := legacy.NewWriter(bufio.NewWriter(io.Discard)).DotWriter()
			_, _ = io.Copy(w, r)
		}
	})

	b.Run("OptimizedSimpleReader", func(b *testing.B) {
		if os.Getenv("SETBYTES") == "" {
			b.SetBytes(size)
		}
		for b.Loop() {
			r := tester.NewBuffer(data)
			w := textsmtp.NewDotWriter(bufio.NewWriter(io.Discard))
			_, _ = io.Copy(w, r)
		}
	})
}
