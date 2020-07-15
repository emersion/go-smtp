package smtp_test

import (
	"io"
	"strings"
	"testing"
)

func TestServer_XCLIENT(t *testing.T) {
	be, s, c, scanner := testServerAuthenticated(t)
	defer s.Close()
	defer c.Close()

	io.WriteString(c, "XCLIENT ADDR=127.0.0.1 PORT=2222 HELO=liar.test\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "550 ") {
		t.Fatal("Invalid XCLIENT response:", scanner.Text())
	}

	be.allowProxy = true
	be.allowProxySession = true

	io.WriteString(c, "XCLIENT HELO=not-liar.test\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "220 ") {
		t.Fatal("Invalid XCLIENT response:", scanner.Text())
	}
	io.WriteString(c, "XCLIENT ADDR=127.0.0.1 PORT=2222 HELO=liar.test\r\n")
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "220 ") {
		t.Fatal("Invalid XCLIENT response:", scanner.Text())
	}

	io.WriteString(c, "EHLO localhost\r\n")
	scanner.Scan()
	if scanner.Text() != "250-Hello localhost" {
		t.Fatal("Invalid EHLO response:", scanner.Text())
	}
	for scanner.Scan() {
		s := scanner.Text()
		if strings.HasPrefix(s, "250 ") {
			break
		}
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

	if len(be.anonmsgs) != 1 || len(be.messages) != 0 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}

	if be.anonmsgs[0].ConnState.Hostname != "liar.test" {
		t.Fatal("Wrong HELO hostname:", be.anonmsgs[0].ConnState.Hostname)
	}
}
