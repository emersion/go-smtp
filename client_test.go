// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/textproto"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
)

// Issue 17794: don't send a trailing space on AUTH command when there's no password.
func TestClientAuthTrimSpace(t *testing.T) {
	server := "220 hello world\r\n" +
		"200 some more"
	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}
	c := NewClient(fake)
	c.didHello = true
	c.Auth(toServerEmptyAuth{})
	c.Close()
	if got, want := wrote.String(), "AUTH FOOAUTH\r\n*\r\n"; got != want {
		t.Errorf("wrote %q; want %q", got, want)
	}
}

// toServerEmptyAuth is an implementation of Auth that only implements
// the Start method, and returns "FOOAUTH", nil, nil. Notably, it returns
// zero bytes for "toServer" so we can test that we don't send spaces at
// the end of the line. See TestClientAuthTrimSpace.
type toServerEmptyAuth struct{}

func (toServerEmptyAuth) Start() (proto string, toServer []byte, err error) {
	return "FOOAUTH", nil, nil
}

func (toServerEmptyAuth) Next(fromServer []byte) (toServer []byte, err error) {
	panic("unexpected call")
}

type faker struct {
	io.ReadWriter
}

func (f faker) Close() error                     { return nil }
func (f faker) LocalAddr() net.Addr              { return nil }
func (f faker) RemoteAddr() net.Addr             { return nil }
func (f faker) SetDeadline(time.Time) error      { return nil }
func (f faker) SetReadDeadline(time.Time) error  { return nil }
func (f faker) SetWriteDeadline(time.Time) error { return nil }

func TestBasic(t *testing.T) {
	server := strings.Join(strings.Split(basicServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(basicClient, "\n"), "\r\n")

	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := &Client{text: textproto.NewConn(fake), conn: fake, localName: "localhost"}

	if err := c.helo(); err != nil {
		t.Fatalf("HELO failed: %s", err)
	}
	if err := c.ehlo(); err == nil {
		t.Fatalf("Expected first EHLO to fail")
	}
	if err := c.ehlo(); err != nil {
		t.Fatalf("Second EHLO failed: %s", err)
	}

	c.didHello = true
	if ok, args := c.Extension("aUtH"); !ok || args != "LOGIN PLAIN" {
		t.Fatalf("Expected AUTH supported")
	}
	if ok, _ := c.Extension("DSN"); ok {
		t.Fatalf("Shouldn't support DSN")
	}
	if !c.SupportsAuth("PLAIN") {
		t.Errorf("Expected AUTH PLAIN supported")
	}
	if size, ok := c.MaxMessageSize(); !ok {
		t.Errorf("Expected SIZE supported")
	} else if size != 35651584 {
		t.Errorf("Expected SIZE=35651584, got %v", size)
	}

	if err := c.Mail("user@gmail.com", nil); err == nil {
		t.Fatalf("MAIL should require authentication")
	}

	if err := c.Verify("user1@gmail.com"); err == nil {
		t.Fatalf("First VRFY: expected no verification")
	}
	if err := c.Verify("user2@gmail.com>\r\nDATA\r\nAnother injected message body\r\n.\r\nQUIT\r\n"); err == nil {
		t.Fatalf("VRFY should have failed due to a message injection attempt")
	}
	if err := c.Verify("user2@gmail.com"); err != nil {
		t.Fatalf("Second VRFY: expected verification, got %s", err)
	}

	c.serverName = "smtp.google.com"
	if err := c.Auth(sasl.NewPlainClient("", "user", "pass")); err != nil {
		t.Fatalf("AUTH failed: %s", err)
	}

	if err := c.Rcpt("golang-nuts@googlegroups.com>\r\nDATA\r\nInjected message body\r\n.\r\nQUIT\r\n", nil); err == nil {
		t.Fatalf("RCPT should have failed due to a message injection attempt")
	}
	if err := c.Mail("user@gmail.com>\r\nDATA\r\nAnother injected message body\r\n.\r\nQUIT\r\n", nil); err == nil {
		t.Fatalf("MAIL should have failed due to a message injection attempt")
	}
	if err := c.Mail("user@gmail.com", nil); err != nil {
		t.Fatalf("MAIL failed: %s", err)
	}
	if err := c.Rcpt("golang-nuts@googlegroups.com", nil); err != nil {
		t.Fatalf("RCPT failed: %s", err)
	}
	msg := `From: user@gmail.com
To: golang-nuts@googlegroups.com
Subject: Hooray for Go

Line 1
.Leading dot line .
Goodbye.`
	w, err := c.Data()
	if err != nil {
		t.Fatalf("DATA failed: %s", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("Data write failed: %s", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Bad data response: %s", err)
	}

	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT failed: %s", err)
	}

	bcmdbuf.Flush()
	actualcmds := cmdbuf.String()
	if client != actualcmds {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualcmds, client)
	}
}

func TestBasic_SMTPError(t *testing.T) {
	faultyServer := `220 mx.google.com at your service
250-mx.google.com at your service
250 ENHANCEDSTATUSCODES
500 5.0.0 Failing with enhanced code
500 Failing without enhanced code
500-5.0.0 Failing with multiline and enhanced code
500 5.0.0 ... still failing
`
	// RFC 2034 says that enhanced codes *SHOULD* be included in errors,
	// this means it can be violated hence we need to handle last
	// case properly.

	faultyServer = strings.Join(strings.Split(faultyServer, "\n"), "\r\n")

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(faultyServer),
		&wrote,
	}
	c := NewClient(fake)

	err := c.Mail("whatever", nil)
	if err == nil {
		t.Fatal("MAIL succeeded")
	}
	smtpErr, ok := err.(*SMTPError)
	if !ok {
		t.Fatal("Returned error is not SMTPError")
	}
	if smtpErr.Code != 500 {
		t.Fatalf("Wrong status code, got %d, want %d", smtpErr.Code, 500)
	}
	if smtpErr.EnhancedCode != (EnhancedCode{5, 0, 0}) {
		t.Fatalf("Wrong enhanced code, got %v, want %v", smtpErr.EnhancedCode, EnhancedCode{5, 0, 0})
	}
	if smtpErr.Message != "Failing with enhanced code" {
		t.Fatalf("Wrong message, got %s, want %s", smtpErr.Message, "Failing with enhanced code")
	}

	err = c.Mail("whatever", nil)
	if err == nil {
		t.Fatal("MAIL succeeded")
	}
	smtpErr, ok = err.(*SMTPError)
	if !ok {
		t.Fatal("Returned error is not SMTPError")
	}
	if smtpErr.Code != 500 {
		t.Fatalf("Wrong status code, got %d, want %d", smtpErr.Code, 500)
	}
	if smtpErr.Message != "Failing without enhanced code" {
		t.Fatalf("Wrong message, got %s, want %s", smtpErr.Message, "Failing without enhanced code")
	}

	err = c.Mail("whatever", nil)
	if err == nil {
		t.Fatal("MAIL succeeded")
	}
	smtpErr, ok = err.(*SMTPError)
	if !ok {
		t.Fatal("Returned error is not SMTPError")
	}
	if smtpErr.Code != 500 {
		t.Fatalf("Wrong status code, got %d, want %d", smtpErr.Code, 500)
	}
	if want := "Failing with multiline and enhanced code\n... still failing"; smtpErr.Message != want {
		t.Fatalf("Wrong message, got %s, want %s", smtpErr.Message, want)
	}
}

