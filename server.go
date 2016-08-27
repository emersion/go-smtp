// An ESMTP server library.
package smtp

import (
	"crypto/tls"
	"errors"
	"io"
	"net"

	"github.com/emersion/go-sasl"
)

// A function that creates SASL servers.
type SaslServerFactory func(conn *Conn) sasl.Server

// A SMTP server.
type Server struct {
	// TCP address to listen on.
	Addr string
	// The server TLS configuration.
	TLSConfig *tls.Config

	Domain            string
	MaxRecipients     int
	MaxIdleSeconds    int
	MaxMessageBytes   int
	AllowInsecureAuth bool
	Debug             io.Writer

	// The server backend.
	Backend Backend

	listener net.Listener
	caps     []string
	auths    map[string]SaslServerFactory
}

// Create a new SMTP server.
func New(bkd Backend) *Server {
	return &Server{
		Backend:  bkd,
		caps:     []string{"PIPELINING", "8BITMIME"},
		auths: map[string]SaslServerFactory{
			"PLAIN": func(conn *Conn) sasl.Server {
				return sasl.NewPlainServer(func(identity, username, password string) error {
					if identity != "" && identity != username {
						return errors.New("Identities not supported")
					}

					user, err := bkd.Login(username, password)
					if err != nil {
						return err
					}

					conn.User = user
					return nil
				})
			},
		},
	}
}

func (s *Server) Serve(l net.Listener) error {
	s.listener = l
	defer s.Close()

	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}

		go s.handleConn(newConn(c, s))
	}
}

func (s *Server) handleConn(c *Conn) error {
	defer c.Close()
	c.greet()

	for {
		line, err := c.readLine()
		if err == nil {
			cmd, arg, err := parseCmd(line)
			if err != nil {
				c.nbrErrors++
				c.Write("501", "Bad command")
				continue
			}

			c.handle(cmd, arg)
		} else {
			if err == io.EOF {
				return nil
			}

			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.Write("221", "Idle timeout, bye bye")
				return nil
			}

			c.Write("221", "Connection error, sorry")
			return err
		}
	}
}

// ListenAndServe listens on the TCP network address s.Addr and then calls Serve
// to handle requests on incoming connections.
//
// If s.Addr is blank, ":smtp" is used.
func (s *Server) ListenAndServe() error {
	addr := s.Addr
	if addr == "" {
		addr = ":smtp"
	}

	l, err := net.Listen("tcp", addr)
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

func (s *Server) Close() {
	// TODO: say bye to all clients

	s.listener.Close()
}
