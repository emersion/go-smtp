package smtp

type State int

type Conn struct {
	server     *Server
	helo       string
	from       string
	recipients []string
	response   string
	data       string
	subject    string
	hash       string
	conn       net.Conn
	errors     int
}

// Commands are dispatched to the appropriate handler functions.
func (c *Conn) handle(cmd string, arg string, line string) {
	c.logTrace("In state %d, got command '%s', args '%s'", c.state, cmd, arg)

	// Check against valid SMTP commands
	if cmd == "" {
		c.Write("500", "Speak up")
		//return
	}

	if cmd != "" && !commands[cmd] {
		c.Write("500", fmt.Sprintf("Syntax error, %v command unrecognized", cmd))
		c.logWarn("Unrecognized command: %v", cmd)
	}

	switch cmd {
	case "SEND", "SOML", "SAML", "EXPN", "HELP", "TURN":
		// These commands are not implemented in any state
		c.Write("502", fmt.Sprintf("%v command not implemented", cmd))
		c.logWarn("Command %v not implemented by Gsmtpd", cmd)
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
		c.logTrace("Resetting session state on RSET request")
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
		c.errors++
		if c.errors > 3 {
			c.Write("500", "Too many unrecognized commands")
			c.Close()
		}
	}
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
		c.state = 1
	case "EHLO":
		domain, err := parseHelloArgument(arg)
		if err != nil {
			c.Write("501", "Domain/address argument required for EHLO")
			return
		}

		if c.server.TLSConfig != nil && !c.tlsOn {
			c.Write("250", "Hello "+domain+"["+c.remoteHost+"]", "PIPELINING", "8BITMIME", "STARTTLS", "AUTH EXTERNAL CRAM-MD5 LOGIN PLAIN", fmt.Sprintf("SIZE %v", c.server.maxMessageBytes))
			//c.Write("250", "Hello "+domain+"["+c.remoteHost+"]", "8BITMIME", fmt.Sprintf("SIZE %v", c.server.maxMessageBytes), "HELP")
		} else {
			c.Write("250", "Hello "+domain+"["+c.remoteHost+"]", "PIPELINING", "8BITMIME", "AUTH EXTERNAL CRAM-MD5 LOGIN PLAIN", fmt.Sprintf("SIZE %v", c.server.maxMessageBytes))
		}
		c.helo = domain
		c.state = 1
	default:
		c.ooSeq(cmd)
	}
}

// READY state -> waiting for MAIL
func (c *Conn) mailHandler(cmd string, arg string) {
	if cmd == "MAIL" {
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
			c.logWarn("Bad MAIL argument: %q", arg)
			return
		}

		from := m[1]
		mailbox, domain, err := ParseEmailAddress(from)
		if err != nil {
			c.Write("501", "Bad sender address syntax")
			c.logWarn("Bad address as MAIL arg: %q, %s", from, err)
			return
		}

		// This is where the Conn may put BODY=8BITMIME, but we already
		// read the DATA as bytes, so it does not effect our processing.
		if m[2] != "" {
			args, ok := c.parseArgs(m[2])
			if !ok {
				c.Write("501", "Unable to parse MAIL ESMTP parameters")
				c.logWarn("Bad MAIL argument: %q", arg)
				return
			}
			if args["SIZE"] != "" {
				size, err := strconv.ParseInt(args["SIZE"], 10, 32)
				if err != nil {
					c.Write("501", "Unable to parse SIZE as an integer")
					c.logWarn("Unable to parse SIZE %q as an integer", args["SIZE"])
					return
				}
				if int(size) > c.server.maxMessageBytes {
					c.Write("552", "Max message size exceeded")
					c.logWarn("Conn wanted to send oversized message: %v", args["SIZE"])
					return
				}
			}
		}
		c.from = from
		c.logInfo("Mail from: %v", from)
		c.Write("250", fmt.Sprintf("Roger, accepting mail from <%v>", from))
		c.state = 1
	} else {
		c.ooSeq(cmd)
	}
}

