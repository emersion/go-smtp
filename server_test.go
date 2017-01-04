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
}

func (be *backend) Login(username, password string) (smtp.User, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &user{be}, nil
}

type user struct {
	backend *backend
}

func (u *user) Send(from string, to []string, r io.Reader) error {
	if b, err := ioutil.ReadAll(r); err != nil {
		return err
	} else {
		u.backend.messages = append(u.backend.messages, &message{
			From: from,
			To:   to,
			Data: b,
		})
	}
	return nil
}

func (u *user) Logout() error {
	return nil
}

func testServer(t *testing.T) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	be = new(backend)
	s = smtp.NewServer(be)
	s.Domain = "localhost"
	s.AllowInsecureAuth = true

	go s.Serve(l)

	c, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	scanner = bufio.NewScanner(c)
	return
}

func testServerGreeted(t *testing.T) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	be, s, c, scanner = testServer(t)

	scanner.Scan()
	if scanner.Text() != "220 localhost ESMTP Service Ready" {
		t.Fatal("Invalid greeting:", scanner.Text())
	}

	return
}

func testServerEhlo(t *testing.T) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	be, s, c, scanner = testServerGreeted(t)

	io.WriteString(c, "EHLO localhost\r\n")

	scanner.Scan()
	if scanner.Text() != "250-Hello localhost" {
		t.Fatal("Invalid EHLO response:", scanner.Text())
	}

	expectedCaps := []string{"PIPELINING", "8BITMIME", "AUTH PLAIN"}
	caps := map[string]bool{}

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
	be, s, c, scanner = testServerEhlo(t)

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

	if len(be.messages) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages)
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
