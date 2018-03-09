# go-smtp

[![GoDoc](https://godoc.org/github.com/emersion/go-smtp?status.svg)](https://godoc.org/github.com/emersion/go-smtp)
[![Build Status](https://travis-ci.org/emersion/go-smtp.svg?branch=master)](https://travis-ci.org/emersion/go-smtp)
[![codecov](https://codecov.io/gh/emersion/go-smtp/branch/master/graph/badge.svg)](https://codecov.io/gh/emersion/go-smtp)
[![stability-unstable](https://img.shields.io/badge/stability-unstable-yellow.svg)](https://github.com/emersion/stability-badges#unstable)
[![Go Report Card](https://goreportcard.com/badge/github.com/emersion/go-smtp)](https://goreportcard.com/report/github.com/emersion/go-smtp)

An ESMTP client and server library written in Go.

## Features

* ESMTP client & server implementing [RFC 5321](https://tools.ietf.org/html/rfc5321)
* Support for SMTP [AUTH](https://tools.ietf.org/html/rfc4954) and [PIPELINING](https://tools.ietf.org/html/rfc2920)
* UTF-8 support for subject and message

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

### Server

```go
package main

import (
	"errors"
	"io/ioutil"
	"log"

	"github.com/emersion/go-smtp"
)

type Backend struct{}

func (bkd *Backend) Login(username, password string) (smtp.User, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &User{}, nil
}

// Require clients to authenticate using SMTP AUTH before sending emails
func (bkd *Backend) AnonymousLogin() (smtp.User, error) {
	return nil, smtp.ErrAuthRequired
}

type User struct{}

func (u *User) Send(from string, to []string, r io.Reader) error {
	log.Println("Sending message:", from, to)

	if b, err := ioutil.ReadAll(r); err != nil {
		return err
	} else {
		log.Println("Data:", string(b))
	}
	return nil
}

func (u *User) Logout() error {
	return nil
}

func main() {
	be := &Backend{}

	s := smtp.NewServer(be)

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.MaxIdleSeconds = 300
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

## Licence

MIT