func TestClient_TooLongLine(t *testing.T) {
	faultyServer := []string{
		"220 mx.google.com at your service\r\n",
		"220 mx.google.com at your service\r\n",
		"500 5.0.0 nU6XC5JJUfiuIkC7NhrxZz36Rl/rXpkfx9QdeZJ+rno6W5J9k9HvniyWXBBi1gOZ/CUXEI6K7Uony70eiVGGGkdFhP1rEvMGny1dqIRo3NM2NifrvvLIKGeX6HrYmkc7NMn9BwHyAnt5oLe5eNVDI+grwIikVPNVFZi0Dg4Xatdg5Cs8rH1x9BWhqyDoxosJst4wRoX4AymYygUcftM3y16nVg/qcb1GJwxSNbah7VjOiSrk6MlTdGR/2AwIIcSw7pZVJjGbCorniOTvKBcyut1YdbrX/4a/dBhvLfZtdSccqyMZAdZno+tGrnu+N2ghFvz6cx6bBab9Z4JJQMlkK/g1y7xjEPr6nKwruAf71NzOclPK5wzs2hY3Ku9xEjU0Cd+g/OjAzVsmeJk2U0q+vmACZsFAiOlRynXKFPLqMAg8skM5lioRTm05K/u3aBaUq0RKloeBHZ/zNp/kfHNp6TmJKAzvsXD3Xdo+PRAgCZRTRAl3ydGdrOOjxTULCVlgOL6xSAJdj9zGkzQoEW4tRmp1OiIab4GSxCtkIo7XnAowJ7EPUfDGTV3hhl5Qn7jvZjPCPlruRTtzVTho7D3HBEouWv1qDsqdED23myw0Ma9ZlobSf9eHqsSv1MxjKG2D5DdFBACu6pXGz3ceGreOHYWnI74TkoHtQ5oNuF6VUkGjGN+f4fOaiypQ54GJ8skTNoSCHLK4XF8ZutSxWzMR+LKoJBWMb6bdAiFNt+vXZOUiTgmTqs6Sw79JXqDX9YFxryJMKjHMiFkm+RZbaK5sIOXqyq+RNmOJ+G0unrQHQMCES476c7uvOlYrNoJtq+uox1qFdisIE/8vfSoKBlTtw+r2m87djIQh4ip/hVmalvtiF5fnVTxigbtwLWv8rAOCXKoktU0c2ie0a5hGtvZT0SXxwX8K2CeYXb81AFD2IaLt/p8Q4WuZ82eOCeXP72qP9yWYj6mIZdgyimm8wjrDowt2yPJU28ZD6k3Ei6C31OKgMpCf8+MW504/VCwld7czAIwjJiZe3DxtUdfM7Q565OzLiWQgI8fxjsvlCKMiOY7q42IGGsVxXJAFMtDKdchgqQA1PJR1vrw+SbI3Mh4AGnn8vKn+WTsieB3qkloo7MZlpMz/bwPXg7XadOVkUaVeHrZ5OsqDWhsWOLtPZLi5XdNazPzn9uxWbpelXEBKAjZzfoawSUgGT5vCYACNfz/yIw1DB067N+HN1KvVddI6TNBA32lpqkQ6VwdWztq6pREE51sNl9p7MUzr+ef0331N5DqQsy+epmRDwebosCx15l/rpvBc91OnxmMMXDNtmxSzVxaZjyGDmJ7RDdTy/Su76AlaMP1zxivxg2MU/9zyTzM16coIAMOd/6Uo9ezKgbZEPeMROKTzAld9BhK9BBPWofoQ0mBkVc7btnahQe3u8HoD6SKCkr9xcTcC9ZKpLkc4svrmxT9e0858pjhis9BbWD/owa6552n2+KwUMRyB8ys7rPL86hh9lBTS+05cVL+BmJfNHOA6ZizdGc3lpwIVbFmzMR5BM0HRf3OCntkWojgsdsP8BGZWHiCGGqA7YGa5AOleR887r8Zhyp47DT3Cn3Rg/icYurIx7Yh0p696gxfANo4jEkE2BOroIscDnhauwck5CCJMcabpTrGwzK8NJ+xZnCUplXnZiIaj85Uh9+yI670B4bybWlZoVmALUxxuQ8bSMAp7CAzMcMWbYJHwBqLF8V2qMj3/g81S3KOptn8b7Idh7IMzAkV8VxE3qAguzwS0zEu8l894sOFUPiJq2/llFeiHNOcEQUGJ+8ATJSAFOMDXAeQS2FoIDOYdesO6yacL0zUkvDydWbA84VXHW8DvdHPli/8hmc++dn5CXSDeBJfC/yypvrpLgkSilZMuHEYHEYHEYEHYEHEYEHEYEHEYEYEYEYEYEYEYEYEYEYEYEYEYEYEYEYEYEYEYYEYEYEYEYEYEYEYYEYEYEYEYEYEYEYEY\r\n",
		"220 2.0.0 Kk\r\n",
	}

	// The pipe is used to avoid bufio.Reader reading the too long line ahead
	// of time (in NewClient) and failing eariler than we expect.
	pr, pw := io.Pipe()

	go func() {
		for _, l := range faultyServer {
			pw.Write([]byte(l))
		}
		pw.Close()
	}()

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		pr,
		&wrote,
	}
	c := NewClient(fake)

	err := c.Mail("whatever", nil)
	if err != ErrTooLongLine {
		t.Fatal("MAIL succeeded or returned a different error:", err)
	}

	// ErrTooLongLine is "sticky" since the connection is in broken state and
	// the only reasonable way to recover is to close it.
	err = c.Mail("whatever", nil)
	if err != ErrTooLongLine {
		t.Fatal("Second MAIL succeeded or returned a different error:", err)
	}
}