// MAIL state -> waiting for RCPTs followed by DATA
func (c *Conn) rcptHandler(cmd string, arg string) {
	if cmd == "RCPT" {
		if c.from == "" {
			c.Write("502", "Missing MAIL FROM command.")
			return
		}

		if (len(arg) < 4) || (strings.ToUpper(arg[0:3]) != "TO:") {
			c.Write("501", "Was expecting RCPT arg syntax of TO:<address>")
			c.logWarn("Bad RCPT argument: %q", arg)
			return
		}

		// This trim is probably too forgiving
		recip := strings.Trim(arg[3:], "<> ")
		mailbox, host, err := ParseEmailAddress(recip)
		if err != nil {
			c.Write("501", "Bad recipient address syntax")
			c.logWarn("Bad address as RCPT arg: %q, %s", recip, err)
			return
		}

		if len(c.recipients) >= c.server.maxRecips {
			c.logWarn("Maximum limit of %v recipients reached", c.server.maxRecips)
			c.Write("552", fmt.Sprintf("Maximum limit of %v recipients reached", c.server.maxRecips))
			return
		}

		c.recipients = append(c.recipients, recip)
		c.logInfo("Recipient: %v", recip)
		c.Write("250", fmt.Sprintf("I'll make sure <%v> gets this", recip))
		return
	} else {
		c.ooSeq(cmd)
	}
}

func (c *Conn) authHandler(cmd string, arg string) {
	if cmd == "AUTH" {
		if c.helo == "" {
			c.Write("502", "Please introduce yourself first.")
			return
		}

		if arg == "" {
			c.Write("502", "Missing parameter")
			return
		}

		c.logTrace("Got AUTH command, staying in MAIL state %s", arg)
		parts := strings.Fields(arg)
		mechanism := strings.ToUpper(parts[0])

		/*	scanner := bufio.NewScanner(c.bufin)
			line := scanner.Text()
			c.logTrace("Read Line %s", line)
			if !scanner.Scan() {
				return
			}
		*/
		switch mechanism {
		case "LOGIN":
			c.Write("334", "VXNlcm5hbWU6")
		case "PLAIN":
			c.logInfo("Got PLAIN authentication: %s", mechanism)
			c.Write("235", "Authentication successful")
		case "CRAM-MD5":
			c.logInfo("Got CRAM-MD5 authentication, switching to AUTH state")
			c.Write("334", "PDQxOTI5NDIzNDEuMTI4Mjg0NzJAc291cmNlZm91ci5hbmRyZXcuY211LmVkdT4=")
		case "EXTERNAL":
			c.logInfo("Got EXTERNAL authentication: %s", strings.TrimPrefix(arg, "EXTERNAL "))
			c.Write("235", "Authentication successful")
		default:
			c.logTrace("Unsupported authentication mechanism %v", arg)
			c.Write("504", "Unsupported authentication mechanism")
		}
	} else {
		c.ooSeq(cmd)
	}
}

func (c *Conn) tlsHandler() {
	if c.tlsOn {
		c.Write("502", "Already running in TLS")
		return
	}

	if c.server.TLSConfig == nil {
		c.Write("502", "TLS not supported")
		return
	}

	log.LogTrace("Ready to start TLS")
	c.Write("220", "Ready to start TLS")

	// upgrade to TLS
	var tlsConn *tls.Conn
	tlsConn = tls.Server(c.conn, c.server.TLSConfig)
	err := tlsConn.Handshake() // not necessary to call here, but might as well

	if err == nil {
		//c.conn   = net.Conn(tlsConn)
		c.conn = tlsConn
		c.bufin = bufio.NewReader(c.conn)
		c.bufout = bufio.NewWriter(c.conn)
		c.tlsOn = true

		// Reset envelope as a new EHLO/HELO is required after STARTTLS
		c.reset()

		// Reset deadlines on the underlying connection before I replace it
		// with a TLS connection
		c.conn.SetDeadline(time.Time{})
		c.flush()
	} else {
		c.logWarn("Could not TLS handshake:%v", err)
		c.Write("550", "Handshake error")
	}

	c.state = 1
}

