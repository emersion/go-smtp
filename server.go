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
	// The server backend.
	Backend Backend
	// The server configuration.
	Config *Config
	// The server TLS configuration.
	TLSConfig *tls.Config

	listener net.Listener
	caps     []string
	auths    map[string]SaslServerFactory
}

// Listen for incoming connections.
func (s *Server) Listen() error {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return err
		}

		go s.handleConn(newConn(c, s))
	}
}

func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *Server) Close() {
	s.listener.Close()
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

// Create a new SMTP server.
func New(l net.Listener, cfg *Config, bkd Backend) *Server {
	return &Server{
		Backend:  bkd,
		Config:   cfg,
		listener: l,
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

func Listen(addr string, cfg *Config, bkd Backend) (s *Server, err error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return
	}

	s = New(l, cfg, bkd)
	go s.Listen()
	return
}

func ListenTLS(addr string, cfg *Config, bkd Backend, tlsConfig *tls.Config) (s *Server, err error) {
	l, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return
	}

	s = New(l, cfg, bkd)
	go s.Listen()
	return
}
