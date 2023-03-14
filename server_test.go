package smtp_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

type message struct {
	From string
	To   []string
	Data []byte
	Opts *smtp.MailOptions
}

type backend struct {
	messages []*message
	anonmsgs []*message

	implementLMTPData bool
	lmtpStatus        []struct {
		addr string
		err  error
	}
	lmtpStatusSync chan struct{}

	// Errors returned by Data method.
	dataErrors chan error

	// Error that will be returned by Data method.
	dataErr error

	// Read N bytes of message before returning dataErr.
	dataErrOffset int64

	panicOnMail bool
	userErr     error
}

func (be *backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	if be.implementLMTPData {
		return &lmtpSession{&session{backend: be, anonymous: true}}, nil
	}

	return &session{backend: be, anonymous: true}, nil
}

type lmtpSession struct {
	*session
}

type session struct {
	backend   *backend
	anonymous bool

	msg *message
}

func (s *session) AuthPlain(username, password string) error {
	if username != "username" || password != "password" {
		return errors.New("Invalid username or password")
	}
	s.anonymous = false
	return nil
}

func (s *session) Reset() {
	s.msg = &message{}
}

func (s *session) Logout() error {
	return nil
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	if s.backend.userErr != nil {
		return s.backend.userErr
	}
	if s.backend.panicOnMail {
		panic("Everything is on fire!")
	}
	s.Reset()
	s.msg.From = from
	s.msg.Opts = opts
	return nil
}

func (s *session) Rcpt(to string) error {
	s.msg.To = append(s.msg.To, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.backend.dataErr != nil {

		if s.backend.dataErrOffset != 0 {
			io.CopyN(ioutil.Discard, r, s.backend.dataErrOffset)
		}

		err := s.backend.dataErr
		if s.backend.dataErrors != nil {
			s.backend.dataErrors <- err
		}
		return err
	}

	if b, err := ioutil.ReadAll(r); err != nil {
		if s.backend.dataErrors != nil {
			s.backend.dataErrors <- err
		}
		return err
	} else {
		s.msg.Data = b
		if s.anonymous {
			s.backend.anonmsgs = append(s.backend.anonmsgs, s.msg)
		} else {
			s.backend.messages = append(s.backend.messages, s.msg)
		}
		if s.backend.dataErrors != nil {
			s.backend.dataErrors <- nil
		}
	}
	return nil
}

func (s *session) LMTPData(r io.Reader, collector smtp.StatusCollector) error {
	if err := s.Data(r); err != nil {
		return err
	}

	for _, val := range s.backend.lmtpStatus {
		collector.SetStatus(val.addr, val.err)

		if s.backend.lmtpStatusSync != nil {
			s.backend.lmtpStatusSync <- struct{}{}
		}
	}

	return nil
}

type failingListener struct {
	c      chan error
	closed bool
}

func newFailingListener() *failingListener {
	return &failingListener{c: make(chan error)}
}

func (l *failingListener) Send(err error) {
	if !l.closed {
		l.c <- err
	}
}

func (l *failingListener) Accept() (net.Conn, error) {
	return nil, <-l.c
}

func (l *failingListener) Close() error {
	if !l.closed {
		close(l.c)
		l.closed = true
	}
	return nil
}

func (l *failingListener) Addr() net.Addr {
	return &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 12345,
	}
}

type mockError struct {
	msg       string
	temporary bool
}

func newMockError(msg string, temporary bool) *mockError {
	return &mockError{
		msg:       msg,
		temporary: temporary,
	}
}

func (m *mockError) Error() string   { return m.msg }
func (m *mockError) String() string  { return m.msg }
func (m *mockError) Timeout() bool   { return false }
func (m *mockError) Temporary() bool { return m.temporary }

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

