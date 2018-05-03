package smtp

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A SMTP message.
type message struct {
	// The message contents.
	io.Reader

	// The sender e-mail address.
	From string
	// The recipients e-mail addresses.
	To []string
}

type Conn struct {
	conn      net.Conn
	text      *textproto.Conn
	server    *Server
	helo      string
	msg       *message
	nbrErrors int
	user      User
	locker    sync.Mutex
}

var (
	ErrAuthRequired = fmt.Errorf("Please authenticate first.")
)

func newConn(c net.Conn, s *Server) *Conn {
	sc := &Conn{
		server: s,
		conn:   c,
	}

	sc.init()
	return sc
}

func (c *Conn) init() {
	var rwc io.ReadWriteCloser = c.conn
	if c.server.Debug != nil {
		rwc = struct {
			io.Reader
			io.Writer
			io.Closer
		}{
			io.TeeReader(c.conn, c.server.Debug),
			io.MultiWriter(c.conn, c.server.Debug),
			c.conn,
		}
	}

	c.text = textproto.NewConn(rwc)
}

func (c *Conn) unrecognizedCommand(cmd string) {
	c.WriteResponse(500, fmt.Sprintf("Syntax error, %v command unrecognized", cmd))

	c.nbrErrors++
	if c.nbrErrors > 3 {
		c.WriteResponse(500, "Too many unrecognized commands")
		c.Close()
	}
}

// Commands are dispatched to the appropriate handler functions.
func (c *Conn) handle(cmd string, arg string) {
	if cmd == "" {
		c.WriteResponse(500, "Speak up")
		return
	}

	switch cmd {
	case "SEND", "SOML", "SAML", "EXPN", "HELP", "TURN":
		// These commands are not implemented in any state
		c.WriteResponse(502, fmt.Sprintf("%v command not implemented", cmd))
	case "HELO", "EHLO":
		c.handleGreet((cmd == "EHLO"), arg)
	case "MAIL":
		c.handleMail(arg)
	case "RCPT":
		c.handleRcpt(arg)
	case "VRFY":
		c.WriteResponse(252, "Cannot VRFY user, but will accept message")
	case "NOOP":
		c.WriteResponse(250, "I have sucessfully done nothing")
	case "RSET": // Reset session
		c.reset()
		c.WriteResponse(250, "Session reset")
	case "DATA":
		c.handleData(arg)
	case "QUIT":
		c.WriteResponse(221, "Goodnight and good luck")
		c.Close()
	case "AUTH":
		if c.server.AuthDisabled {
			c.unrecognizedCommand(cmd)
		} else {
			c.handleAuth(arg)
		}
	case "STARTTLS":
		c.handleStartTLS()
	default:
		c.unrecognizedCommand(cmd)
	}
}

func (c *Conn) Server() *Server {
	return c.server
}

func (c *Conn) User() User {
	c.locker.Lock()
	defer c.locker.Unlock()
	return c.user
}

// Setting the user resets any message beng generated
func (c *Conn) SetUser(user User) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.user = user
	c.msg = &message{}
}

func (c *Conn) Close() error {
	if user := c.User(); user != nil {
		user.Logout()
	}

	return c.conn.Close()
}

// Check if this connection is encrypted.
func (c *Conn) IsTLS() bool {
	_, ok := c.conn.(*tls.Conn)
	return ok
}

func (c *Conn) authAllowed() bool {
	return !c.server.AuthDisabled &&
		(c.IsTLS() || c.server.AllowInsecureAuth)
}