var basicServer = `250 mx.google.com at your service
502 Unrecognized command.
250-mx.google.com at your service
250-SIZE 35651584
250-AUTH LOGIN PLAIN
250 8BITMIME
530 Authentication required
252 Send some mail, I'll try my best
250 User is valid
235 Accepted
250 Sender OK
250 Receiver OK
354 Go ahead
250 Data OK
221 OK
`

var basicClient = `HELO localhost
EHLO localhost
EHLO localhost
MAIL FROM:<user@gmail.com> BODY=8BITMIME
VRFY user1@gmail.com
VRFY user2@gmail.com
AUTH PLAIN AHVzZXIAcGFzcw==
MAIL FROM:<user@gmail.com> BODY=8BITMIME
RCPT TO:<golang-nuts@googlegroups.com>
DATA
From: user@gmail.com
To: golang-nuts@googlegroups.com
Subject: Hooray for Go

Line 1
..Leading dot line .
Goodbye.
.
QUIT
`

func TestNewClient(t *testing.T) {
	server := strings.Join(strings.Split(newClientServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(newClientClient, "\n"), "\r\n")

	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	out := func() string {
		bcmdbuf.Flush()
		return cmdbuf.String()
	}
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := NewClient(fake)
	defer c.Close()
	if ok, args := c.Extension("aUtH"); !ok || args != "LOGIN PLAIN" {
		t.Fatalf("Expected AUTH supported")
	}
	if ok, _ := c.Extension("DSN"); ok {
		t.Fatalf("Shouldn't support DSN")
	}
	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT failed: %s", err)
	}

	actualcmds := out()
	if client != actualcmds {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualcmds, client)
	}
}

