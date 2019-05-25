package backendutil

import (
	"io"

	"github.com/emersion/go-smtp"
)

// TransformBackend is a backend that transforms messages.
type TransformBackend struct {
	Backend smtp.Backend

	TransformReset func()
	TransformMail  func(from string) (string, error)
	TransformRcpt  func(to string) (string, error)
	TransformData  func(r io.Reader) (io.Reader, error)
}

// Login implements the smtp.Backend interface.
func (be *TransformBackend) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	s, err := be.Backend.Login(state, username, password)
	if err != nil {
		return nil, err
	}
	return &transformSession{s, be}, nil
}

// AnonymousLogin implements the smtp.Backend interface.
func (be *TransformBackend) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	s, err := be.Backend.AnonymousLogin(state)
	if err != nil {
		return nil, err
	}
	return &transformSession{s, be}, nil
}

type transformSession struct {
	Session smtp.Session

	be *TransformBackend
}

func (s *transformSession) Reset() {
	s.be.TransformReset()
	s.Session.Reset()
}

func (s *transformSession) Mail(from string) error {
	from, err := s.be.TransformMail(from)
	if err != nil {
		return err
	}
	return s.Session.Mail(from)
}

func (s *transformSession) Rcpt(to string) error {
	to, err := s.be.TransformRcpt(to)
	if err != nil {
		return err
	}
	return s.Session.Rcpt(to)
}

func (s *transformSession) Data(r io.Reader) error {
	r, err := s.be.TransformData(r)
	if err != nil {
		return err
	}
	return s.Session.Data(r)
}

func (s *transformSession) Logout() error {
	s.be.TransformReset()
	return s.Session.Logout()
}