// GREET state -> waiting for HELO
func (c *Conn) handleGreet(enhanced bool, arg string) {
	if !enhanced {
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.WriteResponse(501, "Domain/address argument required for HELO")
			return
		}
		c.helo = domain

		c.WriteResponse(250, fmt.Sprintf("Hello %s", domain))
	} else {
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.WriteResponse(501, "Domain/address argument required for EHLO")
			return
		}

		c.helo = domain

		caps := []string{}
		caps = append(caps, c.server.caps...)
		if c.server.TLSConfig != nil && !c.IsTLS() {
			caps = append(caps, "STARTTLS")
		}
		if c.authAllowed() {
			authCap := "AUTH"
			for name, _ := range c.server.auths {
				authCap += " " + name
			}

			caps = append(caps, authCap)
		}
		if c.server.MaxMessageBytes > 0 {
			caps = append(caps, fmt.Sprintf("SIZE %v", c.server.MaxMessageBytes))
		}

		args := []string{"Hello " + domain}
		args = append(args, caps...)
		c.WriteResponse(250, args...)
	}
}

// READY state -> waiting for MAIL
func (c *Conn) handleMail(arg string) {
	if c.helo == "" {
		c.WriteResponse(502, "Please introduce yourself first.")
		return
	}

	if c.User() == nil {
		user, err := c.server.Backend.AnonymousLogin()
		if err != nil {
			c.WriteResponse(502, err.Error())
			return
		}

		c.SetUser(user)
	}

	if len(arg) < 6 || strings.ToUpper(arg[0:5]) != "FROM:" {
		c.WriteResponse(501, "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}
	fromArgs := strings.Split(strings.Trim(arg[5:], " "), " ")
	if c.server.Strict {
		if !strings.HasPrefix(fromArgs[0], "<") || !strings.HasSuffix(fromArgs[0], ">") {
			c.WriteResponse(501, "Was expecting MAIL arg syntax of FROM:<address>")
			return
		}
	}
	from := strings.Trim(fromArgs[0], "<> ")
	if from == "" {
		c.WriteResponse(501, "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}

	// This is where the Conn may put BODY=8BITMIME, but we already
	// read the DATA as bytes, so it does not effect our processing.
	if len(fromArgs) > 1 {
		args, err := parseArgs(fromArgs[1:])
		if err != nil {
			c.WriteResponse(501, "Unable to parse MAIL ESMTP parameters")
			return
		}

		if args["SIZE"] != "" {
			size, err := strconv.ParseInt(args["SIZE"], 10, 32)
			if err != nil {
				c.WriteResponse(501, "Unable to parse SIZE as an integer")
				return
			}

			if c.server.MaxMessageBytes > 0 && int(size) > c.server.MaxMessageBytes {
				c.WriteResponse(552, "Max message size exceeded")
				return
			}
		}
	}

	c.msg.From = from
	c.WriteResponse(250, fmt.Sprintf("Roger, accepting mail from <%v>", from))
}

// MAIL state -> waiting for RCPTs followed by DATA
func (c *Conn) handleRcpt(arg string) {
	if c.msg == nil || c.msg.From == "" {
		c.WriteResponse(502, "Missing MAIL FROM command.")
		return
	}

	if (len(arg) < 4) || (strings.ToUpper(arg[0:3]) != "TO:") {
		c.WriteResponse(501, "Was expecting RCPT arg syntax of TO:<address>")
		return
	}

	// TODO: This trim is probably too forgiving
	recipient := strings.Trim(arg[3:], "<> ")

	if c.server.MaxRecipients > 0 && len(c.msg.To) >= c.server.MaxRecipients {
		c.WriteResponse(552, fmt.Sprintf("Maximum limit of %v recipients reached", c.server.MaxRecipients))
		return
	}

	c.msg.To = append(c.msg.To, recipient)
	c.WriteResponse(250, fmt.Sprintf("I'll make sure <%v> gets this", recipient))
}

func (c *Conn) handleAuth(arg string) {
	if c.helo == "" {
		c.WriteResponse(502, "Please introduce yourself first.")
		return
	}

	if arg == "" {
		c.WriteResponse(502, "Missing parameter")
		return
	}

	parts := strings.Fields(arg)
	mechanism := strings.ToUpper(parts[0])

	// Parse client initial response if there is one
	var ir []byte
	if len(parts) > 1 {
		var err error
		ir, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return
		}
	}

	newSasl, ok := c.server.auths[mechanism]
	if !ok {
		c.WriteResponse(504, "Unsupported authentication mechanism")
		return
	}

	sasl := newSasl(c)

	response := ir
	for {
		challenge, done, err := sasl.Next(response)
		if err != nil {
			c.WriteResponse(454, err.Error())
			return
		}

		if done {
			break
		}

		encoded := ""
		if len(challenge) > 0 {
			encoded = base64.StdEncoding.EncodeToString(challenge)
		}
		c.WriteResponse(334, encoded)

		encoded, err = c.ReadLine()
		if err != nil {
			return // TODO: error handling
		}

		response, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			c.WriteResponse(454, "Invalid base64 data")
			return
		}
	}

	if c.User() != nil {
		c.WriteResponse(235, "Authentication succeeded")
	}
}

func (c *Conn) handleStartTLS() {
	if c.IsTLS() {
		c.WriteResponse(502, "Already running in TLS")
		return
	}

	if c.server.TLSConfig == nil {
		c.WriteResponse(502, "TLS not supported")
		return
	}

	c.WriteResponse(220, "Ready to start TLS")

	// Upgrade to TLS
	var tlsConn *tls.Conn
	tlsConn = tls.Server(c.conn, c.server.TLSConfig)

	if err := tlsConn.Handshake(); err != nil {
		c.WriteResponse(550, "Handshake error")
	}

	c.conn = tlsConn
	c.init()

	// Reset envelope as a new EHLO/HELO is required after STARTTLS
	c.reset()
}

// DATA
func (c *Conn) handleData(arg string) {
	if arg != "" {
		c.WriteResponse(501, "DATA command should not have any arguments")
		return
	}

	if c.msg == nil || c.msg.From == "" || len(c.msg.To) == 0 {
		c.WriteResponse(502, "Missing RCPT TO command.")
		return
	}

	// We have recipients, go to accept data
	c.WriteResponse(354, "Go ahead. End your data with <CR><LF>.<CR><LF>")

	c.msg.Reader = newDataReader(c)
	err := c.User().Send(c.msg.From, c.msg.To, c.msg.Reader)
	io.Copy(ioutil.Discard, c.msg.Reader) // Make sure all the data has been consumed
	if err != nil {
		if smtperr, ok := err.(*smtpError); ok {
			c.WriteResponse(smtperr.Code, smtperr.Message)
		} else {
			c.WriteResponse(554, "Error: transaction failed, blame it on the weather: "+err.Error())
		}
	} else {
		c.WriteResponse(250, "Ok: queued")
	}

	c.reset()
}

func (c *Conn) Reject() {
	c.WriteResponse(421, "Too busy. Try again later.")
	c.Close()
}

func (c *Conn) greet() {
	c.WriteResponse(220, fmt.Sprintf("%v ESMTP Service Ready", c.server.Domain))
}

// Calculate the next read or write deadline based on MaxIdleSeconds.
func (c *Conn) nextDeadline() time.Time {
	if c.server.MaxIdleSeconds == 0 {
		return time.Time{} // No deadline
	}

	return time.Now().Add(time.Duration(c.server.MaxIdleSeconds) * time.Second)
}

func (c *Conn) WriteResponse(code int, text ...string) {
	// TODO: error handling

	c.conn.SetDeadline(c.nextDeadline())

	for i := 0; i < len(text)-1; i++ {
		c.text.PrintfLine("%v-%v", code, text[i])
	}
	c.text.PrintfLine("%v %v", code, text[len(text)-1])
}

// Reads a line of input
func (c *Conn) ReadLine() (string, error) {
	if err := c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return "", err
	}

	return c.text.ReadLine()
}

func (c *Conn) reset() {
	c.locker.Lock()
	defer c.locker.Unlock()

	if c.user != nil {
		c.user.Logout()
	}

	c.user = nil
	c.msg = nil
}