// DATA
func (c *Conn) dataHandler(cmd string, arg string) {
	c.logTrace("Enter dataHandler %d", c.state)

	if arg != "" {
		c.Write("501", "DATA command should not have any arguments")
		c.logWarn("Got unexpected args on DATA: %q", arg)
		return
	}

	if len(c.recipients) > 0 {
		// We have recipients, go to accept data
		c.logTrace("Go ahead we have recipients %d", len(c.recipients))
		c.Write("354", "Go ahead. End your data with <CR><LF>.<CR><LF>")
		c.state = 2
		return
	} else {
		c.Write("502", "Missing RCPT TO command.")
		return
	}

	return
}

func (c *Conn) processData() {
	var msg string

	for {
		buf := make([]byte, 1024)
		n, err := c.conn.Read(buf)

		if n == 0 {
			c.logInfo("Connection closed by remote host\n")
			c.server.killConn(c)
			break
		}

		if err != nil {
			if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
				c.Write("221", "Idle timeout, bye bye")
			}
			c.logInfo("Error reading from socket: %s\n", err)
			break
		}

		text := string(buf[0:n])
		msg += text

		// If we have debug true, save the mail to file for review
		if c.server.Debug {
			c.saveMailDatatoFile(msg)
		}

		if len(msg) > c.server.maxMessageBytes {
			c.logWarn("Maximum DATA size exceeded (%s)", strconv.Itoa(c.server.maxMessageBytes))
			c.Write("552", "Maximum message size exceeded")
			c.reset()
			return
		}

		//Postfix bug ugly hack (\r\n.\r\nQUIT\r\n)
		if strings.HasSuffix(msg, "\r\n.\r\n") || strings.LastIndex(msg, "\r\n.\r\n") != -1 {
			break
		}
	}

	if len(msg) > 0 {
		c.logTrace("Got EOF, storing message and switching to MAIL state")
		msg = strings.TrimSuffix(msg, "\r\n.\r\n")
		c.data = msg

		// Create Message Structure
		mc := &config.SMTPMessage{}
		mc.Helo = c.helo
		mc.From = c.from
		mc.To = c.recipients
		mc.Data = c.data
		mc.Host = c.remoteHost
		mc.Domain = c.server.domain
		mc.Notify = make(chan int)

		// Send to savemail channel
		c.server.Store.SaveMailChan <- mc

		select {
		// wait for the save to complete
		case status := <-mc.Notify:
			if status == 1 {
				c.Write("250", "Ok: queued as "+mc.Hash)
				c.logInfo("Message size %v bytes", len(msg))
			} else {
				c.Write("554", "Error: transaction failed, blame it on the weather")
				c.logError("Message save failed")
			}
		case <-time.After(time.Second * 60):
			c.Write("554", "Error: transaction failed, blame it on the weather")
			c.logError("Message save timeout")
		}
	}

	c.reset()
}

func (c *Conn) reject() {
	c.Write("421", "Too busy. Try again later.")
	c.server.closeConn(c)
}

func (c *Conn) enterState(state State) {
	c.state = state
	c.logTrace("Entering state %v", state)
}

func (c *Conn) greet() {
	c.Write("220", fmt.Sprintf("%v SMTP # %s (%s) %s", c.server.domain, strconv.FormatInt(c.id, 10), strconv.Itoa(len(c.server.sem)), time.Now().Format(time.RFC1123Z)))
	c.state = 1
}

func (c *Conn) flush() {
	c.conn.SetWriteDeadline(c.nextDeadline())
	c.bufout.Flush()
	c.conn.SetReadDeadline(c.nextDeadline())
}

// Calculate the next read or write deadline based on maxIdleSeconds
func (c *Conn) nextDeadline() time.Time {
	return time.Now().Add(time.Duration(c.server.maxIdleSeconds) * time.Second)
}

