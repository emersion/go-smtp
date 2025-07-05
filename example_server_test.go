package smtp_test

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// The Backend implements SMTP server methods.
type Backend struct{}

// NewSession is called after client greeting (EHLO, HELO).
func (bkd *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{}, nil
}

// A Session is returned after successful login.
type Session struct {
	auth bool
}

// AuthMechanisms returns a slice of available auth mechanisms; only PLAIN is
// supported in this example.
func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

// Auth is the handler for supported authenticators.
func (s *Session) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		if username != "username" || password != "password" {
			return errors.New("Invalid username or password")
		}
		s.auth = true
		return nil
	}), nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	log.Println("Mail from:", from)
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	log.Println("Rcpt to:", to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	if b, err := io.ReadAll(r); err != nil {
		return err
	} else {
		log.Println("Data:", string(b))
	}
	return nil
}

func (s *Session) Reset() {}

func (s *Session) Logout() error {
	return nil
}

// ExampleServer runs an example SMTP server.
//
// It can be tested manually with e.g. netcat:
//
//	> netcat -C localhost 1025
//	EHLO localhost
//	AUTH PLAIN
//	AHVzZXJuYW1lAHBhc3N3b3Jk
//	MAIL FROM:<root@nsa.gov>
//	RCPT TO:<root@gchq.gov.uk>
//	DATA
//	Hey <3
//	.
func ExampleServer() {
	be := &Backend{}

	s := smtp.NewServer(be)

	s.Addr = "localhost:1025"
	s.Domain = "localhost"
	s.WriteTimeout = 10 * time.Second
	s.ReadTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	log.Println("Starting server at", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ExampleServer_connState demonstrates how to use ConnState for monitoring
// connection lifecycle events.
func ExampleServer_connState() {
	be := &Backend{}
	s := smtp.NewServer(be)
	
	// Track active connections
	var mu sync.Mutex
	activeConns := make(map[net.Conn]time.Time)
	
	s.ConnState = func(conn net.Conn, state smtp.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		
		switch state {
		case smtp.StateNew:
			log.Printf("[%s] New connection", conn.RemoteAddr())
			activeConns[conn] = time.Now()
			
		case smtp.StateActive:
			log.Printf("[%s] Connection active", conn.RemoteAddr())
			
		case smtp.StateAuth:
			log.Printf("[%s] Authentication started", conn.RemoteAddr())
			
		case smtp.StateData:
			log.Printf("[%s] Receiving message data", conn.RemoteAddr())
			
		case smtp.StateError:
			log.Printf("[%s] Connection error", conn.RemoteAddr())
			
		case smtp.StateClosed:
			if startTime, ok := activeConns[conn]; ok {
				duration := time.Since(startTime)
				log.Printf("[%s] Connection closed (duration: %v)", conn.RemoteAddr(), duration)
				delete(activeConns, conn)
			}
		}
		
		log.Printf("Active connections: %d", len(activeConns))
	}
	
	s.Addr = "localhost:1025"
	s.Domain = "localhost"
	
	log.Println("Starting server with connection monitoring")
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
