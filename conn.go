package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Conn struct {
	server    *Server
	helo      string
	User      User
	msg       *Message
	conn      net.Conn
	reader    *bufio.Reader
	nbrErrors int
}

// Commands are dispatched to the appropriate handler functions.
func (c *Conn) handle(cmd string, arg string) {
	if cmd == "" {
		c.Write("500", "Speak up")
		return
	}

	switch cmd {
	case "SEND", "SOML", "SAML", "EXPN", "HELP", "TURN":
		// These commands are not implemented in any state
		c.Write("502", fmt.Sprintf("%v command not implemented", cmd))
	case "HELO", "EHLO":
		c.greetHandler(cmd, arg)
	case "MAIL":
		c.mailHandler(cmd, arg)
	case "RCPT":
		c.rcptHandler(cmd, arg)
	case "VRFY":
		c.Write("252", "Cannot VRFY user, but will accept message")
	case "NOOP":
		c.Write("250", "I have sucessfully done nothing")
	case "RSET": // Reset session
		c.reset()
		c.Write("250", "Session reset")
	case "DATA":
		c.dataHandler(cmd, arg)
	case "QUIT":
		c.Write("221", "Goodnight and good luck")
		c.Close()
	case "AUTH":
		c.authHandler(cmd, arg)
	case "STARTTLS":
		c.tlsHandler()
	default:
		c.Write("500", fmt.Sprintf("Syntax error, %v command unrecognized", cmd))

		c.nbrErrors++
		if c.nbrErrors > 3 {
			c.Write("500", "Too many unrecognized commands")
			c.Close()
		}
	}
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

// Check if this connection is encrypted.
func (c *Conn) IsTLS() bool {
	_, ok := c.conn.(*tls.Conn)
	return ok
}

// GREET state -> waiting for HELO
func (c *Conn) greetHandler(cmd string, arg string) {
	switch cmd {
	case "HELO":
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.Write("501", "Domain/address argument required for HELO")
			return
		}
		c.helo = domain

		c.Write("250", fmt.Sprintf("Hello %s", domain))
	case "EHLO":
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.Write("501", "Domain/address argument required for EHLO")
			return
		}

		c.helo = domain

		caps := []string{}
		caps = append(caps, c.server.caps...)
		if c.server.TLSConfig != nil && !c.IsTLS() {
			caps = append(caps, "STARTTLS")
		}
		if c.IsTLS() || c.server.Config.AllowInsecureAuth {
			authCap := "AUTH"
			for name, _ := range c.server.auths {
				authCap += " " + name
			}

			caps = append(caps, authCap)
		}
		if c.server.Config.MaxMessageBytes > 0 {
			caps = append(caps, fmt.Sprintf("SIZE %v", c.server.Config.MaxMessageBytes))
		}

		args := []string{"Hello "+domain}
		args = append(args, caps...)
		c.Write("250", args...)
	default:
		c.ooSeq(cmd)
	}
}

// READY state -> waiting for MAIL
func (c *Conn) mailHandler(cmd string, arg string) {
	if cmd != "MAIL" {
		c.ooSeq(cmd)
		return
	}

	if c.helo == "" {
		c.Write("502", "Please introduce yourself first.")
		return
	}

	// Match FROM, while accepting '>' as quoted pair and in double quoted strings
	// (?i) makes the regex case insensitive, (?:) is non-grouping sub-match
	re := regexp.MustCompile("(?i)^FROM:\\s*<((?:\\\\>|[^>])+|\"[^\"]+\"@[^>]+)>( [\\w= ]+)?$")
	m := re.FindStringSubmatch(arg)
	if m == nil {
		c.Write("501", "Was expecting MAIL arg syntax of FROM:<address>")
		return
	}

	from := m[1]

	// This is where the Conn may put BODY=8BITMIME, but we already
	// read the DATA as bytes, so it does not effect our processing.
	if m[2] != "" {
		args, err := parseArgs(m[2])
		if err != nil {
			c.Write("501", "Unable to parse MAIL ESMTP parameters")
			return
		}

		if args["SIZE"] != "" {
			size, err := strconv.ParseInt(args["SIZE"], 10, 32)
			if err != nil {
				c.Write("501", "Unable to parse SIZE as an integer")
				return
			}

			if c.server.Config.MaxMessageBytes > 0 && int(size) > c.server.Config.MaxMessageBytes {
				c.Write("552", "Max message size exceeded")
				return
			}
		}
	}

	c.msg.From = from
	c.Write("250", fmt.Sprintf("Roger, accepting mail from <%v>", from))
}

// MAIL state -> waiting for RCPTs followed by DATA
func (c *Conn) rcptHandler(cmd string, arg string) {
	if cmd != "RCPT" {
		c.ooSeq(cmd)
		return
	}

	if c.msg == nil || c.msg.From == "" {
		c.Write("502", "Missing MAIL FROM command.")
		return
	}

	if (len(arg) < 4) || (strings.ToUpper(arg[0:3]) != "TO:") {
		c.Write("501", "Was expecting RCPT arg syntax of TO:<address>")
		return
	}

	// This trim is probably too forgiving
	recipient := strings.Trim(arg[3:], "<> ")

	if c.server.Config.MaxRecipients > 0 && len(c.msg.To) >= c.server.Config.MaxRecipients {
		c.Write("552", fmt.Sprintf("Maximum limit of %v recipients reached", c.server.Config.MaxRecipients))
		return
	}

	c.msg.To = append(c.msg.To, recipient)
	c.Write("250", fmt.Sprintf("I'll make sure <%v> gets this", recipient))
}

