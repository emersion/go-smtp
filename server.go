package smtp

import (
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
)

var errTCPAndLMTP = errors.New("smtp: cannot start LMTP server listening on a TCP socket")

// A function that creates SASL servers.
type SaslServerFactory func(conn *Conn) sasl.Server

// Logger interface is used by Server to report unexpected internal errors.
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// A SMTP server.
type Server struct {
	// TCP or Unix address to listen on.
	Addr string
	// The server TLS configuration.
	TLSConfig *tls.Config
	// Enable LMTP mode, as defined in RFC 2033. LMTP mode cannot be used with a
	// TCP listener.
	LMTP bool

	Domain            string
	MaxRecipients     int
	MaxMessageBytes   int
	MaxLineLength     int
	AllowInsecureAuth bool
	Strict            bool
	Debug             io.Writer
	ErrorLog          Logger
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration

	// Advertise SMTPUTF8 (RFC 6531) capability.
	// Should be used only if backend supports it.
	EnableSMTPUTF8 bool

	// Advertise REQUIRETLS (RFC 8689) capability.
	// Should be used only if backend supports it.
	EnableREQUIRETLS bool

	// Advertise BINARYMIME (RFC 3030) capability.
	// Should be used only if backend supports it.
	EnableBINARYMIME bool

	// If set, the AUTH command will not be advertised and authentication
	// attempts will be rejected. This setting overrides AllowInsecureAuth.
	AuthDisabled bool

	// The server backend.
	Backend Backend

	caps  []string
	auths map[string]SaslServerFactory
	done  chan struct{}

	locker    sync.Mutex
	listeners []net.Listener
	conns     map[*Conn]struct{}
}

// New creates a new SMTP server.
func NewServer(be Backend) *Server {
	return &Server{
		// Doubled maximum line length per RFC 5321 (Section 4.5.3.1.6)
		MaxLineLength: 2000,

		Backend:  be,
		done:     make(chan struct{}, 1),
		ErrorLog: log.New(os.Stderr, "smtp/server ", log.LstdFlags),
		caps:     []string{"PIPELINING", "8BITMIME", "ENHANCEDSTATUSCODES", "CHUNKING"},
		auths: map[string]SaslServerFactory{
			sasl.Plain: func(conn *Conn) sasl.Server {
				return sasl.NewPlainServer(func(identity, username, password string) error {
					if identity != "" && identity != username {
						return errors.New("Identities not supported")
					}

					state := conn.State()
					session, err := be.Login(&state, username, password)
					if err != nil {
						return err
					}

					conn.SetSession(session)
					return nil
				})
			},
		},
		conns: make(map[*Conn]struct{}),
	}
}

// Serve accepts incoming connections on the Listener l.
func (s *Server) Serve(l net.Listener) error {
	s.locker.Lock()
	s.listeners = append(s.listeners, l)
	s.locker.Unlock()

	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-s.done:
				// we called Close()
				return nil
			default:
				return err
			}
		}

		go s.handleConn(newConn(c, s))
	}
}

func (s *Server) handleConn(c *Conn) error {
	s.locker.Lock()
	s.conns[c] = struct{}{}
	s.locker.Unlock()

	defer func() {
		c.Close()

		s.locker.Lock()
		delete(s.conns, c)
		s.locker.Unlock()
	}()

	c.greet()

	for {
		line, err := c.ReadLine()
		if err == nil {
			cmd, arg, err := parseCmd(line)
			if err != nil {
				c.protocolError(501, EnhancedCode{5, 5, 2}, "Bad command")
				continue
			}

			c.handle(cmd, arg)
		} else {
			if err == io.EOF {
				return nil
			}
			if err == ErrTooLongLine {
				c.WriteResponse(500, EnhancedCode{5, 4, 0}, "Too long line, closing connection")
				return nil
			}

			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.WriteResponse(221, EnhancedCode{2, 4, 2}, "Idle timeout, bye bye")
				return nil
			}

			c.WriteResponse(221, EnhancedCode{2, 4, 0}, "Connection error, sorry")
			return err
		}
	}
}

// ListenAndServe listens on the network address s.Addr and then calls Serve
// to handle requests on incoming connections.
//
// If s.Addr is blank and LMTP is disabled, ":smtp" is used.
func (s *Server) ListenAndServe() error {
	network := "tcp"
	if s.LMTP {
		network = "unix"
	}

	addr := s.Addr
	if !s.LMTP && addr == "" {
		addr = ":smtp"
	}

	l, err := net.Listen(network, addr)
	if err != nil {
		return err
	}

	return s.Serve(l)
}

// ListenAndServeTLS listens on the TCP network address s.Addr and then calls
// Serve to handle requests on incoming TLS connections.
//
// If s.Addr is blank, ":smtps" is used.
func (s *Server) ListenAndServeTLS() error {
	if s.LMTP {
		return errTCPAndLMTP
	}

	addr := s.Addr
	if addr == "" {
		addr = ":smtps"
	}

	l, err := tls.Listen("tcp", addr, s.TLSConfig)
	if err != nil {
		return err
	}

	return s.Serve(l)
}

// Close immediately closes all active listeners and connections.
//
// Close returns any error returned from closing the server's underlying
// listener(s).
func (s *Server) Close() error {
	select {
	case <-s.done:
		return errors.New("smtp: server already closed")
	default:
		close(s.done)
	}

	var err error
	for _, l := range s.listeners {
		if lerr := l.Close(); lerr != nil && err == nil {
			err = lerr
		}
	}

	s.locker.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.locker.Unlock()

	return err
}

// EnableAuth enables an authentication mechanism on this server.
//
// This function should not be called directly, it must only be used by
// libraries implementing extensions of the SMTP protocol.
func (s *Server) EnableAuth(name string, f SaslServerFactory) {
	s.auths[name] = f
}

// ForEachConn iterates through all opened connections.
func (s *Server) ForEachConn(f func(*Conn)) {
	s.locker.Lock()
	defer s.locker.Unlock()
	for conn := range s.conns {
		f(conn)
	}
}
