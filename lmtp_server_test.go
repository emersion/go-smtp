package smtp_test

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

func sendDeliveryCmdsLMTP(t *testing.T, scanner *bufio.Scanner, c io.Writer) {
	sendLHLO(t, scanner, c)

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
}

func sendLHLO(t *testing.T, scanner *bufio.Scanner, c io.Writer) {
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
}

func TestServer_LMTP(t *testing.T) {
	be, s, c, scanner := testServerGreeted(t, func(s *smtp.Server) {
		s.LMTP = true
		be := s.Backend.(*backend)
		be.implementLMTPData = true
		be.lmtpStatus = []struct {
			addr string
			err  error
		}{
			{"root@gchq.gov.uk", errors.New("nah")},
			{"root@bnd.bund.de", nil},
		}
	})
	defer s.Close()
	defer c.Close()

	sendDeliveryCmdsLMTP(t, scanner, c)

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "554 5.0.0 <root@gchq.gov.uk>") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA second response:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}

func TestServer_LMTP_Early(t *testing.T) {
	// This test confirms responses are sent as early as possible
	// e.g. right after SetStatus is called.

	lmtpStatusSync := make(chan struct{})

	be, s, c, scanner := testServerGreeted(t, func(s *smtp.Server) {
		s.LMTP = true
		be := s.Backend.(*backend)
		be.implementLMTPData = true
		be.lmtpStatusSync = lmtpStatusSync
		be.lmtpStatus = []struct {
			addr string
			err  error
		}{
			{"root@gchq.gov.uk", errors.New("nah")},
			{"root@bnd.bund.de", nil},
		}
	})
	defer s.Close()
	defer c.Close()

	sendDeliveryCmdsLMTP(t, scanner, c)

	// Test backend sends to sync channel after calling SetStatus.

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "554 5.0.0 <root@gchq.gov.uk>") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}

	<-be.lmtpStatusSync

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA second response:", scanner.Text())
	}

	<-be.lmtpStatusSync

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}

func TestServer_LMTP_Expand(t *testing.T) {
	// This checks whether handleDataLMTP
	// correctly expands results if backend doesn't
	// implement LMTPSession.

	be, s, c, scanner := testServerGreeted(t, func(s *smtp.Server) {
		s.LMTP = true
	})
	defer s.Close()
	defer c.Close()

	sendDeliveryCmdsLMTP(t, scanner, c)

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA second response:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}

func TestServer_LMTP_DuplicatedRcpt(t *testing.T) {
	be, s, c, scanner := testServerGreeted(t, func(s *smtp.Server) {
		s.LMTP = true
		be := s.Backend.(*backend)
		be.implementLMTPData = true
		be.lmtpStatus = []struct {
			addr string
			err  error
		}{
			{"root@gchq.gov.uk", &smtp.SMTPError{Code: 555}},
			{"root@bnd.bund.de", nil},
			{"root@gchq.gov.uk", &smtp.SMTPError{Code: 556}},
		}
	})
	defer s.Close()
	defer c.Close()

	sendLHLO(t, scanner, c)

	io.WriteString(c, "MAIL FROM:<root@nsa.gov>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@bnd.bund.de>\r\n")
	scanner.Scan()
	io.WriteString(c, "RCPT TO:<root@gchq.gov.uk>\r\n")
	scanner.Scan()
	io.WriteString(c, "DATA\r\n")
	scanner.Scan()
	io.WriteString(c, "Hey <3\r\n")
	io.WriteString(c, ".\r\n")

	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "555 5.0.0 <root@gchq.gov.uk>") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "250 ") {
		t.Fatal("Invalid DATA second response:", scanner.Text())
	}
	scanner.Scan()
	if !strings.HasPrefix(scanner.Text(), "556 5.0.0 <root@gchq.gov.uk>") {
		t.Fatal("Invalid DATA first response:", scanner.Text())
	}

	if len(be.messages) != 0 || len(be.anonmsgs) != 1 {
		t.Fatal("Invalid number of sent messages:", be.messages, be.anonmsgs)
	}
}
