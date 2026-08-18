# go-smtp

[![Go Reference](https://pkg.go.dev/badge/github.com/emersion/go-smtp.svg)](https://pkg.go.dev/github.com/emersion/go-smtp)

An ESMTP client and server library written in Go.

## Features

* ESMTP client & server implementing [RFC 5321]
* Support for additional SMTP extensions such as [AUTH] and [PIPELINING]
* UTF-8 support for subject and message
* [LMTP] support
* Connection lifecycle hooks via `ConnState` callback

## Relationship with net/smtp

The Go standard library provides a SMTP client implementation in `net/smtp`.
However `net/smtp` is frozen: it's not getting any new features. go-smtp
provides a server implementation and a number of client improvements.

## Connection Lifecycle Monitoring

The server supports a `ConnState` hook for monitoring connection lifecycle events,
similar to `net/http.Server.ConnState`:

```go
s := smtp.NewServer(backend)
s.ConnState = func(conn net.Conn, state smtp.ConnState) {
    log.Printf("Connection %s: %v", conn.RemoteAddr(), state)
}
```

Available connection states:
- `StateNew` - New connection established
- `StateActive` - Connection ready for SMTP commands
- `StateAuth` - During SASL authentication
- `StateData` - Receiving message data
- `StateStartTLS` - During TLS handshake
- `StateReset` - After RSET command
- `StateIdle` - Between commands
- `StateError` - Connection in error state
- `StateClosed` - Connection closed

## Licence

MIT

[RFC 5321]: https://tools.ietf.org/html/rfc5321
[AUTH]: https://tools.ietf.org/html/rfc4954
[PIPELINING]: https://tools.ietf.org/html/rfc2920
[LMTP]: https://tools.ietf.org/html/rfc2033
