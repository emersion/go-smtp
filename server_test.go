package smtp_test

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"strings"
	"testing"

	smtpserver "github.com/emersion/go-smtp-server"
)

type backend struct {
	messages []*smtpserver.Message
	data [][]byte
}

func (be *backend) Login(username, password string) (smtpserver.User, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &user{be}, nil
}

type user struct {
	backend *backend
}

func (u *user) Send(msg *smtpserver.Message) error {
	u.backend.messages = append(u.backend.messages, msg)
	if b, err := ioutil.ReadAll(msg.Data); err != nil {
		return err
	} else {
		u.backend.data = append(u.backend.data, b)
	}
	return nil
}

func (u *user) Logout() error {
	return nil
}

func testServer(t *testing.T) (be *backend, s *smtpserver.Server, c net.Conn, scanner *bufio.Scanner) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	be = &backend{}

	s = smtpserver.NewServer(be)
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

func testServerGreeted(t *testing.T) (be *backend, s *smtpserver.Server, c net.Conn, scanner *bufio.Scanner) {
	be, s, c, scanner = testServer(t)

	scanner.Scan()
	if scanner.Text() != "220 localhost ESMTP Service Ready" {
		t.Fatal("Invalid greeting:", scanner.Text())
	}

	return
}

func testServerEhlo(t *testing.T) (be *backend, s *smtpserver.Server, c net.Conn, scanner *bufio.Scanner) {
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

func testServerAuthenticated(t *testing.T) (be *backend, s *smtpserver.Server, c net.Conn, scanner *bufio.Scanner) {
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

	data := be.data[0]
	if string(data) != "Hey <3\n" {
		t.Fatal("Invalid mail data:", string(data))
	}
}
