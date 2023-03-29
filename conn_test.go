package smtp

import (
	"io"
	"net/textproto"
	"testing"
)

type dummyBackend struct {
	mailFrom string
	opts     MailOptions
}

func (d *dummyBackend) Reset() {}

func (d *dummyBackend) Logout() error {
	return nil
}

func (d *dummyBackend) AuthPlain(username, password string) error {
	return nil
}

func (d *dummyBackend) Mail(from string, opts *MailOptions) error {
	d.mailFrom = from
	d.opts = *opts
	return nil
}

func (d *dummyBackend) Rcpt(to string) error {
	return nil
}

func (d *dummyBackend) Data(r io.Reader) error {
	return nil
}

type testReadWriter struct {
	io.Reader
	io.Writer
	io.Closer
	out [][]byte
}

func (c *testReadWriter) Write(p []byte) (n int, err error) {
	c.out = append(c.out, p)
	return len(p), nil
}

func (c *testReadWriter) Flush() (err error) {
	return nil
}

func newTestConn() (con Conn, tester *testReadWriter) {
	tester = &testReadWriter{}
	con.text = textproto.NewConn(tester)
	con.server = &Server{}
	db := &dummyBackend{}
	con.session = db
	con.helo = "helo"
	return
}

func TestHandleEmptyFrom(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	ret := string(tester.out[0])
	if ret != "501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n, got %s", ret)
	}
}

func TestHandleEmptyValidFrom(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n, got %s", ret)
	}
}

func TestHandleFromServerTest(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM: root@nsa.gov AUTH=<hey+41>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if *con.session.(*dummyBackend).opts.Auth != "heyA" {
		t.Errorf("Expected heyA, got %s", *con.session.(*dummyBackend).opts.Auth)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <root@nsa.gov>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n, got %s", ret)
	}
}

func TestHandleFromServerTestAuthShort(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM: root@nsa.gov AUTH=<hey+A>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	ret := string(tester.out[0])
	if ret != "500 5.5.4 Malformed AUTH parameter value\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n, got %s", ret)
	}
}

func TestHandleSimpleFrom(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:test@bla.de")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).mailFrom != "test@bla.de" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <test@bla.de>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
}

func TestHandleSimpleSharpFrom(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<test@bla.de>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).mailFrom != "test@bla.de" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <test@bla.de>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
}

func TestHandleNaturalFrom(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<Test Name <test@bla.de>>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).mailFrom != "Test Name <test@bla.de>" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <Test Name <test@bla.de>>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
}

func TestHandleNaturalFromDefect(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<Test Name <test@bla.de>")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).mailFrom != "" {
		t.Errorf("Expected '', got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address> got %s", ret)
	}
}

func TestHandleEmptyFromOptions(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM: BODY=8BITMIME SIZE=12345")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <BODY=8BITMIME>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n, got %s", ret)
	}
}

func TestHandleSimpleFromOptions(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:test@bla.de BODY=8BITMIME SIZE=12345")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <test@bla.de>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
	if con.session.(*dummyBackend).opts.Body != "8BITMIME" {
		t.Errorf("Expected 8BITMIME, got %s", con.session.(*dummyBackend).opts.Body)
	}
	if con.session.(*dummyBackend).opts.Size != 12345 {
		t.Errorf("Expected 12345, got %d", con.session.(*dummyBackend).opts.Size)
	}
	if con.session.(*dummyBackend).mailFrom != "test@bla.de" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
}

func TestHandleSimpleSharpFromOptions(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<test@bla.de> BODY=8BITMIME SIZE=12345")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).opts.Body != "8BITMIME" {
		t.Errorf("Expected 8BITMIME, got %s", con.session.(*dummyBackend).opts.Body)
	}
	if con.session.(*dummyBackend).opts.Size != 12345 {
		t.Errorf("Expected 12345, got %d", con.session.(*dummyBackend).opts.Size)
	}
	if con.session.(*dummyBackend).mailFrom != "test@bla.de" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <test@bla.de>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
}

func TestHandleNaturalFromOptions(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<Test Name <test@bla.de>>  BODY=8BITMIME SIZE=12345")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).opts.Body != "8BITMIME" {
		t.Errorf("Expected 8BITMIME, got %s", con.session.(*dummyBackend).opts.Body)
	}
	if con.session.(*dummyBackend).opts.Size != 12345 {
		t.Errorf("Expected 12345, got %d", con.session.(*dummyBackend).opts.Size)
	}
	if con.session.(*dummyBackend).mailFrom != "Test Name <test@bla.de>" {
		t.Errorf("Expected test@bla.de, got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "250 2.0.0 Roger, accepting mail from <Test Name <test@bla.de>>\r\n" {
		t.Errorf("Expected 250 2.0.0 Roger, accepting mail from <test@bla> got %s", ret)
	}
}

func TestHandleNaturalFromDefectOptions(t *testing.T) {
	con, tester := newTestConn()
	con.handleMail("FROM:<Test Name <test@bla.de> BODY=8BITMIME SIZE=12345")
	if len(tester.out) != 1 {
		t.Errorf("Expected 1 output, got %d", len(tester.out))
	}
	if con.session.(*dummyBackend).mailFrom != "" {
		t.Errorf("Expected '', got %s", con.session.(*dummyBackend).mailFrom)
	}
	ret := string(tester.out[0])
	if ret != "501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n" {
		t.Errorf("Expected 501 5.5.2 Was expecting MAIL arg syntax of FROM:<address> got %s", ret)
	}
}
