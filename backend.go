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

// MailOptions contains custom arguments that were
// passed as an argument to the MAIL command.
type MailOptions struct {
	// Size of the body. Can be 0 if not specified by client.
	Size int
}

type Session interface {
	// Discard currently processed message.
	Reset()

	// Free all resources associated with session.
	Logout() error

	// Set return path for currently processed message.
	Mail(from string, opts MailOptions) error
	// Add recipient for currently processed message.
	Rcpt(to string) error
	// Set currently processed message contents and send it.
	Data(r io.Reader) error
}
