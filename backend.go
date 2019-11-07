package smtp

import (
	"errors"
	"io"
)

var (
	ErrAuthRequired    = errors.New("Please authenticate first")
	ErrAuthUnsupported = errors.New("Authentication not supported")
)

// A SMTP server backend.
type Backend interface {
	// Authenticate a user. Return smtp.ErrAuthUnsupported if you don't want to
	// support this.
	Login(state *ConnectionState, username, password string) (Session, error)

	// Called if the client attempts to send mail without logging in first.
	// Return smtp.ErrAuthRequired if you don't want to support this.
	AnonymousLogin(state *ConnectionState) (Session, error)
}

type Session interface {
	// Discard currently processed message.
	Reset()

	// Free all resources associated with session.
	Logout() error

	// Set return path for currently processed message.
	Mail(from string) error
	// Add recipient for currently processed message.
	Rcpt(to string) error
	// Set currently processed message contents and send it.
	Data(r io.Reader) error
}

type LMTPSession interface {
	// LMTPData is the LMTP-specific version of Data method.
	// It can be optionally implemented by the backend to provide
	// per-recipient status information when it is used over LMTP
	// protocol.
	//
	// LMTPData implementation sets status information using passed
	// StatusCollector by calling SetStatus once per each AddRcpt
	// call, even if AddRcpt was called multiple times with
	// the same argument. SetStatus must not be called after
	// LMTPData returns.
	//
	// Return value of LMTPData itself is used as a status for
	// recipients that got no status set before using StatusCollector.
	LMTPData(r io.Reader, status StatusCollector) error
}

type StatusCollector interface {
	SetStatus(rcptTo string, err error)
}