func TestServerAcceptErrorHandling(t *testing.T) {
	errorLog := bytes.NewBuffer(nil)
	be := new(backend)
	s := smtp.NewServer(be)
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	s.ErrorLog = log.New(errorLog, "", 0)

	l := newFailingListener()
	var serveError error
	go func() {
		serveError = s.Serve(l)
		l.Close()
	}()

	temporaryError := newMockError("temporary mock error", true)
	l.Send(temporaryError)
	permanentError := newMockError("permanent mock error", false)
	l.Send(permanentError)
	s.Close()

	if serveError == nil {
		t.Fatal("Serve had exited without an expected error")
	} else if serveError != permanentError {
		t.Fatal("Unexpected error:", serveError)
	}
	if !strings.Contains(errorLog.String(), temporaryError.String()) {
		t.Fatal("Missing temporary error in log output:", errorLog.String())
	}
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

func TestServerAuthTwice(t *testing.T) {
	_, _, c, scanner, caps := testServerEhlo(t)

	if _, ok := caps["AUTH PLAIN"]; !ok {
		t.Fatal("AUTH PLAIN capability is missing when auth is enabled")
	}

	io.WriteString(c, "AUTH PLAIN AHVzZXJuYW1lAHBhc3N3b3Jk\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "235 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	io.WriteString(c, "AUTH PLAIN AHVzZXJuYW1lAHBhc3N3b3Jk\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "503 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	io.WriteString(c, "RSET\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	io.WriteString(c, "AUTH PLAIN AHVzZXJuYW1lAHBhc3N3b3Jk\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "503 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}
}

func TestServerCancelSASL(t *testing.T) {
	_, _, c, scanner, caps := testServerEhlo(t)

	if _, ok := caps["AUTH PLAIN"]; !ok {
		t.Fatal("AUTH PLAIN capability is missing when auth is enabled")
	}

	io.WriteString(c, "AUTH PLAIN\r\n")
	scanner.Scan()
	if scanner.Text() != "334 " {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}

	io.WriteString(c, "*\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "501 ") {
		t.Fatal("Invalid AUTH response:", scanner.Text())
	}
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
}

func TestServerEmptyFrom2(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServerPanicRecover(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	s.Backend.(*backend).panicOnMail = true
	// Don't log panic in tests to not confuse people who run 'go test'.
	s.ErrorLog = log.New(ioutil.Discard, "", 0)

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "421 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServerSMTPUTF8(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	s.EnableSMTPUTF8 = true
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> SMTPUTF8\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServerSMTPUTF8_Disabled(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> SMTPUTF8\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServer8BITMIME(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> BODY=8BITMIME\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServer_BODYInvalidValue(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> BODY=RABIIT\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
}

func TestServerUnknownArg(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<alice@wonderland.book> RABIIT\r\n")
	scanner.Scan()
	if strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}
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

	io.WriteString(c, "From: root@nsa.gov\r\n")
	io.WriteString(c, "\r\n")
	io.WriteString(c, "Hey\r <3\r\n")
	io.WriteString(c, "..this dot is fine\r\n")
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
	if string(msg.Data) != "From: root@nsa.gov\r\n\r\nHey\r <3\r\n.this dot is fine\r\n" {
		t.Fatal("Invalid mail data:", string(msg.Data))
	}
}

func TestServer_LFDotLF(t *testing.T) {
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

	io.WriteString(c, "From: root@nsa.gov\r\n")
	io.WriteString(c, "\r\n")
	io.WriteString(c, "hey\r\n")
	io.WriteString(c, "\n.\n")
	io.WriteString(c, "this is going to break your server\r\n")
	io.WriteString(c, ".\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA response:", scanner.Text())
	}

	if len(be.messages) != 1 || len(be.anonmsgs) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}

	msg := be.messages[0]
	if string(msg.Data) != "From: root@nsa.gov\r\n\r\nhey\r\n\n.\nthis is going to break your server\r\n" {
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
	if scanner.Text() != "500 5.5.2 Syntax error, AUTH command unrecognized" {
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

func TestServer_tooLongLine(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()

	io.WriteString(c, "MAIL FROM:<root@nsa.gov> "+strings.Repeat("A", 2000))
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "500 ") {
		t.Fatal("Invalid response, expected an error but got:", scanner.Text())
	}
}

func TestServer_anonymousUserError(t *testing.T) {
	be, s, c, scanner, _ := testServerEhlo(t)
	defer s.Close()
	defer c.Close()

	be.userErr = smtp.ErrAuthRequired

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	if scanner.Text() != "502 5.7.0 Please authenticate first" {
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

func TestServer_authParam(t *testing.T) {
	be, s, c, scanner, _ := testServerEhlo(t)
	defer s.Close()
	defer c.Close()

	// Invalid HEXCHAR
	io.WriteString(c, "MAIL FROM: root@nsa.gov AUTH=<hey+A>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "500 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	// Invalid HEXCHAR
	io.WriteString(c, "MAIL FROM: root@nsa.gov AUTH=<he+YYa>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "500 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	// https://tools.ietf.org/html/rfc4954#section-4
	// >servers that advertise support for this
	// >extension MUST support the AUTH parameter to the MAIL FROM
	// >command even when the client has not authenticated itself to the
	// >server.
	io.WriteString(c, "MAIL FROM: root@nsa.gov AUTH=<hey+3Da>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	// Go on as usual.
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
	if val := be.anonmsgs[0].Opts.Auth; val == nil || *val != "hey=a" {
		t.Fatal("Invalid Auth value:", val)
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

func TestServer_Chunking(t *testing.T) {
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

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	io.WriteString(c, "BDAT 8 LAST\r\n")
	io.WriteString(c, "Hey :3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
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
	if want := "Hey <3\r\nHey :3\r\n"; string(msg.Data) != want {
		t.Fatal("Invalid mail data:", string(msg.Data), msg.Data)
	}
}

func TestServer_Chunking_LMTP(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	s.LMTP = true
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
	io.WriteString(c, "RCPT TO:<toor@gchq.gov.uk>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RCPT response:", scanner.Text())
	}

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	io.WriteString(c, "BDAT 8 LAST\r\n")
	io.WriteString(c, "Hey :3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	if len(be.messages) != 1 || len(be.anonmsgs) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}

	msg := be.messages[0]
	if msg.From != "root@nsa.gov" {
		t.Fatal("Invalid mail sender:", msg.From)
	}
	if want := "Hey <3\r\nHey :3\r\n"; string(msg.Data) != want {
		t.Fatal("Invalid mail data:", string(msg.Data), msg.Data)
	}
}

func TestServer_Chunking_Reset(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()
	be.dataErrors = make(chan error, 10)

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

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	// Client changed its mind... Note, in this case Data method error is discarded and not returned to the cilent.
	io.WriteString(c, "RSET\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	if err := <-be.dataErrors; err != smtp.ErrDataReset {
		t.Fatal("Backend received a different error:", err)
	}
}

func TestServer_Chunking_ClosedInTheMiddle(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()
	be.dataErrors = make(chan error, 10)

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

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <")

	// Bye!
	c.Close()

	if err := <-be.dataErrors; err != smtp.ErrDataReset {
		t.Fatal("Backend received a different error:", err)
	}
}

func TestServer_Chunking_EarlyError(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	be.dataErr = &smtp.SMTPError{
		Code:         555,
		EnhancedCode: smtp.EnhancedCode{5, 0, 0},
		Message:      "I failed",
	}

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

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "555 5.0.0 I failed") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}
}

func TestServer_Chunking_EarlyErrorDuringChunk(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	be.dataErr = &smtp.SMTPError{
		Code:         555,
		EnhancedCode: smtp.EnhancedCode{5, 0, 0},
		Message:      "I failed",
	}
	be.dataErrOffset = 5

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

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "555 5.0.0 I failed") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	// See that command stream state is not corrupted e.g. server is still not
	// waiting for remaining chunk octets.
	io.WriteString(c, "NOOP\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RCPT response:", scanner.Text())
	}
}

func TestServer_Chunking_tooLongMessage(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()

	s.MaxMessageBytes = 50

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "BDAT 30\r\n")
	io.WriteString(c, "This is a very long message.\r\n")
	scanner.Scan()

	io.WriteString(c, "BDAT 96 LAST\r\n")
	io.WriteString(c, "Much longer than you can possibly imagine.\r\n")
	io.WriteString(c, "And much longer than the server's MaxMessageBytes.\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "552 ") {
		t.Fatal("Invalid DATA response, expected an error but got:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}

func TestServer_Chunking_Binarymime(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()
	s.EnableBINARYMIME = true

	io.WriteString(c, "MAIL FROM:<root@nsa.gov> BODY=BINARYMIME\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid MAIL response:", scanner.Text())
	}

	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid RCPT response:", scanner.Text())
	}

	io.WriteString(c, "BDAT 8\r\n")
	io.WriteString(c, "Hey <3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
	}

	io.WriteString(c, "BDAT 8 LAST\r\n")
	io.WriteString(c, "Hey :3\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid BDAT response:", scanner.Text())
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
	if want := "Hey <3\r\nHey :3\r\n"; string(msg.Data) != want {
		t.Fatal("Invalid mail data:", string(msg.Data), msg.Data)
	}
}

func TestServer_TooLongCommand(t *testing.T) {
	_, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "MAIL FROM:<"+strings.Repeat("a", s.MaxLineLength)+">\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "500 5.4.0 ") {
		t.Fatal("Invalid too long MAIL response:", scanner.Text())
	}
}

func TestServerShutdown(t *testing.T) {
	_, s, c, _ := testServerGreeted(t)

	ctx := context.Background()
	errChan := make(chan error)
	go func() {
		defer close(errChan)

		errChan <- s.Shutdown(ctx)
		errChan <- s.Shutdown(ctx)
	}()

	select {
	case err := <-errChan:
		t.Fatal("Expected no err because conn is open:", err)
	default:
		c.Close()
	}

	errOne := <-errChan
	if errOne != nil {
		t.Fatal("Expected err to be nil:", errOne)
	}

	errTwo := <-errChan
	if errTwo != smtp.ErrServerClosed {
		t.Fatal("Expected err to be ErrServerClosed:", errTwo)
	}
}
