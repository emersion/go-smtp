# go-smtp-server

[![GoDoc](https://godoc.org/github.com/emersion/go-smtp-server?status.svg)](https://godoc.org/github.com/emersion/go-smtp-server)
[![Build Status](https://travis-ci.org/emersion/go-smtp-server.svg?branch=master)](https://travis-ci.org/emersion/go-smtp-server)

An ESMTP server library written in Go.

## Features

* ESMTP server implementing [RFC 5321](https://tools.ietf.org/html/rfc5321)
* Support for SMTP AUTH ([RFC 4954](https://tools.ietf.org/html/rfc4954)) and PIPELINING ([RFC 2920](https://tools.ietf.org/html/rfc2920))
* UTF-8 support for subject and message

## Usage

```go
package main

import (
	"errors"
	"log"

	smtpserver "github.com/emersion/go-smtp-server"
)

type Backend struct {}

func (bkd *Backend) Login(username, password string) (smtp.User, error) {
	if username != "username" || password != "password" {
		return nil, errors.New("Invalid username or password")
	}
	return &User{}, nil
}

type User struct {}

func (u *User) Send(msg *smtp.Message) error {
	log.Println("Message sent:", msg)
	return nil
}

func main() {
	cfg := &smtp.Config{
		Domain: "localhost",
		MaxIdleSeconds: 300,
		MaxMessageBytes: 1024 * 1024,
		AllowInsecureAuth: true,
	}

	bkd := &Backend{}

	s, err := smtp.Listen(":3000", cfg, bkd)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Server listening at", s.Addr())

	done := make(chan bool)
	<-done
}
```

You can use the server manually with `telnet`:
```
$ telnet localhost 3000
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

Copyright © 2014 Gleez Technologies  
Copyright © 2016 emersion  

Released under MIT license, see [LICENSE](LICENSE) for details.
