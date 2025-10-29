package smtp_test

import (
	"fmt"
	"io"
	"log"

	"github.com/emersion/go-smtp"
)

// Backend that implements XCLIENT support
type XCLIENTBackend struct{}

func (b *XCLIENTBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &XCLIENTSession{conn: c}, nil
}

type XCLIENTSession struct {
	conn *smtp.Conn
}

func (s *XCLIENTSession) Mail(from string, opts *smtp.MailOptions) error {
	// Access XCLIENT data to get real client information
	xclientData := s.conn.XCLIENTData()
	if xclientData != nil {
		if realAddr, ok := xclientData["ADDR"]; ok && realAddr != "[UNAVAILABLE]" {
			log.Printf("Mail from %s via proxy, real client: %s", from, realAddr)
		}
		if realHelo, ok := xclientData["HELO"]; ok && realHelo != "[UNAVAILABLE]" {
			log.Printf("Real HELO: %s", realHelo)
		}
	}
	return nil
}

func (s *XCLIENTSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	return nil
}

func (s *XCLIENTSession) Data(r io.Reader) error {
	return nil
}

func (s *XCLIENTSession) Reset() {}

func (s *XCLIENTSession) Logout() error {
	return nil
}

// Implement XCLIENT backend interface
func (s *XCLIENTSession) XCLIENT(session smtp.Session, attrs map[string]string) error {
	// Validate and process XCLIENT attributes
	for name, value := range attrs {
		switch name {
		case "ADDR":
			if value != "[UNAVAILABLE]" && value != "[TEMPUNAVAIL]" {
				log.Printf("Client connected via proxy from real IP: %s", value)
			}
		case "HELO":
			if value != "[UNAVAILABLE]" && value != "[TEMPUNAVAIL]" {
				log.Printf("Real client HELO: %s", value)
			}
		case "LOGIN":
			if value != "[UNAVAILABLE]" && value != "[TEMPUNAVAIL]" {
				log.Printf("Client authenticated as: %s", value)
			}
		}
	}
	return nil
}

func ExampleServer_xclient() {
	be := &XCLIENTBackend{}

	s := smtp.NewServer(be)
	s.Addr = ":2525"
	s.Domain = "localhost"
	s.EnableXCLIENT = true

	// Add trusted networks for XCLIENT
	err := s.AddXCLIENTTrustedNetwork("127.0.0.0/8")
	if err != nil {
		log.Fatal(err)
	}
	err = s.AddXCLIENTTrustedNetwork("192.168.0.0/16")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("XCLIENT server configured")
	// Output: XCLIENT server configured
}