func (c *Conn) Write(code string, text ...string) {
	c.conn.SetDeadline(c.nextDeadline())
	if len(text) == 1 {
		c.logTrace(">> Sent %d bytes: %s >>", len(text[0]), text[0])
		c.conn.Write([]byte(code + " " + text[0] + "\r\n"))
		c.bufout.Flush()
		return
	}
	for i := 0; i < len(text)-1; i++ {
		c.logTrace(">> Sent %d bytes: %s >>", len(text[i]), text[i])
		c.conn.Write([]byte(code + "-" + text[i] + "\r\n"))
	}
	c.logTrace(">> Sent %d bytes: %s >>", len(text[len(text)-1]), text[len(text)-1])
	c.conn.Write([]byte(code + " " + text[len(text)-1] + "\r\n"))

	c.bufout.Flush()
}

// readByteLine reads a line of input into the provided buffer. Does
// not reset the Buffer - please do so prior to calling.
func (c *Conn) readByteLine(buf *bytes.Buffer) error {
	if err := c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return err
	}
	for {
		line, err := c.bufin.ReadBytes('\r')
		if err != nil {
			return err
		}
		buf.Write(line)
		// Read the next byte looking for '\n'
		c, err := c.bufin.ReadByte()
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
	// Should be unreachable
}

// Reads a line of input
func (c *Conn) readLine() (line string, err error) {
	if err = c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
		return "", err
	}

	line, err = c.bufin.ReadString('\n')
	if err != nil {
		return "", err
	}
	c.logTrace("<< %v <<", strings.TrimRight(line, "\r\n"))
	return line, nil
}

func (c *Conn) parseCmd(line string) (cmd string, arg string, ok bool) {
	line = strings.TrimRight(line, "\r\n")
	l := len(line)
	switch {
	case strings.HasPrefix(line, "STARTTLS"):
		return "STARTTLS", "", true
	case l == 0:
		return "", "", true
	case l < 4:
		c.logWarn("Command too short: %q", line)
		return "", "", false
	case l == 4:
		return strings.ToUpper(line), "", true
	case l == 5:
		// Too long to be only command, too short to have args
		c.logWarn("Mangled command: %q", line)
		return "", "", false
	}
	// If we made it here, command is long enough to have args
	if line[4] != ' ' {
		// There wasn't a space after the command?
		c.logWarn("Mangled command: %q", line)
		return "", "", false
	}
	// I'm not sure if we should trim the args or not, but we will for now
	//return strings.ToUpper(line[0:4]), strings.Trim(line[5:], " "), true
	return strings.ToUpper(line[0:4]), strings.Trim(line[5:], " \n\r"), true
}

// parseArgs takes the arguments proceeding a command and files them
// into a map[string]string after uppercasing each key.  Sample arg
// string:
//		" BODY=8BITMIME SIZE=1024"
// The leading space is mandatory.
func (c *Conn) parseArgs(arg string) (args map[string]string, ok bool) {
	args = make(map[string]string)
	re := regexp.MustCompile(" (\\w+)=(\\w+)")
	pm := re.FindAllStringSubmatch(arg, -1)
	if pm == nil {
		c.logWarn("Failed to parse arg string: %q")
		return nil, false
	}
	for _, m := range pm {
		args[strings.ToUpper(m[1])] = m[2]
	}
	c.logTrace("ESMTP params: %v", args)
	return args, true
}

func (c *Conn) reset() {
	c.state = 1
	c.from = ""
	c.helo = ""
	c.recipients = nil
}

func (c *Conn) ooSeq(cmd string) {
	c.Write("503", fmt.Sprintf("Command %v is out of sequence", cmd))
	c.logWarn("Wasn't expecting %v here", cmd)
}

func parseHelloArgument(arg string) (string, error) {
	domain := arg
	if idx := strings.IndexRune(arg, ' '); idx >= 0 {
		domain = arg[:idx]
	}
	if domain == "" {
		return "", fmt.Errorf("Invalid domain")
	}
	return domain, nil
}
