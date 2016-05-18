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
	"log"

	smtpserver "github.com/emersion/go-smtp-server"
)

func main() {
	cfg := &smtpserver.Config{
		Domain: "localhost",
		MaxIdleSeconds: 300,
		MaxMessageBytes: 1024 * 1024,
		AllowInsecureAuth: true,
	}

	s, err := smtpserver.Listen(":3000", cfg, nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Server listening at", s.Addr())

	done := make(chan bool)
	<-done
}
```

## Licence

Copyright © 2014 Gleez Technologies  
Copyright © 2016 emersion  

Released under MIT license, see [LICENSE](LICENSE) for details.
