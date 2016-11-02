package smtp

import (
	"io"
)

// A SMTP server backend.
type Backend interface {
	// Authenticate a user.
	Login(username, password string) (User, error)
}

// An authenticated user.
type User interface {
	// Send an e-mail.
	Send(from string, to []string, r io.Reader) error
	// Logout is called when this User will no longer be used.
	Logout() error
}
