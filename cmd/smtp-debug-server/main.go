package main

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/emersion/go-smtp"
)

var addr string

func init() {
	flag.StringVar(&addr, "l", "127.0.0.1:1025", "Listen address")
}

type backend struct{}

func (bkd *backend) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	return &session{}, nil
}

func (bkd *backend) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	return &session{}, nil
}

type session struct{}

func (s *session) Mail(from string, opts smtp.MailOptions) error {
	return nil
}

func (s *session) Rcpt(to string) error {
	return nil
}

func (s *session) Data(r io.Reader) error {
	return nil
}

func (s *session) Reset() {}

func (s *session) Logout() error {
	return nil
}

func main() {
	flag.Parse()

	s := smtp.NewServer(&backend{})

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.AllowInsecureAuth = true
	s.Debug = os.Stdout

	log.Println("Starting SMTP server at", addr)
	log.Fatal(s.ListenAndServe())
}
