package smtp_test

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

type message struct {
	From string
	To   []string
	Data []byte
}

type backend struct {
	messages []*message
	anonmsgs []*message

	userErr error
}

func (be *backend) Login(username, password string) (smtp.User, error) {
	if be.userErr != nil {
		return &user{}, be.userErr
	}

	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &user{backend: be}, nil
}

func (be *backend) AnonymousLogin() (smtp.User, error) {
	if be.userErr != nil {
		return &user{}, be.userErr
	}

	return &user{backend: be, anonymous: true}, nil
}

type user struct {
	backend   *backend
	anonymous bool
}

func (u *user) Send(from string, to []string, r io.Reader) error {
	if b, err := ioutil.ReadAll(r); err != nil {
		return err
	} else {
		msg := &message{
			From: from,
			To:   to,
			Data: b,
		}

		if u.anonymous {
			u.backend.anonmsgs = append(u.backend.anonmsgs, msg)
		} else {
			u.backend.messages = append(u.backend.messages, msg)
		}
	}
	return nil
}

func (u *user) Logout() error {
	return nil
}

type serverConfigureFunc func(*smtp.Server)

var (
	authDisabled = func(s *smtp.Server) {
		s.AuthDisabled = true
	}
)

func testServer(t *testing.T, fn ...serverConfigureFunc) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	be = new(backend)
	s = smtp.NewServer(be)
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	for _, f := range fn {
		f(s)
	}

	go s.Serve(l)

	c, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	scanner = bufio.NewScanner(c)
	return
}

func testServerGreeted(t *testing.T, fn ...serverConfigureFunc) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	be, s, c, scanner = testServer(t, fn...)

	scanner.Scan()
	if scanner.Text() != "220 localhost ESMTP Service Ready" {
		t.Fatal("Invalid greeting:", scanner.Text())
	}

	return
}

func testServerEhlo(t *testing.T, fn ...serverConfigureFunc) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner, caps map[string]bool) {
	be, s, c, scanner = testServerGreeted(t, fn...)

	io.WriteString(c, "EHLO localhost\r\n")

	scanner.Scan()
	if scanner.Text() != "250-Hello localhost" {
		t.Fatal("Invalid EHLO response:", scanner.Text())
	}

	expectedCaps := []string{"PIPELINING", "8BITMIME"}
	caps = make(map[string]bool)

	for scanner.Scan() {
		s := scanner.Text()

		if strings.HasPrefix(s, "250 ") {
			caps[strings.TrimPrefix(s, "250 ")] = true
			break
		} else {
			if !strings.HasPrefix(s, "250-") {
				t.Fatal("Invalid capability response:", s)
			}
			caps[strings.TrimPrefix(s, "250-")] = true
		}
	}

	for _, cap := range expectedCaps {
		if !caps[cap] {
			t.Fatal("Missing capability:", cap)
		}
	}

	return
}

func TestServer_helo(t *testing.T) {
	_, s, c, scanner := testServerGreeted(t)
	defer s.Close()

	io.WriteString(c, "HELO localhost\r\n")

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid HELO response:", scanner.Text())
	}
}

func testServerAuthenticated(t *testing.T) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	be, s, c, scanner, caps := testServerEhlo(t)

	if _, ok := caps["AUTH PLAIN"]; !ok {
		t.Fatal("AUTH PLAIN capability is missing when auth is enabled")
	}

	io.WriteString(c, "AUTH PLAIN\r\n")
	scanner.Scan()
	if scanner.Text() != "334 " {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	io.WriteString(c, "AHVzZXJuYW1lAHBhc3N3b3Jk\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "235 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	return
}

func TestServerEmptyFrom1(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	return
}

func TestServerEmptyFrom2(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<>\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	return
}

func TestServerBadESMTPVar(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> RABBIT\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	return
}

func TestServerBadSize(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> SIZE=rabbit\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	return
}

func TestServerTooBig(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> SIZE=4294967295\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	return
}

func TestServerEmptyTo(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	io.WriteString(c, "RCPT TO:\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RCPT response:", scanner.Text())
	}

	return
}

func TestServer(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RCPT response:", scanner.Text())
	}

	io.WriteString(c, "DATA\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "354 ") {
		t.Fatal("Invalid DATA response:", scanner.Text())
	}

	io.WriteString(c, "Hey <3\r\n")
	io.WriteString(c, ".\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA response:", scanner.Text())
	}

	if len(be.messages) != 1 || len(be.anonmsgs) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}

	msg := be.messages[0]
	if msg.From != "root@nsa.gov" {
		t.Fatal("Invalid mail sender:", msg.From)
	}
	if len(msg.To) != 1 || msg.To[0] != "root@gchq.gov.uk" {
		t.Fatal("Invalid mail recipients:", msg.To)
	}
	if string(msg.Data) != "Hey <3\n" {
		t.Fatal("Invalid mail data:", string(msg.Data))
	}
}