func (c *Conn) authHandler(cmd string, arg string) {
	if cmd != "AUTH" {
		c.ooSeq(cmd)
		return
	}

	if c.helo == "" {
		c.Write("502", "Please introduce yourself first.")
		return
	}

	if arg == "" {
		c.Write("502", "Missing parameter")
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
		c.Write("504", "Unsupported authentication mechanism")
		return
	}

	sasl := newSasl(c)
	scanner := bufio.NewScanner(c.reader)

	response := ir
	for {
		challenge, done, err := sasl.Next(response)
		if err != nil {
			c.Write("454", err.Error())
			return
		}

		if done {
			break
		}

		encoded := ""
		if len(challenge) > 0 {
			encoded = base64.StdEncoding.EncodeToString(challenge)
		}
		c.Write("334", encoded)

		if !scanner.Scan() {
			return
		}

		encoded = scanner.Text()
		if encoded != "" {
			response, err = base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				c.Write("454", "Invalid base64 data")
				return
			}
		}
	}

	if c.User != nil {
		c.Write("235", "Authentication succeeded")

		c.msg = &Message{}
	}
}

func (c *Conn) tlsHandler() {
	if c.IsTLS() {
		c.Write("502", "Already running in TLS")
		return
	}

	if c.server.TLSConfig == nil {
		c.Write("502", "TLS not supported")
		return
	}

	c.Write("220", "Ready to start TLS")

	// Upgrade to TLS
	var tlsConn *tls.Conn
	tlsConn = tls.Server(c.conn, c.server.TLSConfig)

	if err := tlsConn.Handshake(); err != nil {
		c.Write("550", "Handshake error")
	}

	c.conn = tlsConn

	// Reset envelope as a new EHLO/HELO is required after STARTTLS
	c.reset()
}

// DATA
func (c *Conn) dataHandler(cmd string, arg string) {
	if arg != "" {
		c.Write("501", "DATA command should not have any arguments")
		return
	}

	if c.msg == nil || c.msg.From == "" || len(c.msg.To) == 0 {
		c.Write("502", "Missing RCPT TO command.")
		return
	}

	// We have recipients, go to accept data
	c.Write("354", "Go ahead. End your data with <CR><LF>.<CR><LF>")

	c.processData()
	return
}

func (c *Conn) processData() {
	var msg string

	for {
		buf := make([]byte, 1024)
		n, err := c.conn.Read(buf)

		if n == 0 { // Connection closed by remote host
			c.Close()
			break
		}

		if err != nil {
			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.Write("221", "Idle timeout, bye bye")
			}
			break // Error reading from socket
		}

		text := string(buf[0:n])
		msg += text

		if c.server.Config.MaxMessageBytes > 0 && len(msg) > c.server.Config.MaxMessageBytes {
			c.Write("552", "Maximum message size exceeded")
			c.reset()
			return
		}

		// Postfix bug ugly hack (\r\n.\r\nQUIT\r\n)
		if strings.HasSuffix(msg, "\r\n.\r\n") || strings.LastIndex(msg, "\r\n.\r\n") != -1 {
			break
		}
	}

	if len(msg) > 0 { // Got EOF, storing message and switching to MAIL state
		msg = strings.TrimSuffix(msg, "\r\n.\r\n")
		c.msg.Data = []byte(msg)

		if err := c.User.Send(c.msg); err != nil {
			c.Write("554", "Error: transaction failed, blame it on the weather")
		} else {
			c.Write("250", "Ok: queued")
		}
	}

	c.reset()
}

func (c *Conn) Reject() {
	c.Write("421", "Too busy. Try again later.")
	c.Close()
}

func (c *Conn) greet() {
	c.Write("220", fmt.Sprintf("%v ESMTP Service Ready", c.server.Config.Domain))
}

// Calculate the next read or write deadline based on MaxIdleSeconds.
func (c *Conn) nextDeadline() time.Time {
	if c.server.Config.MaxIdleSeconds == 0 {
		return time.Time{} // No deadline
	}

	return time.Now().Add(time.Duration(c.server.Config.MaxIdleSeconds) * time.Second)
}

func (c *Conn) Write(code string, text ...string) {
	c.conn.SetDeadline(c.nextDeadline())

	if len(text) == 1 {
		c.conn.Write([]byte(code + " " + text[0] + "\r\n"))
		return
	}
	for i := 0; i < len(text)-1; i++ {
		c.conn.Write([]byte(code + "-" + text[i] + "\r\n"))
	}
	c.conn.Write([]byte(code + " " + text[len(text)-1] + "\r\n"))
}

// readByteLine reads a line of input into the provided buffer. Does
// not reset the Buffer - please do so prior to calling.
func (c *Conn) readByteLine(buf *bytes.Buffer) error {
	if err := c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return err
	}
	for {
		line, err := c.reader.ReadBytes('\r')
		if err != nil {
			return err
		}
		buf.Write(line)
		// Read the next byte looking for '\n'
		c, err := c.reader.ReadByte()
		if err != nil {
			return err
		}
		buf.WriteByte(c)
		if c == '\n' {
			// We've reached the end of the line, return
			return nil
		}
		// Else, keep looking
	}
}

// Reads a line of input
func (c *Conn) readLine() (line string, err error) {
	if err = c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return "", err
	}

	line, err = c.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return line, nil
}

func (c *Conn) reset() {
	c.helo = ""
	c.User = nil
	c.msg = nil
}

func (c *Conn) ooSeq(cmd string) {
	c.Write("503", fmt.Sprintf("Command %v is out of sequence", cmd))
}
