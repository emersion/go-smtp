# Server Cookbook for go-smtp
This document contains information that can be usefull when implementing the go-smtp server.


## How to use go-smtp with TLS on the server

### Generate private key
```shell
# Key considerations for algorithm "RSA" ≥ 2048-bit
openssl genrsa -out ./server.key 2048

# Key considerations for algorithm "ECDSA" (X25519 || ≥ secp384r1)
# https://safecurves.cr.yp.to/
# List ECDSA the supported curves (openssl ecparam -list_curves)
openssl ecparam -genkey -name secp384r1 -out ./server.key
```


### Modify server code

Implement the default server as described in the [root README.md](../README.md)
and change the `main` function to

```go
func main() {
	be := &Backend{}

	s := smtp.NewServer(be)

	s.Addr = ":1025"
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50

	// force TLS for auth
	s.AllowInsecureAuth = false
	// Load the certificate and key
	cer, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		log.Fatal(err)
		return
	}
	// Configure the TLS support
	s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cer}}

	log.Println("Starting server at", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
```


## Use alternate authentication methods
I you keep yourself to using methods that are supported by
[go-sasl](https://github.com/emersion/go-sasl), which is used by go-smtp, then
it should be fairly easy.

### AUTH LOGIN
LOGIN is an deprecated authentication method but sometimes you have to play
with the toys you got. For example, Microsofts `Send-MailMessage` PowerShell
command does uses LOGIN and does not support PLAIN.

If you read issue [Implement AUTH LOGIN method #41][i41] you will
find that [RFC4422](https://tools.ietf.org/html/rfc4422) reference a
[list](https://www.iana.org/assignments/sasl-mechanisms/sasl-mechanisms.xhtml)
that shows LOGIN as deprecated.


[i41]: https://github.com/emersion/go-smtp/issues/41

Change your server code and a call to `EnableAuth`
```go
	s := smtp.NewServer(be)

	// Add deprecated LOGIN auth method as some clients haven't learned
	s.EnableAuth(sasl.Login, func(conn *smtp.Conn) sasl.Server {
		return sasl.NewLoginServer(func(username, password string) error {
			state := conn.State()
			session, err := be.Login(&state, username, password)
			if err != nil {
				return err
			}

			conn.SetSession(session)
			return nil
		})
	})
```