var newClientServer = `220 hello world
250-mx.google.com at your service
250-SIZE 35651584
250-AUTH LOGIN PLAIN
250 8BITMIME
221 OK
`

var newClientClient = `EHLO localhost
QUIT
`

func TestNewClient2(t *testing.T) {
	server := strings.Join(strings.Split(newClient2Server, "\n"), "\r\n")
	client := strings.Join(strings.Split(newClient2Client, "\n"), "\r\n")

	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := NewClient(fake)
	defer c.Close()
	if ok, _ := c.Extension("DSN"); ok {
		t.Fatalf("Shouldn't support DSN")
	}
	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT failed: %s", err)
	}

	bcmdbuf.Flush()
	actualcmds := cmdbuf.String()
	if client != actualcmds {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualcmds, client)
	}
}

var newClient2Server = `220 hello world
502 EH?
250-mx.google.com at your service
250-SIZE 35651584
250-AUTH LOGIN PLAIN
250 8BITMIME
221 OK
`

var newClient2Client = `EHLO localhost
HELO localhost
QUIT
`

func TestHello(t *testing.T) {

	if len(helloServer) != len(helloClient) {
		t.Fatalf("Hello server and client size mismatch")
	}

	for i := 0; i < len(helloServer); i++ {
		server := strings.Join(strings.Split(baseHelloServer+helloServer[i], "\n"), "\r\n")
		client := strings.Join(strings.Split(baseHelloClient+helloClient[i], "\n"), "\r\n")
		var cmdbuf bytes.Buffer
		bcmdbuf := bufio.NewWriter(&cmdbuf)
		var fake faker
		fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
		c := NewClient(fake)
		defer c.Close()
		c.serverName = "fake.host"
		c.localName = "customhost"

		var err error
		switch i {
		case 0:
			err = c.Hello("hostinjection>\n\rDATA\r\nInjected message body\r\n.\r\nQUIT\r\n")
			if err == nil {
				t.Errorf("Expected Hello to be rejected due to a message injection attempt")
			}
			err = c.Hello("customhost")
		case 1:
			err = c.startTLS(nil)
			if err.Error() == "SMTP error 502: Not implemented" {
				err = nil
			}
		case 2:
			err = c.Verify("test@example.com")
		case 3:
			c.serverName = "smtp.google.com"
			err = c.Auth(sasl.NewPlainClient("", "user", "pass"))
		case 4:
			err = c.Mail("test@example.com", nil)
		case 5:
			ok, _ := c.Extension("feature")
			if ok {
				t.Errorf("Expected FEATURE not to be supported")
			}
		case 6:
			err = c.Reset()
		case 7:
			err = c.Quit()
		case 8:
			err = c.Verify("test@example.com")
			if err != nil {
				err = c.Hello("customhost")
				if err != nil {
					t.Errorf("Want error, got none")
				}
			}
		case 9:
			err = c.Noop()
		default:
			t.Fatalf("Unhandled command")
		}

		if err != nil {
			t.Errorf("Command %d failed: %v", i, err)
		}

		bcmdbuf.Flush()
		actualcmds := cmdbuf.String()
		if client != actualcmds {
			t.Errorf("Got:\n%s\nExpected:\n%s", actualcmds, client)
		}
	}
}

var baseHelloServer = `220 hello world
502 EH?
250-mx.google.com at your service
250 FEATURE
`

