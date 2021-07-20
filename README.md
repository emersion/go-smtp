# go-smtp

[![godocs.io](https://godocs.io/github.com/emersion/go-smtp?status.svg)](https://godocs.io/github.com/emersion/go-smtp)
[![builds.sr.ht status](https://builds.sr.ht/~emersion/go-smtp/commits.svg)](https://builds.sr.ht/~emersion/go-smtp/commits?)

An ESMTP client and server library written in Go.

## Features

* ESMTP client & server implementing [RFC 5321](https://tools.ietf.org/html/rfc5321)
* Support for SMTP [AUTH](https://tools.ietf.org/html/rfc4954) and [PIPELINING](https://tools.ietf.org/html/rfc2920)
* UTF-8 support for subject and message
* [LMTP](https://tools.ietf.org/html/rfc2033) support

## Usage

### Client

```go
package main

import (
	"log"
	"strings"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func main() {
	// Setup authentication information.
	auth := sasl.NewPlainClient("", "user@example.com", "password")

	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	to := []string{"recipient@example.net"}
	msg := strings.NewReader("To: recipient@example.net\r\n" +
		"Subject: discount Gophers!\r\n" +
		"\r\n" +
		"This is the email body.\r\n")
	err := smtp.SendMail("mail.example.com:25", auth, "sender@example.org", to, msg)
	if err != nil {
		log.Fatal(err)
	}
}
```

If you need more control, you can use `Client` instead. For example, if you
want to send an email via a server without TLS or auth support, you can do
something like this:

```go
package main

import (
	"log"
	"strings"

	"github.com/emersion/go-smtp"
)

func main() {
	// Setup an unencrypted connection to a local mail server.
	c, err := smtp.Dial("localhost:25")
	if err != nil {
		return err
	}
	defer c.Close()

	// Set the sender and recipient, and send the email all in one step.
	to := []string{"recipient@example.net"}
	msg := strings.NewReader("To: recipient@example.net\r\n" +
		"Subject: discount Gophers!\r\n" +
		"\r\n" +
		"This is the email body.\r\n")
	err := c.SendMail("sender@example.org", to, msg)
	if err != nil {
		log.Fatal(err)
	}
}
```

### Server

```go
package main

import (
	"errors"
	"io"
	"io/ioutil"
	"log"
	"time"

	"github.com/emersion/go-smtp"
)

// The Backend implements SMTP server methods.
type Backend struct{}

func (bkd *Backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &Session{}, nil
}

// A Session is returned after EHLO.
type Session struct{}

func (s *Session) AuthPlain(username, password string) error {
	if username != "username" || password != "password" {
		return errors.New("Invalid username or password")
	}
	return nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	log.Println("Mail from:", from)
	return nil
}

func (s *Session) Rcpt(to string) error {
	log.Println("Rcpt to:", to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	if b, err := ioutil.ReadAll(r); err != nil {
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

func main() {
	be := &Backend{}

	s := smtp.NewServer(be)

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	log.Println("Starting server at", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
```

You can use the server manually with `telnet`:
```
$ telnet localhost 1025
EHLO localhost
AUTH PLAIN
AHVzZXJuYW1lAHBhc3N3b3Jk
MAIL FROM:<root@nsa.gov>
RCPT TO:<root@gchq.gov.uk>
DATA
Hey <3
.
```

## Relationship with net/smtp

The Go standard library provides a SMTP client implementation in `net/smtp`.
However `net/smtp` is frozen: it's not getting any new features. go-smtp
provides a server implementation and a number of client improvements.

## Licence

MIT
