package backendutil

import (
	"io"

	"github.com/emersion/go-smtp"
)

// TransformBackend is a backend that transforms messages.
type TransformBackend struct {
	Backend smtp.Backend
	Transform func(from string, to []string, r io.Reader) (string, []string, io.Reader, error)
}

// Login implements the smtp.Backend interface.
func (be *TransformBackend) Login(username, password string) (smtp.User, error) {
	u, err := be.Backend.Login(username, password)
	if err != nil {
		return nil, err
	}
	return &transformUser{u, be}, nil
}

func (be *TransformBackend) AnonymousLogin() (smtp.User, error) {
	u, err := be.Backend.AnonymousLogin()
	if err != nil {
		return nil, err
	}
	return &transformUser{u, be}, nil
}

type transformUser struct {
	smtp.User
	be *TransformBackend
}

func (u *transformUser) Send(from string, to []string, r io.Reader) error {
	from, to, r, err := u.be.Transform(from, to, r)
	if err != nil {
		return err
	}
	return u.User.Send(from, to, r)
}
