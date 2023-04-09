package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/emersion/go-sasl"
)

var (
	errTCPAndLMTP   = errors.New("smtp: cannot start LMTP server listening on a TCP socket")
	ErrServerClosed = errors.New("smtp: server already closed")
)

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

	// Run as the other user.
	Username string
	// Additionally, change the group.
	Groupname string
	// The file mode of UNIX domain socket.
	SocketMode int

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

	wg sync.WaitGroup

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

					sess := conn.Session()
					if sess == nil {
						panic("No session when AUTH is called")
					}

					return sess.AuthPlain(username, password)
				})
			},
		},
		conns: make(map[*Conn]struct{}),
	}
}

func (s *Server) Listen(network, addr string) (net.Listener, error) {
	var (
		l   net.Listener
		err error
	)
	if mode := s.SocketMode; network == "unix" && mode > 0 {
		origUmask := syscall.Umask(-1 &^ mode)
		l, err = net.Listen(network, addr)
		syscall.Umask(origUmask)
		if err != nil {
			return nil, err
		}
		if err = os.Chmod(addr, os.FileMode(mode)); err != nil {
			return nil, err
		}
	} else {
		l, err = net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
	}

	if s.Username != "" {
		var userUser *user.User

		var uid int
		_, err = strconv.Atoi(s.Username)
		if err == nil {
			userUser, err = user.LookupId(s.Username)
		}
		if err != nil {
			userUser, err = user.Lookup(s.Username)
		}
		if err != nil {
			return nil, err
		}
		uid, err = strconv.Atoi(userUser.Uid)
		if err != nil {
			return nil, err
		}

		var gids []int
		if os.Getuid() == 0 {
			groups, err := userUser.GroupIds()
			if err != nil {
				return nil, err
			}
			for _, g := range groups {
				id, err := strconv.Atoi(g)
				if err != nil {
					return nil, err
				}
				gids = append(gids, id)
			}
		}

		var gid int
		if s.Groupname != "" {
			var userGroup *user.Group

			_, err = strconv.Atoi(s.Groupname)
			if err == nil {
				userGroup, err = user.LookupGroupId(s.Groupname)
			}
			if err != nil {
				userGroup, err = user.LookupGroup(s.Groupname)
			}
			if err != nil {
				return nil, err
			}
			gid, err = strconv.Atoi(userGroup.Gid)
		} else {
			gid, err = strconv.Atoi(userUser.Gid)
		}
		if err != nil {
			return nil, err
		}

		if network == "unix" {
			if err := os.Chown(addr, uid, gid); err != nil {
				return nil, err
			}
		}
		if err := syscall.Setgid(gid); err != nil {
			return nil, err
		}
		if gids != nil {
			if err := syscall.Setgroups(gids); err != nil {
				return nil, err
			}
		}
		if err := syscall.Setuid(uid); err != nil {
			return nil, err
		}
	}

	return l, nil
}

// Serve accepts incoming connections on the Listener l.
func (s *Server) Serve(l net.Listener) error {
	s.locker.Lock()
	s.listeners = append(s.listeners, l)
	s.locker.Unlock()

	var tempDelay time.Duration // how long to sleep on accept failure

	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-s.done:
				// we called Close()
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				s.ErrorLog.Printf("accept error: %s; retrying in %s", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return err
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()

			err := s.handleConn(newConn(c, s))
			if err != nil {
				s.ErrorLog.Printf("handler error: %s", err)
			}
		}()
	}
}

func (s *Server) ServeTLS(l net.Listener) error {
	tlsListener := tls.NewListener(l, s.TLSConfig)
	return s.Serve(tlsListener)
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

	if tlsConn, ok := c.conn.(*tls.Conn); ok {
		if d := s.ReadTimeout; d != 0 {
			c.conn.SetReadDeadline(time.Now().Add(d))
		}
		if d := s.WriteTimeout; d != 0 {
			c.conn.SetWriteDeadline(time.Now().Add(d))
		}
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
	}

	c.greet()

	for {
		line, err := c.readLine()
		if err == nil {
			cmd, arg, err := parseCmd(line)
			if err != nil {
				c.protocolError(501, EnhancedCode{5, 5, 2}, "Bad command")
				continue
			}

			c.handle(cmd, arg)
		} else {
			if err == io.EOF || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if err == ErrTooLongLine {
				c.writeResponse(500, EnhancedCode{5, 4, 0}, "Too long line, closing connection")
				return nil
			}

			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.writeResponse(221, EnhancedCode{2, 4, 2}, "Idle timeout, bye bye")
				return nil
			}

			c.writeResponse(221, EnhancedCode{2, 4, 0}, "Connection error, sorry")
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

	l, err := s.Listen(network, addr)
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

	l, err := s.Listen("tcp", addr)
	if err != nil {
		return err
	}

	return s.ServeTLS(l)
}

// Close immediately closes all active listeners and connections.
//
// Close returns any error returned from closing the server's underlying
// listener(s).
func (s *Server) Close() error {
	select {
	case <-s.done:
		return ErrServerClosed
	default:
		close(s.done)
	}

	var err error
	s.locker.Lock()
	for _, l := range s.listeners {
		if lerr := l.Close(); lerr != nil && err == nil {
			err = lerr
		}
	}

	for conn := range s.conns {
		conn.Close()
	}
	s.locker.Unlock()

	return err
}

// Shutdown gracefully shuts down the server without interrupting any
// active connections. Shutdown works by first closing all open
// listeners and then waiting indefinitely for connections to return to
// idle and then shut down.
// If the provided context expires before the shutdown is complete,
// Shutdown returns the context's error, otherwise it returns any
// error returned from closing the Server's underlying Listener(s).
func (s *Server) Shutdown(ctx context.Context) error {
	select {
	case <-s.done:
		return ErrServerClosed
	default:
		close(s.done)
	}

	var err error
	s.locker.Lock()
	for _, l := range s.listeners {
		if lerr := l.Close(); lerr != nil && err == nil {
			err = lerr
		}
	}
	s.locker.Unlock()

	connDone := make(chan struct{})
	go func() {
		defer close(connDone)
		s.wg.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connDone:
		return err
	}
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
