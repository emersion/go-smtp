package backendutil

import (
	"io"

	"github.com/emersion/go-smtp"
)

// TransformBackend is a backend that transforms messages.
type TransformBackend struct {
	Backend smtp.Backend

	Transform          func(session *smtp.Session, username string) TransformHandler
	AnonymousTransform func(session *smtp.Session) TransformHandler
}

// TransformHandler is a container for transforming funcs.
type TransformHandler interface {
	TransformReset()
	TransformMail(from string) (string, error)
	TransformRcpt(to string) (string, error)
	TransformData(r io.Reader) (io.Reader, error)
}

// Login implements the smtp.Backend interface.
func (be *TransformBackend) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	s, err := be.Backend.Login(state, username, password)
	if err != nil {
		return nil, err
	}
	trans := be.Transform(&s, username)
	return &transformSession{s, trans}, nil
}

// AnonymousLogin implements the smtp.Backend interface.
func (be *TransformBackend) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	s, err := be.Backend.AnonymousLogin(state)
	if err != nil {
		return nil, err
	}
	trans := be.AnonymousTransform(&s)
	return &transformSession{s, trans}, nil
}

type transformSession struct {
	Session smtp.Session

	trans TransformHandler
}

func (s *transformSession) Reset() {
	if s.trans != nil && s.trans.TransformReset != nil {
		s.trans.TransformReset()
	}
	s.Session.Reset()
}

func (s *transformSession) Mail(from string) error {
	if s.trans != nil && s.trans.TransformMail != nil {
		var err error
		from, err = s.trans.TransformMail(from)
		if err != nil {
			return err
		}
	}
	return s.Session.Mail(from)
}

func (s *transformSession) Rcpt(to string) error {
	if s.trans != nil && s.trans.TransformRcpt != nil {
		var err error
		to, err = s.trans.TransformRcpt(to)
		if err != nil {
			return err
		}
	}
	return s.Session.Rcpt(to)
}

func (s *transformSession) Data(r io.Reader) error {
	if s.trans != nil && s.trans.TransformData != nil {
		var err error
		r, err = s.trans.TransformData(r)
		if err != nil {
			return err
		}
	}
	return s.Session.Data(r)
}

func (s *transformSession) Logout() error {
	if s.trans != nil && s.trans.TransformReset != nil {
		s.trans.TransformReset()
	}
	return s.Session.Logout()
}
