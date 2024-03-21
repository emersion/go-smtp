package backendutil

import (
	"io"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// TransformBackend is a backend that transforms messages.
type TransformBackend struct {
	Backend smtp.Backend

	TransformMail func(from string) (string, error)
	TransformRcpt func(to string) (string, error)
	TransformData func(r io.Reader) (io.Reader, error)
}

func (be *TransformBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	sess, err := be.Backend.NewSession(c)
	if err != nil {
		return nil, err
	}
	return &transformSession{Session: sess, be: be}, nil
}

type transformSession struct {
	Session smtp.Session

	be *TransformBackend
}

func (s *transformSession) Reset() {
	s.Session.Reset()
}

func (s *transformSession) AuthMechanisms() []string {
	if authSession, ok := s.Session.(smtp.AuthSession); ok {
		return authSession.AuthMechanisms()
	}
	return nil
}

func (s *transformSession) Auth(mech string) (sasl.Server, error) {
	if authSession, ok := s.Session.(smtp.AuthSession); ok {
		return authSession.Auth(mech)
	}
	return nil, smtp.ErrAuthUnsupported
}

func (s *transformSession) Mail(from string, opts *smtp.MailOptions) error {
	if s.be.TransformMail != nil {
		var err error
		from, err = s.be.TransformMail(from)
		if err != nil {
			return err
		}
	}
	return s.Session.Mail(from, opts)
}

func (s *transformSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	if s.be.TransformRcpt != nil {
		var err error
		to, err = s.be.TransformRcpt(to)
		if err != nil {
			return err
		}
	}
	return s.Session.Rcpt(to, opts)
}

func (s *transformSession) Data(r io.Reader) error {
	if s.be.TransformData != nil {
		var err error
		r, err = s.be.TransformData(r)
		if err != nil {
			return err
		}
	}
	return s.Session.Data(r)
}

func (s *transformSession) Logout() error {
	return s.Session.Logout()
}
