// A ESMTP server library.
package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A SMTP server.
type Server struct {
	Backend         Backend
	domain          string
	maxRecipients   int
	maxIdleSeconds  int
	maxMessageBytes int
	maxConns      int
	listener        net.Listener
	timeout         time.Duration
	TLSConfig       *tls.Config
	AllowInsecure   bool
}

// Create a new SMTP server.
func New(l net.Listener, cfg Config, bkd Backend) *Server {
	maxConns := make(chan int, cfg.MaxConns)

	return &Server{
		Backend:         bkd,
		domain:          cfg.Domain,
		maxRecips:       cfg.MaxRecipients,
		maxIdleSeconds:  cfg.MaxIdleSeconds,
		maxMessageBytes: cfg.MaxMessageBytes,
		listener:        l,
		waitgroup:       new(sync.WaitGroup),
		sem:             maxConns,
	}
}

// Listen for incoming connections.
func (s *Server) Listen() error {
	var tempDelay time.Duration
	var ConnId int64

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}

		go s.handleConn(&Conn{
			server: s,
			conn:   conn,
		})
	}
}

func (s *Server) Close() {
	s.listener.Close()
}

func (s *Server) handleConn(c *Conn) error {
	log.LogInfo("SMTP Connection from %v, starting session <%v>", c.conn.RemoteAddr(), c.id)

	defer c.Close()
	c.greet()

	for {
		if c.state == 2 {
			// Special case, does not use SMTP command format
			c.processData()
			continue
		}

		line, err := c.readLine()
		if err == nil {
			if cmd, arg, ok := c.parseCmd(line); ok {
				c.handle(cmd, arg, line)
			}
		} else {
			if err == io.EOF {
				c.logWarn("Got EOF")
				return nil
			}

			c.logWarn("Connection error: %v", err)
			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.Write("221", "Idle timeout, bye bye")
				return nil
			}

			c.Write("221", "Connection error, sorry")
			return err
		}
	}
}
