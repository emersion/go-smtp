# go-smtp

[![GoDoc](https://godoc.org/github.com/emersion/go-smtp?status.svg)](https://godoc.org/github.com/emersion/go-smtp)
[![builds.sr.ht status](https://builds.sr.ht/~emersion/go-smtp.svg)](https://builds.sr.ht/~emersion/go-smtp?)
[![codecov](https://codecov.io/gh/emersion/go-smtp/branch/master/graph/badge.svg)](https://codecov.io/gh/emersion/go-smtp)
[![stability-unstable](https://img.shields.io/badge/stability-unstable-yellow.svg)](https://github.com/emersion/stability-badges#unstable)

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
	// Set up authentication information.
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

If you need more control, you can use `Client` instead.

### SMTP Server

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

// Login handles a login command with username and password.
func (bkd *Backend) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &Session{}, nil
}

// AnonymousLogin requires clients to authenticate using SMTP AUTH before sending emails
func (bkd *Backend) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	return nil, smtp.ErrAuthRequired
}

// A Session is returned after successful login.
type Session struct{}

func (s *Session) Mail(from string) error {
	log.Println("Mail from:", from)
	return nil
}

func (s *Session) Rcpt(to string) error {
	log.Println("Rcpt to:", to)
	return nil
}

func (s *Session) Data(r io.Reader, d smtp.DataContext) error {
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

### LMTP Server

```go
package main

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"time"
	"fmt"

	"github.com/emersion/go-smtp"
)

// The Backend implements LMTP server methods.
type Backend struct{}

// Login handles a login command with username and password.
func (be *Backend) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	return nil, smtp.ErrAuthUnsupported
}

// AnonymousLogin requires clients to authenticate using SMTP AUTH before sending emails
func (be *Backend) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	return &Session{}, nil
}

// Session is returned for every connection
type Session struct{
	RcptTos     []string
}

func (s *Session) Mail(from string) error {
	log.Println("Mail from:", from)
	return nil
}

func (s *Session) Rcpt(to string) error {
	log.Println("Rcpt to:", to)
	s.RcptTos = append(s.RcptTos, to)
	return nil
}

func (s *Session) Data(r io.Reader, dataContext smtp.DataContext) error {
	mailBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	} 
	
	globalCtx := context.Background()

	for i, rcpt := range s.RcptTos {
		rcptCtx, _ := context.WithTimeout(globalCtx, 2*time.Second)
		// we have to assing i and rcpt new to access them in the go() routine
		rcpt := rcpt
		i := i

		dataContext.StartDelivery(rcptCtx, rcpt)
		go func() {
			// normaly we would deliver the mailBytes to our Maildir/HTTP backend
			// in this case we just do a sleep 
			time.Sleep(time.Duration(2+i) * time.Second)
			fmt.Println(string(mailBytes))
            // Lets finish with OK (if the request wasn't canceled because of the ctx timeout)
			dataContext.SetStatus(rcpt, &smtp.SMTPError{
				Code: 250,
				EnhancedCode:  smtp.EnhancedCode{2, 0, 0},
				Message: "Finished",
			})
		}()

	}
	// we always return nil in LMTP because every rcpt return code was set with dataContext.SetStatus()
	return nil
}

func (s *Session) Reset() {
	// we need to reset our rcptTo's slice:
	s.RcptTos = []string{}
}

func (s *Session) Logout() error {
	return nil
}

func main() {
	be := &Backend{}

	s := smtp.NewServer(be)

	s.LMTP = true
	s.Addr = ":1025"
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	log.Println("Starting LMTP Server at", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
```

You can use the server manually with `telnet`:
```
$ telnet localhost 1025
LHLO localhost
MAIL FROM:<from@example.com>
RCPT TO:<rcpt1@example.com>
RCPT TO:<rcpt2@example.com>
RCPT TO:<rcpt3@example.com>
DATA
Hey <3
.
250 2.0.0 <rcpt1@example.com> Finished
420 4.4.7 <rcpt2@example.com> Error: timeout reached
420 4.4.7 <rcpt3@example.com> Error: timeout reached
```


## Licence

MIT