func TestServer_authDisabled(t *testing.T) {
	_, s, c, scanner, caps := testServerEhlo(t, authDisabled)
	defer s.Close()
	defer c.Close()

	if _, ok := caps["AUTH PLAIN"]; ok {
		t.Fatal("AUTH PLAIN capability is present when auth is disabled")
	}

	io.WriteString(c, "AUTH PLAIN\r\n")
	scanner.Scan()
	if scanner.Text() != "500 Syntax error, AUTH command unrecognized" {
		t.Fatal("Invalid AUTH response with auth disabled:", scanner.Text())
	}
}

func TestServer_otherCommands(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()

	io.WriteString(c, "HELP\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "502 ") {
		t.Fatal("Invalid HELP response:", scanner.Text())
	}

	io.WriteString(c, "VRFY\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "252 ") {
		t.Fatal("Invalid VRFY response:", scanner.Text())
	}

	io.WriteString(c, "NOOP\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid NOOP response:", scanner.Text())
	}

	io.WriteString(c, "RSET\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RSET response:", scanner.Text())
	}

	io.WriteString(c, "QUIT\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "221 ") {
		t.Fatal("Invalid QUIT response:", scanner.Text())
	}
}

func TestServer_tooManyInvalidCommands(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()

	// Let's assume XXXX is a non-existing command
	for i := 0; i < 4; i++ {
		io.WriteString(c, "XXXX\r\n")
		scanner.Scan()
		if !strings.HasPrefix(scanner.Text(), "500 ") {
			t.Fatal("Invalid invalid command response:", scanner.Text())
		}
	}

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "500 ") {
		t.Fatal("Invalid invalid command response:", scanner.Text())
	}
}

func TestServer_tooLongMessage(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()

	s.MaxMessageBytes = 50

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "DATA\r\n")
	scanner.Scan()

	io.WriteString(c, "This is a very long message.\r\n")
	io.WriteString(c, "Much longer than you can possibly imagine.\r\n")
	io.WriteString(c, "And much longer than the server's MaxMessageBytes.\r\n")
	io.WriteString(c, ".\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "552 ") {
		t.Fatal("Invalid DATA response, expected an error but got:", scanner.Text())
	}
}

func TestServer_anonymousUserError(t *testing.T) {
	be, s, c, scanner, _ := testServerEhlo(t)
	defer s.Close()
	defer c.Close()

	be.userErr = smtp.ErrAuthRequired

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	if scanner.Text() != "502 Please authenticate first" {
		t.Fatal("Backend refused anonymous mail but client was permitted:", scanner.Text())
	}
}

func TestServer_anonymousUserOK(t *testing.T) {
	be, s, c, scanner, _ := testServerEhlo(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM: root@nsa.gov\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "DATA\r\n")
	scanner.Scan()
	io.WriteString(c, "Hey <3\r\n")
	io.WriteString(c, ".\r\n")
	scanner.Scan()

	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA response:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}

func testStrictServer(t *testing.T) (s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s = smtp.NewServer(new(backend))
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	s.AuthDisabled = true
	s.Strict = true

	go s.Serve(l)

	c, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	scanner = bufio.NewScanner(c)

	scanner.Scan()
	if scanner.Text() != "220 localhost ESMTP Service Ready" {
		t.Fatal("Invalid greeting:", scanner.Text())
	}

	io.WriteString(c, "EHLO localhost\r\n")

	scanner.Scan()
	if scanner.Text() != "250-Hello localhost" {
		t.Fatal("Invalid EHLO response:", scanner.Text())
	}

	expectedCaps := []string{"PIPELINING", "8BITMIME"}
	caps := make(map[string]bool)

	for scanner.Scan() {
		s := scanner.Text()

		if strings.HasPrefix(s, "250 ") {
			caps[strings.TrimPrefix(s, "250 ")] = true
			break
		} else {
			if !strings.HasPrefix(s, "250-") {
				t.Fatal("Invalid capability response:", s)
			}
			caps[strings.TrimPrefix(s, "250-")] = true
		}
	}

	for _, cap := range expectedCaps {
		if !caps[cap] {
			t.Fatal("Missing capability:", cap)
		}
	}

	return
}

func TestStrictServerGood(t *testing.T) {
	s, c, scanner := testStrictServer(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestStrictServerBad(t *testing.T) {
	s, c, scanner := testStrictServer(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM: root@nsa.gov\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServer_lmtpOK(t *testing.T) {
	be, s, c, scanner := testServerGreeted(t, func(s *smtp.Server) {
		s.LMTP = true
	})
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "LHLO localhost\r\n")

	scanner.Scan()
	if scanner.Text() != "250-Hello localhost" {
		t.Fatal("Invalid LHLO response:", scanner.Text())
	}

	for scanner.Scan() {
		s := scanner.Text()

		if strings.HasPrefix(s, "250 ") {
			break
		} else if !strings.HasPrefix(s, "250-") {
			t.Fatal("Invalid capability response:", s)
		}
	}

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@bnd.bund.de>\r\n")
	scanner.Scan()
	io.WriteString(c, "DATA\r\n")
	scanner.Scan()
	io.WriteString(c, "Hey <3\r\n")
	io.WriteString(c, ".\r\n")
	scanner.Scan()

	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA second response:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}