var helloServer = []string{
	"",
	"502 Not implemented\n",
	"250 User is valid\n",
	"235 Accepted\n",
	"250 Sender ok\n",
	"",
	"250 Reset ok\n",
	"221 Goodbye\n",
	"250 Sender ok\n",
	"250 ok\n",
}

var baseHelloClient = `EHLO customhost
HELO customhost
`

var helloClient = []string{
	"",
	"STARTTLS\n",
	"VRFY test@example.com\n",
	"AUTH PLAIN AHVzZXIAcGFzcw==\n",
	"MAIL FROM:<test@example.com>\n",
	"",
	"RSET\n",
	"QUIT\n",
	"VRFY test@example.com\n",
	"NOOP\n",
}

var sendMailServer = `220 hello world
502 EH?
250 mx.google.com at your service
250 Sender ok
250 Receiver ok
354 Go ahead
250 Data ok
221 Goodbye
`

var sendMailClient = `EHLO localhost
HELO localhost
MAIL FROM:<test@example.com>
RCPT TO:<other@example.com>
DATA
From: test@example.com
To: other@example.com
Subject: SendMail test

SendMail is working for me.
.
QUIT
`

func TestAuthFailed(t *testing.T) {
	server := strings.Join(strings.Split(authFailedServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(authFailedClient, "\n"), "\r\n")
	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := NewClient(fake)
	defer c.Close()

	c.serverName = "smtp.google.com"
	err := c.Auth(sasl.NewPlainClient("", "user", "pass"))

	if err == nil {
		t.Error("Auth: expected error; got none")
	} else if err.Error() != "SMTP error 535: Invalid credentials\nplease see www.example.com" {
		t.Errorf("Auth: got error: %v, want: %s", err, "Invalid credentials\nplease see www.example.com")
	}

	bcmdbuf.Flush()
	actualcmds := cmdbuf.String()
	if client != actualcmds {
		t.Errorf("Got:\n%s\nExpected:\n%s", actualcmds, client)
	}
}

var authFailedServer = `220 hello world
250-mx.google.com at your service
250 AUTH LOGIN PLAIN
535-Invalid credentials
535 please see www.example.com
221 Goodbye
`

var authFailedClient = `EHLO localhost
AUTH PLAIN AHVzZXIAcGFzcw==
*
`

func TestTLSClient(t *testing.T) {
	ln := newLocalListener(t)
	defer ln.Close()
	errc := make(chan error)
	go func() {
		errc <- doSendMail(ln.Addr().String())
	}()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("failed to accept connection: %v", err)
	}
	defer conn.Close()
	if err := serverHandle(conn, t); err != nil {
		t.Fatalf("failed to handle connection: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("client error: %v", err)
	}
}

func TestTLSConnState(t *testing.T) {
	ln := newLocalListener(t)
	defer ln.Close()
	clientDone := make(chan bool)
	serverDone := make(chan bool)
	go func() {
		defer close(serverDone)
		c, err := ln.Accept()
		if err != nil {
			t.Errorf("Server accept: %v", err)
			return
		}
		defer c.Close()
		if err := serverHandle(c, t); err != nil {
			t.Errorf("server error: %v", err)
		}
	}()
	go func() {
		defer close(clientDone)
		cfg := &tls.Config{ServerName: "example.com"}
		testHookStartTLS(cfg) // set the RootCAs
		c, err := DialStartTLS(ln.Addr().String(), cfg)
		if err != nil {
			t.Errorf("Client dial: %v", err)
			return
		}
		defer c.Quit()
		cs, ok := c.TLSConnectionState()
		if !ok {
			t.Errorf("TLSConnectionState returned ok == false; want true")
			return
		}
		if cs.Version == 0 || !cs.HandshakeComplete {
			t.Errorf("ConnectionState = %#v; expect non-zero Version and HandshakeComplete", cs)
		}
	}()
	<-clientDone
	<-serverDone
}

func newLocalListener(t *testing.T) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ln, err = net.Listen("tcp6", "[::1]:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

type smtpSender struct {
	w io.Writer
}

func (s smtpSender) send(f string) {
	s.w.Write([]byte(f + "\r\n"))
}

// smtp server, finely tailored to deal with our own client only!
func serverHandle(c net.Conn, t *testing.T) error {
	send := smtpSender{c}.send
	send("220 127.0.0.1 ESMTP service ready")
	s := bufio.NewScanner(c)
	for s.Scan() {
		switch s.Text() {
		case "EHLO localhost":
			send("250-127.0.0.1 ESMTP offers a warm hug of welcome")
			send("250-STARTTLS")
			send("250 Ok")
		case "STARTTLS":
			send("220 Go ahead")
			keypair, err := tls.X509KeyPair(localhostCert, localhostKey)
			if err != nil {
				return err
			}
			config := &tls.Config{Certificates: []tls.Certificate{keypair}}
			c = tls.Server(c, config)
			defer c.Close()
			return serverHandleTLS(c, t)
		default:
			t.Fatalf("unrecognized command: %q", s.Text())
		}
	}
	return s.Err()
}

func serverHandleTLS(c net.Conn, t *testing.T) error {
	send := smtpSender{c}.send
	s := bufio.NewScanner(c)
	for s.Scan() {
		switch s.Text() {
		case "EHLO localhost":
			send("250 Ok")
		case "MAIL FROM:<joe1@example.com>":
			send("250 Ok")
		case "RCPT TO:<joe2@example.com>":
			send("250 Ok")
		case "DATA":
			send("354 send the mail data, end with .")
			send("250 Ok")
		case "Subject: test":
		case "":
		case "howdy!":
		case ".":
		case "QUIT":
			send("221 127.0.0.1 Service closing transmission channel")
			return nil
		default:
			t.Fatalf("unrecognized command during TLS: %q", s.Text())
		}
	}
	return s.Err()
}

func init() {
	testRootCAs := x509.NewCertPool()
	testRootCAs.AppendCertsFromPEM(localhostCert)
	testHookStartTLS = func(config *tls.Config) {
		config.RootCAs = testRootCAs
	}
}

func doSendMail(hostPort string) error {
	from := "joe1@example.com"
	to := []string{"joe2@example.com"}
	return SendMail(hostPort, nil, from, to, strings.NewReader("Subject: test\n\nhowdy!"))
}

// localhostCert is a PEM-encoded TLS cert generated from src/crypto/tls:
//
//	go run generate_cert.go --rsa-bits 1024 --host 127.0.0.1,::1,example.com \
//			--ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var localhostCert = []byte(`
-----BEGIN CERTIFICATE-----
MIICFDCCAX2gAwIBAgIRAK0xjnaPuNDSreeXb+z+0u4wDQYJKoZIhvcNAQELBQAw
EjEQMA4GA1UEChMHQWNtZSBDbzAgFw03MDAxMDEwMDAwMDBaGA8yMDg0MDEyOTE2
MDAwMFowEjEQMA4GA1UEChMHQWNtZSBDbzCBnzANBgkqhkiG9w0BAQEFAAOBjQAw
gYkCgYEA0nFbQQuOWsjbGtejcpWz153OlziZM4bVjJ9jYruNw5n2Ry6uYQAffhqa
JOInCmmcVe2siJglsyH9aRh6vKiobBbIUXXUU1ABd56ebAzlt0LobLlx7pZEMy30
LqIi9E6zmL3YvdGzpYlkFRnRrqwEtWYbGBf3znO250S56CCWH2UCAwEAAaNoMGYw
DgYDVR0PAQH/BAQDAgKkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1UdEwEB/wQF
MAMBAf8wLgYDVR0RBCcwJYILZXhhbXBsZS5jb22HBH8AAAGHEAAAAAAAAAAAAAAA
AAAAAAEwDQYJKoZIhvcNAQELBQADgYEAbZtDS2dVuBYvb+MnolWnCNqvw1w5Gtgi
NmvQQPOMgM3m+oQSCPRTNGSg25e1Qbo7bgQDv8ZTnq8FgOJ/rbkyERw2JckkHpD4
n4qcK27WkEDBtQFlPihIM8hLIuzWoi/9wygiElTy/tVL3y7fGCvY2/k1KBthtZGF
tN8URjVmyEo=
-----END CERTIFICATE-----`)

// localhostKey is the private key for localhostCert.
var localhostKey = []byte(`
-----BEGIN RSA PRIVATE KEY-----
MIICXgIBAAKBgQDScVtBC45ayNsa16NylbPXnc6XOJkzhtWMn2Niu43DmfZHLq5h
AB9+Gpok4icKaZxV7ayImCWzIf1pGHq8qKhsFshRddRTUAF3np5sDOW3QuhsuXHu
lkQzLfQuoiL0TrOYvdi90bOliWQVGdGurAS1ZhsYF/fOc7bnRLnoIJYfZQIDAQAB
AoGBAMst7OgpKyFV6c3JwyI/jWqxDySL3caU+RuTTBaodKAUx2ZEmNJIlx9eudLA
kucHvoxsM/eRxlxkhdFxdBcwU6J+zqooTnhu/FE3jhrT1lPrbhfGhyKnUrB0KKMM
VY3IQZyiehpxaeXAwoAou6TbWoTpl9t8ImAqAMY8hlULCUqlAkEA+9+Ry5FSYK/m
542LujIcCaIGoG1/Te6Sxr3hsPagKC2rH20rDLqXwEedSFOpSS0vpzlPAzy/6Rbb
PHTJUhNdwwJBANXkA+TkMdbJI5do9/mn//U0LfrCR9NkcoYohxfKz8JuhgRQxzF2
6jpo3q7CdTuuRixLWVfeJzcrAyNrVcBq87cCQFkTCtOMNC7fZnCTPUv+9q1tcJyB
vNjJu3yvoEZeIeuzouX9TJE21/33FaeDdsXbRhQEj23cqR38qFHsF1qAYNMCQQDP
QXLEiJoClkR2orAmqjPLVhR3t2oB3INcnEjLNSq8LHyQEfXyaFfu4U9l5+fRPL2i
jiC0k/9L5dHUsF0XZothAkEA23ddgRs+Id/HxtojqqUT27B8MT/IGNrYsp4DvS/c
qgkeluku4GjxRlDMBuXk94xOBEinUs+p/hwP1Alll80Tpg==
-----END RSA PRIVATE KEY-----`)

func TestLMTP(t *testing.T) {
	server := strings.Join(strings.Split(lmtpServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(lmtpClient, "\n"), "\r\n")

	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := &Client{text: textproto.NewConn(fake), conn: fake, lmtp: true}

	if err := c.Hello("localhost"); err != nil {
		t.Fatalf("LHLO failed: %s", err)
	}
	c.didHello = true

	if err := c.Mail("user@gmail.com", nil); err != nil {
		t.Fatalf("MAIL failed: %s", err)
	}
	if err := c.Rcpt("golang-nuts@googlegroups.com", nil); err != nil {
		t.Fatalf("RCPT failed: %s", err)
	}
	msg := `From: user@gmail.com
To: golang-nuts@googlegroups.com
Subject: Hooray for Go

Line 1
.Leading dot line .
Goodbye.`
	w, err := c.Data()
	if err != nil {
		t.Fatalf("DATA failed: %s", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("Data write failed: %s", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Bad data response: %s", err)
	}

	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT failed: %s", err)
	}

	bcmdbuf.Flush()
	actualcmds := cmdbuf.String()
	if client != actualcmds {
		t.Fatalf("Got:\n%s\nExpected:\n%s", actualcmds, client)
	}
}

var lmtpServer = `220 localhost Simple Mail Transfer Service Ready
250-localhost at your service
250-SIZE 35651584
250 8BITMIME
250 Sender OK
250 Receiver OK
354 Go ahead
250 Data OK
221 OK
`

var lmtpClient = `LHLO localhost
MAIL FROM:<user@gmail.com> BODY=8BITMIME
RCPT TO:<golang-nuts@googlegroups.com>
DATA
From: user@gmail.com
To: golang-nuts@googlegroups.com
Subject: Hooray for Go

Line 1
..Leading dot line .
Goodbye.
.
QUIT
`

func TestLMTPData(t *testing.T) {
	var lmtpServerPartial = `220 localhost Simple Mail Transfer Service Ready
250-localhost at your service
250-SIZE 35651584
250 8BITMIME
250 Sender OK
250 Receiver OK
250 Receiver OK
354 Go ahead
250 This recipient is fine
500 But not this one
221 OK
`
	server := strings.Join(strings.Split(lmtpServerPartial, "\n"), "\r\n")

	var cmdbuf bytes.Buffer
	bcmdbuf := bufio.NewWriter(&cmdbuf)
	var fake faker
	fake.ReadWriter = bufio.NewReadWriter(bufio.NewReader(strings.NewReader(server)), bcmdbuf)
	c := &Client{text: textproto.NewConn(fake), conn: fake, lmtp: true}

	if err := c.Hello("localhost"); err != nil {
		t.Fatalf("LHLO failed: %s", err)
	}
	c.didHello = true

	if err := c.Mail("user@gmail.com", nil); err != nil {
		t.Fatalf("MAIL failed: %s", err)
	}
	if err := c.Rcpt("golang-nuts@googlegroups.com", nil); err != nil {
		t.Fatalf("RCPT failed: %s", err)
	}
	if err := c.Rcpt("golang-not-nuts@googlegroups.com", nil); err != nil {
		t.Fatalf("RCPT failed: %s", err)
	}
	msg := `From: user@gmail.com
To: golang-nuts@googlegroups.com
Subject: Hooray for Go

Line 1
.Leading dot line .
Goodbye.`

	rcpts := []string{}
	errors := []*SMTPError{}

	w, err := c.LMTPData(func(rcpt string, status *SMTPError) {
		rcpts = append(rcpts, rcpt)
		errors = append(errors, status)
	})
	if err != nil {
		t.Fatalf("DATA failed: %s", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("Data write failed: %s", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Bad data response: %s", err)
	}

	if !reflect.DeepEqual(rcpts, []string{"golang-nuts@googlegroups.com", "golang-not-nuts@googlegroups.com"}) {
		t.Fatal("Status callbacks called for wrong recipients:", rcpts)
	}

	if len(errors) != 2 {
		t.Fatalf("Wrong amount of status callback calls: %v", len(errors))
	}
	if errors[0] != nil {
		t.Fatalf("Unexpected error status for the first recipient: %v", errors[0])
	}
	if errors[1] == nil {
		t.Fatalf("Unexpected success status for the second recipient")
	}

	if err := c.Quit(); err != nil {
		t.Fatalf("QUIT failed: %s", err)
	}
}

var xtextClient = `MAIL FROM:<e=mc2@example.com> AUTH=e+3Dmc2@example.com
RCPT TO:<e=mc2@example.com> ORCPT=UTF-8;e\x{3D}mc2@example.com
`

func TestClientXtext(t *testing.T) {
	server := "220 hello world\r\n" +
		"250 ok\r\n" +
		"250 ok"
	client := strings.Join(strings.Split(xtextClient, "\n"), "\r\n")
	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}
	c := NewClient(fake)
	c.didHello = true
	c.ext = map[string]string{"AUTH": "PLAIN", "DSN": ""}
	email := "e=mc2@example.com"
	c.Mail(email, &MailOptions{Auth: &email})
	c.Rcpt(email, &RcptOptions{
		OriginalRecipientType: DSNAddressTypeUTF8,
		OriginalRecipient:     email,
	})
	c.Close()
	if got := wrote.String(); got != client {
		t.Errorf("wrote %q; want %q", got, client)
	}
}

const (
	dsnEnvelopeID  = "e=mc2"
	dsnEmailRFC822 = "e=mc2@example.com"
	dsnEmailUTF8   = "e=mc2@ドメイン名例.jp"
)

var dsnServer = `220 hello world
250 ok
250 ok
250 ok
250 ok
`

var dsnClient = `MAIL FROM:<e=mc2@example.com> RET=HDRS ENVID=e+3Dmc2
RCPT TO:<e=mc2@example.com> NOTIFY=NEVER ORCPT=RFC822;e+3Dmc2@example.com
RCPT TO:<e=mc2@example.com> NOTIFY=FAILURE,DELAY ORCPT=UTF-8;e\x{3D}mc2@\x{30C9}\x{30E1}\x{30A4}\x{30F3}\x{540D}\x{4F8B}.jp
RCPT TO:<e=mc2@ドメイン名例.jp> ORCPT=UTF-8;e\x{3D}mc2@ドメイン名例.jp
`

func TestClientDSN(t *testing.T) {
	server := strings.Join(strings.Split(dsnServer, "\n"), "\r\n")
	client := strings.Join(strings.Split(dsnClient, "\n"), "\r\n")

	var wrote bytes.Buffer
	var fake faker
	fake.ReadWriter = struct {
		io.Reader
		io.Writer
	}{
		strings.NewReader(server),
		&wrote,
	}
	c := NewClient(fake)
	c.didHello = true
	c.ext = map[string]string{"DSN": ""}
	c.Mail(dsnEmailRFC822, &MailOptions{
		Return:     DSNReturnHeaders,
		EnvelopeID: dsnEnvelopeID,
	})
	c.Rcpt(dsnEmailRFC822, &RcptOptions{
		OriginalRecipientType: DSNAddressTypeRFC822,
		OriginalRecipient:     dsnEmailRFC822,
		Notify:                []DSNNotify{DSNNotifyNever},
	})
	c.Rcpt(dsnEmailRFC822, &RcptOptions{
		OriginalRecipientType: DSNAddressTypeUTF8,
		OriginalRecipient:     dsnEmailUTF8,
		Notify:                []DSNNotify{DSNNotifyFailure, DSNNotifyDelayed},
	})
	c.ext["SMTPUTF8"] = ""
	c.Rcpt(dsnEmailUTF8, &RcptOptions{
		OriginalRecipientType: DSNAddressTypeUTF8,
		OriginalRecipient:     dsnEmailUTF8,
	})
	c.Close()
	if actualcmds := wrote.String(); client != actualcmds {
		t.Errorf("wrote %q; want %q", actualcmds, client)
	}
}
