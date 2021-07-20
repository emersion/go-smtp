package backendutil_test

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
	"github.com/emersion/go-smtp/backendutil"
)

var _ smtp.Backend = &backendutil.TransformBackend{}

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

func (be *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &session{backend: be, anonymous: true}, nil
}

type session struct {
	backend   *backend
	anonymous bool

	msg *message
}

func (s *session) Reset() {
	s.msg = &message{}
}

func (s *session) Logout() error {
	return nil
}

func (s *session) AuthPlain(username, password string) error {
	if username != "username" || password != "password" {
		return errors.New("Invalid username or password")
	}
	s.anonymous = false
	return nil
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	if s.backend.userErr != nil {
		return s.backend.userErr
	}
	s.Reset()
	s.msg.From = from
	return nil
}

func (s *session) Rcpt(to string) error {
	s.msg.To = append(s.msg.To, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	if b, err := ioutil.ReadAll(r); err != nil {
		return err
	} else {
		s.msg.Data = b
		if s.anonymous {
			s.backend.anonmsgs = append(s.backend.anonmsgs, s.msg)
		} else {
			s.backend.messages = append(s.backend.messages, s.msg)
		}
	}
	return nil
}

type serverConfigureFunc func(*smtp.Server)

func transformMailString(s string) (string, error) {
	s = base64.StdEncoding.EncodeToString([]byte(s))
	return s, nil
}

func transformMailReader(r io.Reader) (io.Reader, error) {
	pr, pw := io.Pipe()
	w := base64.NewEncoder(base64.StdEncoding, pw)
	go copyAndClose(w, r, func(err error) { pw.CloseWithError(err) })
	return pr, nil
}

func copyAndClose(w io.WriteCloser, r io.Reader, done func(err error)) {
	_, err := io.Copy(w, r)
	w.Close()
	done(err)
}

func testServer(t *testing.T, fn ...serverConfigureFunc) (be *backend, s *smtp.Server, c net.Conn, scanner *bufio.Scanner) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	be = new(backend)
	tbe := &backendutil.TransformBackend{
		Backend:       be,
		TransformMail: transformMailString,
		TransformRcpt: transformMailString,
		TransformData: transformMailReader,
	}
	s = smtp.NewServer(tbe)
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
	// base64 of "root@nsa.gov"
	if msg.From != "cm9vdEBuc2EuZ292" {
		t.Fatal("Invalid mail sender:", msg.From)
	}
	// base64 of "root@gchq.gov.uk"
	if len(msg.To) != 1 || msg.To[0] != "cm9vdEBnY2hxLmdvdi51aw==" {
		t.Fatal("Invalid mail recipients:", msg.To)
	}
	// base64 of "Hey <3\n" (with actual newline)
	if string(msg.Data) != "SGV5IDwzDQo=" {
		t.Fatal("Invalid mail data:", string(msg.Data))
	}
}

func TestServer_tooLongMessage(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
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

	if len(be.messages) != 0 || len(be.anonmsgs) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
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

	msg := be.anonmsgs[0]
	// base64 of "root@nsa.gov"
	if msg.From != "cm9vdEBuc2EuZ292" {
		t.Fatal("Invalid mail sender:", msg.From)
	}
	// base64 of "root@gchq.gov.uk"
	if len(msg.To) != 1 || msg.To[0] != "cm9vdEBnY2hxLmdvdi51aw==" {
		t.Fatal("Invalid mail recipients:", msg.To)
	}
	// base64 of "Hey <3\r\n" (with actual newline)
	if string(msg.Data) != "SGV5IDwzDQo=" {
		t.Fatal("Invalid mail data:", string(msg.Data))
	}
}
